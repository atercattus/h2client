package h2client

type (
	respWaitItem struct {
		streamId uint32
		succ     bool
	}

	connectionStream struct {
		streamId          uint32
		conn              *Connection
		flowControlWindow int64 // ToDo: реализовать
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
	select {
	case <-s.respWait:
	default:
	}
}
