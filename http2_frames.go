package h2client

import (
	"bytes"
	"fmt"
)

type (
	HeaderPair struct {
		Key   string
		Value string
	}

	Frame interface {
		Hdr() FrameHdr
	}

	FrameHdr struct {
		Length uint32
		Type   FrameType
		Flags  FrameFlags
		//R        bool
		StreamId uint32
		//Payload []byte
	}

	DataFrame struct { // http://http2.github.io/http2-spec/#DATA
		FrameHdr
		Data *bytes.Buffer
	}

	HeadersFrame struct { // http://http2.github.io/http2-spec/#HEADERS
		FrameHdr
		//Exclusive           bool
		//StreamIdDepend      uint32
		//Weight              uint8
		HeaderBlockFragment []HeaderPair
	}

	PriorityFrame struct { // http://http2.github.io/http2-spec/#PRIORITY
		FrameHdr
		Exclusive      bool
		StreamIdDepend uint32
		Weight         uint8
	}

	RstStreamFrame struct { // http://http2.github.io/http2-spec/#RST_STREAM
		FrameHdr
		// FrameHdr.StreamId содержит поток с ошибкой
		ErrorCode ErrorCode
	}

	SettingsFrame struct { // http://http2.github.io/http2-spec/#SETTINGS
		FrameHdr
		Params []SettingsFrameParam
	}

	SettingsFrameParam struct { // http://http2.github.io/http2-spec/#SettingFormat
		Id    SettingsType
		Value uint32
	}

	PushPromiseFrame struct { // http://http2.github.io/http2-spec/#PUSH_PROMISE
		FrameHdr
		//R bool
		StreamIdPromised    uint32
		HeaderBlockFragment []HeaderPair
	}

	PingFrame struct { // http://http2.github.io/http2-spec/#PING
		FrameHdr
		Data uint64
	}

	GoawayFrame struct { // http://http2.github.io/http2-spec/#GOAWAY
		FrameHdr
		//R bool
		LastStreamId uint32 // последний успешно полученный стрим
		ErrorCode    ErrorCode
		DebugData    []byte
	}

	WindowUpdateFrame struct { // http://http2.github.io/http2-spec/#WINDOW_UPDATE
		FrameHdr
		//R bool
		WindowSizeIncrement uint32
	}

	ContinuationFrame struct { // http://http2.github.io/http2-spec/#CONTINUATION
		FrameHdr
		HeaderBlockFragment []HeaderPair
	}
)

func (fh FrameHdr) Hdr() FrameHdr {
	return fh
}

func (fh FrameHdr) String() string {
	return fmt.Sprintf(`FrameHdr{Length:%d, Type:%s, Flags:%s, StreamId:%d}`, fh.Length, fh.Type, fh.Flags, fh.StreamId)
}
