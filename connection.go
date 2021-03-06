package h2client

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"github.com/pkg/errors"
	"golang.org/x/net/http2/hpack"
	"io"
	"math"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type (
	Connection struct {
		Logger io.Writer

		host string
		port int

		tcpConn     net.Conn
		conn        *tls.Conn
		connReadMu  sync.Mutex
		connWriteMu sync.Mutex

		doMu sync.Mutex

		connState ConnectionState

		pollBytesBuffer  sync.Pool // *bytes.Buffer
		pollByteSlice    sync.Pool // []byte Первоначальный размер гарантируется не менее 4096 байт
		pollStream       sync.Pool // *Stream
		pollHeaderFrames sync.Pool // *HeadersFrame
		pollDataFrames   sync.Pool // *DataFrame с инициализированным DataFrame.Data

		lastStreamId       uint32
		settings           Settings
		hpackDecoder       *hpack.Decoder
		hpackEncoder       *hpack.Encoder
		hpackEncoderBuffer bytes.Buffer

		flowControlWindow          int64 // это лимит удаленной стороны, который мы уменьшаем, когда МЫ отсылаем DATA фреймы, а не когда их присылают нам
		flowControlWindowEmptyFrom int64 // с какого времени нам не достаточно буфера

		streamsActive   map[uint32]*connectionStream
		streamsReserved int32 // сколько стримов используются в соединении. может быть больше len(streamsActive), но всегда не больше settings.MaxConcurrentStreams
		streamsActiveMu sync.RWMutex

		limitedReader io.LimitedReader

		goawayFrames   []*GoawayFrame
		goawayFramesMu sync.RWMutex

		readTimeout  time.Duration
		writeTimeout time.Duration
	}
)

var (
	clientConnectionPreface = []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
	endianess               = binary.BigEndian
	nextProto               = `h2`
)

var (
	// nowCached обновляется примерно каждые 10мс
	nowCached time.Time
)

func init() {
	nowCached = time.Now()
	go func() {
		t := time.Tick(10 * time.Millisecond)
		for {
			<-t
			nowCached = time.Now()
		}
	}()
}

func NewConnection(req *request) (*Connection, error) {
	tlsConf := tls.Config{
		NextProtos: []string{nextProto},
		ServerName: req.Host,
	}
	req.tlsConfMu.RLock()
	if req.tlsConf != nil {
		tlsConf.Certificates = req.tlsConf.Certificates
	}
	req.tlsConfMu.RUnlock()

	tcpConn, err := net.DialTimeout(`tcp`, req.Host+`:`+strconv.Itoa(req.Port), req.DialTimeout)
	if err != nil {
		return nil, errors.Wrap(err, `TCP connect fail`)
	}

	conn := tls.Client(tcpConn, &tlsConf)

	errChan := make(chan error, 2)
	timer := time.AfterFunc(req.DialTimeout, func() {
		errChan <- ErrTimeout
	})
	go func() {
		err := conn.Handshake()
		timer.Stop()
		errChan <- errors.Wrap(err, `TLS handshake fail`)
	}()
	if err := <-errChan; err != nil {
		tcpConn.Close()
		return nil, errors.Wrap(err, `TLS handshare fail or timeouted`)
	}

	settings := GetDefaultSettings()
	h2c := Connection{
		Logger: os.Stderr, // ioutil.Discard,

		host:              req.Host,
		port:              req.Port,
		tcpConn:           tcpConn,
		conn:              conn,
		connState:         ConnectionStateDisconn,
		lastStreamId:      1, // нечетные для клиента, 1 стрим пропускается
		settings:          settings,
		flowControlWindow: int64(settings.InitialWindowSize),
		streamsActive:     make(map[uint32]*connectionStream),

		readTimeout:  1 * time.Second,
		writeTimeout: 1 * time.Second,
	}

	h2c.pollBytesBuffer.New = func() interface{} {
		return &bytes.Buffer{}
	}
	h2c.pollByteSlice.New = func() interface{} {
		return make([]byte, 4096)
	}
	h2c.pollStream.New = func() interface{} {
		return newStream(&h2c)
	}
	h2c.pollHeaderFrames.New = func() interface{} {
		return &HeadersFrame{}
	}
	h2c.pollDataFrames.New = func() interface{} {
		buf := bytes.Buffer{}
		buf.Grow(4096)
		return &DataFrame{
			Data: &buf,
		}
	}

	h2c.hpackEncoder = hpack.NewEncoder(&h2c.hpackEncoderBuffer)
	h2c.hpackEncoder.SetMaxDynamicTableSizeLimit(settings.HeaderTableSize)

	h2c.hpackDecoder = hpack.NewDecoder(settings.HeaderTableSize, nil)

	if err := h2c.beginHandshake(); err != nil {
		conn.Close()
		return nil, errors.Wrap(err, `Handshake fail`)
	}

	go h2c.reader()

	return &h2c, nil
}

func (c *Connection) IsConnected() bool {
	return c.connState == ConnectionStateOpened
}

func (c *Connection) LockStream() bool {
	if c.connState != ConnectionStateOpened {
		return false
	}

	if c.HasGoAwayFrames() {
		// после получения GOAWAY создавать новые стримы на данном соединении уже нельзя
		return false
	}

	if fcw := atomic.LoadInt64(&c.flowControlWindow); fcw < 512 {
		// окна почти нет, лучше взять другое соединение
		c.markFlowControlInsufficient()
		return false
	}

	settingsMaxCnt := int64(c.settings.MaxConcurrentStreams)
	if cnt := atomic.AddInt32(&c.streamsReserved, 1); int64(cnt) <= settingsMaxCnt {
		return true
	}
	// достигнут лимит числа стримов в соединении
	atomic.AddInt32(&c.streamsReserved, -1)
	return false
}

func (c *Connection) UnlockStream() bool {
	if cnt := atomic.AddInt32(&c.streamsReserved, -1); cnt >= 0 {
		return true
	}
	// не парный вызов?

	atomic.AddInt32(&c.streamsReserved, 1)
	return false
}

func (c *Connection) GetGoAwayFrames() (frames []*GoawayFrame) {
	c.goawayFramesMu.RLock()
	frames = c.goawayFrames
	c.goawayFramesMu.RUnlock()
	return
}

func (c *Connection) HasGoAwayFrames() (has bool) {
	c.goawayFramesMu.RLock()
	has = len(c.goawayFrames) > 0
	c.goawayFramesMu.RUnlock()
	return
}

func (c *Connection) IsClosed() bool {
	return c.connState == ConnectionStateClosed
}

func (c *Connection) IsConsideredClosed() bool {
	return c.connState == ConnectionStateClosed || c.connState == ConnectionStateWantClose
}

func (c *Connection) Close() error {
	if c.IsClosed() {
		return errors.Wrap(ErrConnectionAlreadyClosed, `Cannot close already closed connection`)
	}
	c.connState = ConnectionStateClosed

	c.streamsActiveMu.Lock()
	for _, stream := range c.streamsActive {
		select {
		case stream.respWait <- respWaitItem{streamId: stream.id, succ: false}:
		default:
		}
	}
	c.streamsActiveMu.Unlock()

	c.hpackDecoder.Close()

	return c.conn.Close()
}

func (c *Connection) WantClose() {
	if !c.IsConsideredClosed() {
		c.connState = ConnectionStateWantClose
	}
}

func (c *Connection) writeChunks(chunks ...[]byte) error {
	c.connWriteMu.Lock()
	c.conn.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	for _, chunk := range chunks {
		if _, err := c.conn.Write(chunk); err != nil {
			c.connWriteMu.Unlock()
			return err
		}
	}
	c.connWriteMu.Unlock()
	return nil
}

func (c *Connection) readChunk(chunk []byte) error {
	c.connReadMu.Lock()

	readed, needed := 0, len(chunk)
	for readed < needed {
		c.conn.SetReadDeadline(time.Now().Add(c.readTimeout))
		if n, err := c.conn.Read(chunk[readed:]); err != nil {
			c.connReadMu.Unlock()
			return err
		} else {
			readed += n
		}
	}

	c.connReadMu.Unlock()
	return nil
}

func (c *Connection) beginHandshake() error {
	// тут при ошибке получаем PROTOCOL_ERROR, а GOAWAY не обязателен - удаленная сторона считается не умеющей в HTTP/2

	// send preface
	if err := c.writeChunks(clientConnectionPreface); err != nil {
		return errors.Wrap(err, `Send preface failed`)
	}

	// send settings
	var buf [18]byte
	endianess.PutUint16(buf[0:2], uint16(SettingsInitialWindowSize))
	endianess.PutUint32(buf[2:6], 1024*1024)
	endianess.PutUint16(buf[6:8], uint16(SettingsMaxFrameSize))
	endianess.PutUint32(buf[8:12], 1024*1024)
	endianess.PutUint16(buf[12:14], uint16(SettingsEnablePush))
	endianess.PutUint32(buf[14:18], 0)

	if err := c.sendFrame(FrameTypeSettings, 0, 0, buf[:]); err != nil {
		return errors.Wrap(err, `Send settings frame failed`)
	}

	c.connState = ConnectionStateWaitPrefaceSettings

	return nil
}

func (c *Connection) reader() {
	defer c.Close()

	timer := time.NewTimer(math.MaxInt64)

	lastRecvAt := nowCached.Unix()

	for !c.IsClosed() {
		frame, err := c.recvFrame()
		if err != nil {
			if diff := nowCached.Unix() - lastRecvAt; diff > 15 {
				//c.Logger.Write([]byte(fmt.Sprintf("Connection#%p lastRecvFrame timeout\n", c)))
				return
			}

			causeErr := errors.Cause(err)
			if netErr, ok := causeErr.(net.Error); ok {
				if netErr.Timeout() || netErr.Temporary() {
					continue
				}
			} else if causeErr == ErrTimeout {
				continue
			} else if causeErr == io.EOF || causeErr == io.ErrUnexpectedEOF {
				return
			}

			c.Logger.Write([]byte(fmt.Sprintf(`CONNECTION#%p h2client.Connection.recvFrame error: `, c)))
			errors.Fprint(c.Logger, err)
			c.Logger.Write([]byte("\n"))

			continue // или выйти?
		}

		lastRecvAt = nowCached.Unix()

		frameHdr := frame.Hdr()

		if frameHdr.StreamId == 0 { // служебный фрейм
			switch frameHdr.Type {
			case FrameTypeSettings:
				c.settings.UpdateFromSettingsFrame(frame.(*SettingsFrame))
				c.hpackEncoder.SetMaxDynamicTableSizeLimit(c.settings.HeaderTableSize)

				if frame.Hdr().Flags&FlagAck == 0 {
					// не нужно отсылать ACK на пришедший ACK :)
					if err := c.sendFrame(FrameTypeSettings, FlagAck, 0, nil); err != nil {
						c.Logger.Write([]byte("Cannot send answer to SETTINGS frame\n"))
						return
					}
				}

				if c.connState == ConnectionStateWaitPrefaceSettings {
					c.connState = ConnectionStateOpened
				}

			case FrameTypeWindowUpdate:
				windowUpdateFrame := frame.(*WindowUpdateFrame)
				atomic.AddInt64(&c.flowControlWindow, int64(windowUpdateFrame.WindowSizeIncrement))
				c.markFlowControlSufficient()

			case FrameTypeGoaway:
				goawayFrame := frame.(*GoawayFrame)
				c.goawayFramesMu.Lock()
				c.goawayFrames = append(c.goawayFrames, goawayFrame)
				c.goawayFramesMu.Unlock()

			case FrameTypePing:
				pingFrame := frame.(*PingFrame)
				if (pingFrame.Flags & FlagAck) == 0 {
					c.sendFrame(FrameTypePing, FlagAck, 0, pingFrame.Data[:])
				}

			default:
				// игнорируем незнакомые типы фреймов
			}

			continue
		}

		c.streamsActiveMu.RLock()
		stream, ok := c.streamsActive[frameHdr.StreamId]
		c.streamsActiveMu.RUnlock()
		if !ok {
			// Сюда попадаем (по хорошему) для уже отработанных стримов, по которым еще долетают фреймы.
			// Просто игнорируем такие фреймы.
			continue
		}

		switch frameHdr.Type {
		case FrameTypeHeaders:
			// ToDo: HeaderBlockFragment всегда передается фреймами, идущими подряд, без включений других

			headersFrame := frame.(*HeadersFrame)
			for _, header := range headersFrame.HeaderBlockFragment {
				switch header.Key {
				case `:status`:
					if code, err := strconv.Atoi(header.Value); err == nil {
						stream.resp.Status = code
					}

				case `content-length`:
					if size, err := strconv.Atoi(header.Value); err == nil {
						stream.resp.Body.Grow(size) // ?
					}

				default:
					stream.resp.Headers = append(stream.resp.Headers, header)
				}
			}
			c.pollHeaderFrames.Put(headersFrame)

		case FrameTypeData:
			dataFrame := frame.(*DataFrame)

			c.limitedReader.R = dataFrame.Data
			c.limitedReader.N = int64(dataFrame.Data.Len())
			stream.resp.Body.ReadFrom(&c.limitedReader)

			c.pollDataFrames.Put(dataFrame)

		case FrameTypeRstStream:
			rstStreamFrame := frame.(*RstStreamFrame)

			c.streamsActiveMu.Lock()
			c.Logger.Write([]byte(fmt.Sprintln(`RST_STREAM`, rstStreamFrame.ErrorCode, len(c.streamsActive))))
			delete(c.streamsActive, frameHdr.StreamId)
			c.streamsActiveMu.Unlock()

			stream.respWait <- respWaitItem{streamId: stream.id, succ: false}

			continue // или break?

		case FrameTypeWindowUpdate:
			windowUpdateFrame := frame.(*WindowUpdateFrame)
			atomic.AddInt64(&stream.flowControlWindow, int64(windowUpdateFrame.WindowSizeIncrement))

		default:
			// по документации мы должны игнорировать не известные фреймы
		}

		if frameHdr.Flags&FlagEndStream != 0 {
			c.streamsActiveMu.Lock()
			delete(c.streamsActive, frameHdr.StreamId)
			c.streamsActiveMu.Unlock()

			timer.Reset(1 * time.Second)
			select {
			case <-timer.C:
			default:
			}

			select {
			case stream.respWait <- respWaitItem{streamId: stream.id, succ: true}:
				// all ok
			case <-timer.C:
				// wtf ?
			}
		}
	}
}

func (c *Connection) Req(req *request) (*response, error) {
	if !c.LockStream() {
		var err error
		if c.HasGoAwayFrames() {
			err = ErrGoAwayRecieved
		} else {
			err = ErrPoolCapacityLimit
		}
		return nil, errors.Wrap(err, `There are no capacity for new stream in connection`)
	}

	resp, err := c.reqWithLockedStream(req)

	c.UnlockStream()

	return resp, err
}

func (c *Connection) reqWithLockedStream(req *request) (*response, error) {
	for c.connState != ConnectionStateOpened { // ToDo: сделать лучше
		if c.IsConsideredClosed() {
			return nil, errors.Wrap(ErrConnectionAlreadyClosed, `Cannot work over closed connection`)
		}
		time.Sleep(10 * time.Millisecond)
	}

	c.doMu.Lock()
	payload, err := c.buildRequestPayload(req.Method, req.Path, req.Headers)
	if err != nil {
		c.doMu.Unlock()
		return nil, errors.Wrap(err, `buildRequestPayload fail`)
	}

	streamIdx := atomic.AddUint32(&c.lastStreamId, 2)
	stream := c.pollStream.Get().(*connectionStream)
	stream.Reset(c)
	stream.id = streamIdx
	stream.flowControlWindow = int64(c.settings.InitialWindowSize)

	c.streamsActiveMu.Lock()
	c.streamsActive[streamIdx] = stream
	c.streamsActiveMu.Unlock()

	// send request

	withBody := (req.Body != nil) && (req.Body.Len() > 0)

	flags := FlagEndHeaders
	if !withBody {
		flags |= FlagEndStream
	}

	err = c.sendFrame(FrameTypeHeaders, flags, streamIdx, payload.Bytes())
	if err != nil {
		c.doMu.Unlock()
		c.pollStream.Put(stream)
		c.WantClose()
		return nil, errors.Wrap(err, `Send header frame fail`)
	}
	c.doMu.Unlock()

	if withBody {
		if err := c.sendRequestBody(req, stream); err != nil {
			c.WantClose()
			return nil, errors.Wrap(err, `Cannot send request body payload`)
		}
	}

	// recv response

	req.timer.Reset(req.Timeout)
forLabel:
	for {
		select {
		case respItem := <-stream.respWait:
			if respItem.streamId == streamIdx {
				// это ответ на наш запрос, все хорошо
				if !respItem.succ {
					stream.resp.Canceled = true
				}
				break forLabel
			}
			// к нам долетел ответ на стрим, который раньше вовремя не ответил
			// повторяем ожидание
			continue forLabel

		case <-req.timer.C:
			// время вышло
			stream.resp.Canceled = true
		}

		break
	}

	return &stream.resp, nil
}

func (c *Connection) sendRequestBody(req *request, stream *connectionStream) error {
	bodyPos, bodyRest, bodyBuf := int64(0), int64(req.Body.Len()), req.Body.Bytes()
	for bodyRest > 0 {
		wantSend := int64(c.settings.MaxFrameSize)
		if wantSend > bodyRest {
			wantSend = bodyRest
		}

		waitTill := time.Now().Add(req.Timeout)
		for {
			if waitTill.Before(nowCached) {
				// таймаут ожидания
				c.markFlowControlInsufficient()
				return errors.Wrap(ErrNoFlowControlCapacity, `Flow-control timeout`)
			}

			connFcw := atomic.AddInt64(&c.flowControlWindow, -wantSend)
			streamFcw := atomic.AddInt64(&stream.flowControlWindow, -wantSend)
			if connFcw < 0 || streamFcw < 0 {
				atomic.AddInt64(&c.flowControlWindow, wantSend)
				atomic.AddInt64(&stream.flowControlWindow, wantSend)
				time.Sleep(time.Duration(rand.Int31n(50)+10) * time.Millisecond)
				continue
			}

			c.markFlowControlSufficient()

			break
		}

		flags := FrameFlags(0)
		if wantSend == bodyRest {
			flags |= FlagEndStream
		}
		err := c.sendFrame(FrameTypeData, flags, stream.id, bodyBuf[bodyPos:bodyPos+wantSend])
		if err != nil {
			// В теории, можно бы увеличивать flowControlWindow, коли не отправили. Но нужно ли?
			return errors.Wrap(err, `Send data frame fail`)
		}
		bodyRest -= wantSend
	}
	return nil
}

func (c *Connection) buildRequestPayload(method string, path string, headers []HeaderPair) (*bytes.Buffer, error) {
	c.hpackEncoderBuffer.Reset()

	if err := c.hpackEncoder.WriteField(hpack.HeaderField{Name: `:method`, Value: strings.ToUpper(method)}); err != nil {
		return nil, errors.Wrap(err, `HPACK encoder fail`)
	} else if err := c.hpackEncoder.WriteField(hpack.HeaderField{Name: `:scheme`, Value: `https`}); err != nil {
		return nil, errors.Wrap(err, `HPACK encoder fail`)
	} else if err := c.hpackEncoder.WriteField(hpack.HeaderField{Name: `:path`, Value: path, Sensitive: true}); err != nil {
		return nil, errors.Wrap(err, `HPACK encoder fail`)
	} else if err := c.hpackEncoder.WriteField(hpack.HeaderField{Name: `host`, Value: c.host}); err != nil {
		return nil, errors.Wrap(err, `HPACK encoder fail`)
	}
	for _, header := range headers {
		key := strings.ToLower(header.Key)
		val := header.Value
		if err := c.hpackEncoder.WriteField(hpack.HeaderField{Name: key, Value: val}); err != nil {
			return nil, errors.Wrap(err, `HPACK encoder fail`)
		}
	}

	// ToDo: если в запросе есть тело, то тогда нужно учитывать flow-control window удаленной стороны
	// flowControlWindow может быть меньше нуля (если переборщили с отправкой)

	return &c.hpackEncoderBuffer, nil
}

func (c *Connection) sendFrame(type_ FrameType, flags FrameFlags, streamId uint32, payload []byte) error {
	buf := c.pollBytesBuffer.Get().(*bytes.Buffer)
	buf.Reset()

	if err := c.buildFrame(buf, type_, flags, streamId, payload); err != nil {
		c.pollBytesBuffer.Put(buf)
		return errors.Wrap(err, `Build frame failed`)
	}

	err := c.writeChunks(buf.Bytes())

	c.pollBytesBuffer.Put(buf)
	return errors.Wrap(err, `Write to connection failed`)
}

func (c *Connection) buildFrame(buffer *bytes.Buffer, type_ FrameType, flags FrameFlags, streamId uint32, payload []byte) error {
	var buf [4]byte

	endianess.PutUint32(buf[0:4], uint32(len(payload))&0xFFFFFF) // The size of the frame header is not included when describing frame sizes.
	buffer.Write(buf[1:4])

	buffer.Write([]byte{byte(type_), byte(flags)})
	endianess.PutUint32(buf[0:4], streamId&0x7FFFFFFF)
	buffer.Write(buf[0:4])

	if len(payload) > 0 {
		buffer.Write(payload)
	}

	return nil
}

func (c *Connection) updateFlowWindows(size uint32, streamId uint32) error {
	var payload [4]byte
	endianess.PutUint32(payload[:], size)

	if err := c.sendFrame(FrameTypeWindowUpdate, 0, 0, payload[:]); err != nil {
		return err
	} else if err := c.sendFrame(FrameTypeWindowUpdate, 0, streamId, payload[:]); err != nil {
		return err
	}
	return nil
}

func (c *Connection) recvFrame() (frame Frame, err error) {
	var frameHdr FrameHdr

	payload := c.pollByteSlice.Get().([]byte)[:]
	defer c.pollByteSlice.Put(payload)

	if err := c.readChunk(payload[0:9]); err != nil {
		return frame, errors.Wrap(err, `ReadAtLeast(frameHdr) failed`)
	}
	var buf2 [4]byte
	copy(buf2[1:], payload[0:3])
	frameHdr.Length = endianess.Uint32(buf2[:]) // если длина недопустима, то обязаны отправить ошибку FRAME_SIZE_ERROR

	frameHdr.Type = FrameType(payload[3])
	frameHdr.Flags = FrameFlags(payload[4])
	frameHdr.StreamId = endianess.Uint32(payload[5:9]) & 0x7FFFFFFF

	if frameHdr.Length > 0 {
		if uint32(cap(payload)) < frameHdr.Length {
			payload = make([]byte, frameHdr.Length)
		}

		if err := c.readChunk(payload[0:frameHdr.Length]); err != nil {
			return frame, errors.Wrap(err, `ReadAtLeast(payload) failed`)
		}
	}

	switch frameHdr.Type {
	case FrameTypeSettings:
		if (frameHdr.Length % 6) != 0 {
			return frame, errors.Wrap(ErrWrongFramePayloadLength, `Wrong frame`)
		}

		frameSettings := SettingsFrame{FrameHdr: frameHdr}
		for p := uint32(0); p < frameHdr.Length; p += 6 {
			id := SettingsType(endianess.Uint16(payload[p : p+2]))
			val := endianess.Uint32(payload[p+2 : p+6])
			frameSettings.Params = append(frameSettings.Params, SettingsFrameParam{Id: id, Value: val})
		}
		return &frameSettings, nil

	case FrameTypeGoaway:
		if frameHdr.Length < 8 {
			return frame, errors.Wrap(ErrWrongFramePayloadLength, `Wrong frame`)
		}

		frameGoaway := GoawayFrame{FrameHdr: frameHdr}
		frameGoaway.LastStreamId = endianess.Uint32(payload[0:4])
		frameGoaway.ErrorCode = ErrorCode(endianess.Uint32(payload[4:8]))
		if frameHdr.Length > 8 {
			frameGoaway.DebugData = make([]byte, frameHdr.Length-8)
			copy(frameGoaway.DebugData, payload[8:])
		}
		return &frameGoaway, nil

	case FrameTypeRstStream:
		if frameHdr.Length != 4 {
			return frame, errors.Wrap(ErrWrongFramePayloadLength, `Wrong frame`)
		}

		frameRstStream := RstStreamFrame{FrameHdr: frameHdr}
		frameRstStream.ErrorCode = ErrorCode(endianess.Uint32(payload[0:4]))
		return &frameRstStream, nil

	case FrameTypeHeaders:
		if frameHdr.Flags&(FlagPadded|FlagPriority) != 0 {
			panic(`NIH padded or priority headers frame`)
		}

		frameHeaders := c.pollHeaderFrames.Get().(*HeadersFrame)
		frameHeaders.FrameHdr = frameHdr
		frameHeaders.HeaderBlockFragment = frameHeaders.HeaderBlockFragment[0:0]

		if frameHdr.Length > 0 {
			c.hpackDecoder.SetEmitFunc(func(f hpack.HeaderField) {
				frameHeaders.HeaderBlockFragment = append(frameHeaders.HeaderBlockFragment, HeaderPair{Key: f.Name, Value: f.Value})
			})

			if _, err = c.hpackDecoder.Write(payload[0:frameHdr.Length]); err != nil {
				// ToDo: нужно отправлять COMPRESSION_ERROR
				return frame, errors.Wrap(ErrHPACKDecodeFail, `Wrong frame`)
			}
			c.hpackDecoder.Close()
		}

		return frameHeaders, nil

	case FrameTypeData:
		if frameHdr.Flags&FlagPadded != 0 {
			panic(`NIH padded data frame`)
		}

		frameData := c.pollDataFrames.Get().(*DataFrame)
		frameData.FrameHdr = frameHdr

		if frameHdr.Length > 0 {
			frameData.Data.Reset()
			frameData.Data.Write(payload[0:frameHdr.Length])

			if err := c.updateFlowWindows(frameHdr.Length, frameHdr.StreamId); err != nil { // ToDo: делать асинхронно, да и вообще аккумулируя апдейты
				c.pollDataFrames.Put(frameData)
				return frame, errors.Wrap(err, `Update flow control window sizes fail`)
			}
		}
		return frameData, nil

	case FrameTypeWindowUpdate:
		if frameHdr.Length != 4 {
			return frame, errors.Wrap(ErrWrongFramePayloadLength, `Wrong frame`)
		}

		frameWindowUpdate := WindowUpdateFrame{FrameHdr: frameHdr}
		frameWindowUpdate.WindowSizeIncrement = endianess.Uint32(payload[0:4])
		return &frameWindowUpdate, nil

	case FrameTypePing:
		if frameHdr.Length != 8 {
			return frame, errors.Wrap(ErrWrongFramePayloadLength, `Wrong frame`)
		}

		framePing := PingFrame{}
		copy(framePing.Data[:], payload[0:8])
		return &framePing, nil

	default:
		err = NewErrProtocol(`Unsupported frame type ` + frameHdr.Type.String())
		return frame, errors.Wrap(err, `Wrong frame`)
	}

	return
}

func (c *Connection) markFlowControlInsufficient() {
	// сохраняю первое срабатывание нехватки буфера, чтобы все последующие не сдвигали его
	atomic.CompareAndSwapInt64(&c.flowControlWindowEmptyFrom, 0, nowCached.Unix())
}

func (c *Connection) markFlowControlSufficient() {
	// хватило буферов для отправки - сбрасываю "время недостаточности окна"
	atomic.StoreInt64(&c.flowControlWindowEmptyFrom, 0)
}
