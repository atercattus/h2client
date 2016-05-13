package h2client

import "bytes"

type (
	response struct {
		stream *connectionStream

		Status  int
		Body    bytes.Buffer
		Headers []HeaderPair
	}
)

func (rr *response) Close() {
	rr.stream.conn.pollStream.Put(rr.stream)
}
