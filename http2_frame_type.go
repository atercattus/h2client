package h2client

import "fmt"

type (
	FrameType uint8
)

// http://http2.github.io/http2-spec/#iana-frames

const (
	FrameTypeData         = FrameType(0)
	FrameTypeHeaders      = FrameType(1)
	FrameTypePriority     = FrameType(2)
	FrameTypeRstStream    = FrameType(3)
	FrameTypeSettings     = FrameType(4)
	FrameTypePushPromise  = FrameType(5)
	FrameTypePing         = FrameType(6)
	FrameTypeGoaway       = FrameType(7)
	FrameTypeWindowUpdate = FrameType(8)
	FrameTypeContinuation = FrameType(9)
)

func (f FrameType) String() string {
	switch f {
	case FrameTypeData:
		return `FrameType(DATA)`
	case FrameTypeHeaders:
		return `FrameType(HEADERS)`
	case FrameTypePriority:
		return `FrameType(PRIORITY)`
	case FrameTypeRstStream:
		return `FrameType(RST_STREAM)`
	case FrameTypeSettings:
		return `FrameType(SETTINGS)`
	case FrameTypePushPromise:
		return `FrameType(PUSH_PROMISE)`
	case FrameTypePing:
		return `FrameType(PING)`
	case FrameTypeGoaway:
		return `FrameType(GOAWAY)`
	case FrameTypeWindowUpdate:
		return `FrameType(WINDOW_UPDATE)`
	case FrameTypeContinuation:
		return `FrameType(CONTINUATION)`
	default:
		return fmt.Sprintf(`FrameType(%d)`, f)
	}
}
