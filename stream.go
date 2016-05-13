package h2client

import (
	"bytes"
)

type (
	Stream struct {
		// state int // idle, open, half-closed (x2), closed
		conn           *Connection
		responseStatus int
		contentLength  int64
		responseBody   bytes.Buffer
		headers        []HeaderPair
		respWait       chan struct{}
	}
)

func newStream(conn *Connection) *Stream {
	stream := Stream{}
	stream.respWait = make(chan struct{})
	stream.Reset(conn)
	return &stream
}

func (s *Stream) Reset(conn *Connection) {
	s.conn = conn
	s.responseStatus = 0
	s.contentLength = -1
	s.responseBody.Reset()
	s.headers = s.headers[0:0]
	select {
	case <-s.respWait:
	default:
	}
}
