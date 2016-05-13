package h2client

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"github.com/pkg/errors"
	"golang.org/x/net/http2/hpack"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type (
	Connection struct {
		host string
		port int

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
		flowControlWindow  int64 // это лимит удаленной стороны, который мы уменьшаем, когда МЫ отсылаем DATA фреймы, а не когда их присылают нам

		streamsActive   map[uint32]*connectionStream
		streamsActiveMu sync.RWMutex

		limitedReader io.LimitedReader
	}
)

var (
	clientConnectionPreface = []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
	endianess               = binary.BigEndian
)

func NewConnection(host string, port int) (*Connection, error) {
	conn, err := tls.Dial(`tcp`, host+`:`+strconv.Itoa(port), &tls.Config{NextProtos: []string{`h2`}})
	if err != nil {
		return nil, errors.Wrap(err, `TLS connect fail`)
	}

	settings := GetDefaultSettings()
	h2c := Connection{
		host:              host,
		port:              port,
		conn:              conn,
		connState:         ConnectionStateDisconn,
		lastStreamId:      1, // нечетные для клиента, 1 стрим пропускается.
		settings:          settings,
		flowControlWindow: int64(settings.InitialWindowSize),
		streamsActive:     make(map[uint32]*connectionStream),
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
		return nil, errors.Wrap(err, `Handshake fail`)
	}

	go h2c.reader()

	return &h2c, nil
}

func (c *Connection) writeChunks(chunks ...[]byte) error {
	c.connWriteMu.Lock()
	for _, chunk := range chunks {
		if _, err := c.conn.Write(chunk); err != nil { // ToDo: timeout
			c.connWriteMu.Unlock()
			return err
		}
	}
	c.connWriteMu.Unlock()
	return nil
}

func (c *Connection) readChunk(chunk []byte) error {
	c.connReadMu.Lock()
	_, err := io.ReadAtLeast(c.conn, chunk, len(chunk)) // ToDo: timeout
	c.connReadMu.Unlock()
	return err
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

func (c *Connection) Close() error {
	if c.connState == ConnectionStateClosed {
		return errors.Wrap(ErrConnectionAlreadyClosed, `Cannot close already closed connection`)
	}
	c.connState = ConnectionStateClosed

	c.hpackDecoder.Close()

	return c.conn.Close()
}

func (c *Connection) reader() {
	for c.connState != ConnectionStateClosed {
		frame, err := c.recvFrame()
		if err != nil {
			errors.Fprint(os.Stderr, err)
			return
		}

		frameHdr := frame.Hdr()

		if frameHdr.StreamId == 0 {
			// служебный фрейм
			switch frameHdr.Type {
			case FrameTypeSettings:
				c.settings.UpdateFromSettingsFrame(frame.(*SettingsFrame))
				c.hpackEncoder.SetMaxDynamicTableSizeLimit(c.settings.HeaderTableSize)

				if frame.Hdr().Flags&FlagAck == 0 {
					// не нужно отсылать ACK на пришедший ACK :)
					if err := c.sendFrame(FrameTypeSettings, FlagAck, 0, nil); err != nil {
						panic(errors.Wrap(err, `Cannot send answer to SETTINGS frame`))
					}
				}

				if c.connState == ConnectionStateWaitPrefaceSettings {
					c.connState = ConnectionStateOpened
				}

			case FrameTypeWindowUpdate:
				windowUpdateFrame := frame.(*WindowUpdateFrame)
				if windowUpdateFrame.StreamId == 0 {
					c.flowControlWindow += int64(windowUpdateFrame.WindowSizeIncrement)
				} else {
					// ToDo: обработка конкретного стрима
				}

			case FrameTypeGoaway:
				goawayFrame := frame.(*GoawayFrame)
				fmt.Printf("GOAWAY %v\n", goawayFrame)
				// ToDo: нужна нормальная обработка

			default:
				panic(`WTF for connection frame type ` + frameHdr.Type.String())
			}

			continue
		}

		c.streamsActiveMu.RLock()
		stream, ok := c.streamsActive[frameHdr.StreamId]
		c.streamsActiveMu.RUnlock()
		if !ok {
			fmt.Printf("unexpected frame recv: %v\n", frame)
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

		default:
			panic(`WTF for stream frame type ` + frameHdr.Type.String())
		}

		if frameHdr.Flags&FlagEndStream != 0 {
			c.streamsActiveMu.Lock()
			delete(c.streamsActive, frameHdr.StreamId)
			c.streamsActiveMu.Unlock()

			stream.respWait <- struct{}{}
		}
	}
}

func (c *Connection) Req(method string, path string, headers []HeaderPair) (*response, error) {
	if c.connState == ConnectionStateClosed {
		return nil, errors.Wrap(ErrConnectionAlreadyClosed, `Cannot work over closed connection`)
	}

	for c.connState != ConnectionStateOpened { // ToDo: сделать лучше
		time.Sleep(10 * time.Millisecond)
	}

	c.doMu.Lock()
	payload, err := c.buildRequestPayload(method, path, headers)
	if err != nil {
		c.doMu.Unlock()
		return nil, errors.Wrap(err, `buildRequestPayload fail`)
	}

	streamIdx := atomic.AddUint32(&c.lastStreamId, 2)
	stream := c.pollStream.Get().(*connectionStream)
	stream.Reset(c)

	c.streamsActiveMu.Lock()
	c.streamsActive[streamIdx] = stream
	c.streamsActiveMu.Unlock()

	// send request
	err = c.sendFrame(FrameTypeHeaders, FlagEndStream|FlagEndHeaders, streamIdx, payload.Bytes())
	if err != nil {
		c.doMu.Unlock()
		c.pollStream.Put(stream)
		return nil, errors.Wrap(err, `Send frame fail`)
	}
	c.doMu.Unlock()

	// recv response
	<-stream.respWait

	return &stream.resp, nil
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
			panic(`NIH`)
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
			panic(`NIH`)
		}

		frameData := c.pollDataFrames.Get().(*DataFrame)
		frameData.FrameHdr = frameHdr

		if frameHdr.Length > 0 {
			frameData.Data.Reset()
			frameData.Data.Write(payload[0:frameHdr.Length])

			if err := c.updateFlowWindows(frameHdr.Length, frameHdr.StreamId); err != nil { // ToDo: делать асинхронно
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

	default:
		err = NewErrProtocol(`Unsupported frame type ` + frameHdr.Type.String())
		return frame, errors.Wrap(err, `Wrong frame`)
	}

	return
}
