package h2client

type (
	connectionStream struct {
		conn     *Connection
		resp     response
		respWait chan struct{}
	}
)

func newStream(conn *Connection) *connectionStream {
	stream := connectionStream{}
	stream.respWait = make(chan struct{})
	stream.Reset(conn)
	return &stream
}

func (s *connectionStream) Reset(conn *Connection) {
	s.conn = conn
	s.resp.stream = s
	s.resp.Status = 0
	s.resp.Body.Reset()
	s.resp.Headers = s.resp.Headers[0:0]
	select {
	case <-s.respWait:
	default:
	}
}
