package h2client

type (
	respWaitItem struct {
		streamId uint32
		succ     bool
	}

	connectionStream struct {
		id                uint32
		conn              *Connection
		flowControlWindow int64
		resp              response
		respWait          chan respWaitItem
	}
)

func newStream(conn *Connection) *connectionStream {
	stream := connectionStream{}
	stream.respWait = make(chan respWaitItem)
	stream.Reset(conn)
	return &stream
}

func (s *connectionStream) Reset(conn *Connection) {
	s.conn = conn
	s.resp.Reset(s)
	s.flowControlWindow = 65535
	select {
	case <-s.respWait:
	default:
	}
}
