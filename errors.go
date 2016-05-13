package h2client

type (
	Http2Error interface {
		Http2Error() string
	}

	// ErrProtocol реализует Http2Error
	ErrProtocol string
)

const (
	ErrWrongFramePayloadLength = ErrProtocol(`Wrong frame payload length`)
	ErrHPACKDecodeFail         = ErrProtocol(`Cannot decode HPACK headers`)
	ErrConnectionAlreadyClosed = ErrProtocol(`Connection already closed`)
)

func NewErrProtocol(s string) error {
	err := ErrProtocol(s)
	return &err
}

func (e ErrProtocol) Http2Error() string {
	return `Protocol error`
}

func (e ErrProtocol) Error() string {
	return e.Http2Error() + `: ` + string(e)
}
