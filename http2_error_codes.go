package h2client

import "fmt"

type (
	ErrorCode uint32
)

// http://http2.github.io/http2-spec/#iana-errors

const (
	ErrorCodeNoError            = ErrorCode(0x0)
	ErrorCodeProtocolError      = ErrorCode(0x1)
	ErrorCodeInternalError      = ErrorCode(0x2)
	ErrorCodeFlowControlError   = ErrorCode(0x3)
	ErrorCodeSettingsTimeout    = ErrorCode(0x4)
	ErrorCodeStreamClosed       = ErrorCode(0x5)
	ErrorCodeFrameSizeError     = ErrorCode(0x6)
	ErrorCodeRefusedStream      = ErrorCode(0x7)
	ErrorCodeCancel             = ErrorCode(0x8)
	ErrorCodeCompressionError   = ErrorCode(0x9)
	ErrorCodeConnectError       = ErrorCode(0xa)
	ErrorCodeEnhanceYourCalm    = ErrorCode(0xb)
	ErrorCodeInadequateSecurity = ErrorCode(0xc)
	ErrorCodeHTTP11Required     = ErrorCode(0xd)
)

func (e ErrorCode) String() string {
	switch e {
	case ErrorCodeNoError:
		return `ErrorCode(NO_ERROR)`
	case ErrorCodeProtocolError:
		return `ErrorCode(PROTOCOL_ERROR)`
	case ErrorCodeInternalError:
		return `ErrorCode(INTERNAL_ERROR)`
	case ErrorCodeFlowControlError:
		return `ErrorCode(FLOW_CONTROL_ERROR)`
	case ErrorCodeSettingsTimeout:
		return `ErrorCode(SETTINGS_TIMEOUT)`
	case ErrorCodeStreamClosed:
		return `ErrorCode(STREAM_CLOSED)`
	case ErrorCodeFrameSizeError:
		return `ErrorCode(FRAME_SIZE_ERROR)`
	case ErrorCodeRefusedStream:
		return `ErrorCode(REFUSED_STREAM)`
	case ErrorCodeCancel:
		return `ErrorCode(CANCEL)`
	case ErrorCodeCompressionError:
		return `ErrorCode(COMPRESSION_ERROR)`
	case ErrorCodeConnectError:
		return `ErrorCode(CONNECT_ERROR)`
	case ErrorCodeEnhanceYourCalm:
		return `ErrorCode(ENHANCE_YOUR_CALM)`
	case ErrorCodeInadequateSecurity:
		return `ErrorCode(INADEQUATE_SECURITY)`
	case ErrorCodeHTTP11Required:
		return `ErrorCode(HTTP_1_1_REQUIRED)`
	default:
		return fmt.Sprintf(`ErrorCode(%d)`, e)
	}
}
