package h2client

type (
	Http2Error interface {
		Http2Error() string
	}

	// ErrProtocol реализует Http2Error
	ErrProtocol string

	// ErrCommon реализует Http2Error
	ErrCommon string
)

const (
	ErrWrongFramePayloadLength = ErrProtocol(`Wrong frame payload length`)
	ErrHPACKDecodeFail         = ErrProtocol(`Cannot decode HPACK headers`)
	ErrConnectionAlreadyClosed = ErrProtocol(`Connection already closed`)

	ErrPoolCapacityLimit = ErrCommon(`Pool capacity limit`)
	ErrGoAwayRecieved    = ErrCommon(`GOAWAY frame received on connection`)
	ErrBug               = ErrCommon(`Bug`)
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

func NewErrInternal(s string) error {
	err := ErrCommon(s)
	return &err
}

func (e ErrCommon) Http2Error() string {
	return `Error`
}

func (e ErrCommon) Error() string {
	return string(e)
}
