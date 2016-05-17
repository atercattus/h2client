package h2client

import "bytes"

type (
	response struct {
		stream *connectionStream

		Status   int
		Body     bytes.Buffer
		Headers  []HeaderPair
		Canceled bool
	}
)

func (r *response) Close() {
	r.stream.conn.pollStream.Put(r.stream)
}

func (r *response) Reset(stream *connectionStream) {
	r.stream = stream
	r.Status = 0
	r.Body.Reset()
	r.Headers = r.Headers[0:0]
	r.Canceled = false
}
