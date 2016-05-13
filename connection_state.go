package h2client

import "fmt"

type (
	ConnectionState uint8
)

const (
	ConnectionStateDisconn = ConnectionState(iota)
	ConnectionStateWaitPrefaceSettings
	ConnectionStateOpened
	ConnectionStateClosed
)

func (c ConnectionState) String() string {
	switch c {
	case ConnectionStateDisconn:
		return `ConnectionState(Disconn)`
	case ConnectionStateWaitPrefaceSettings:
		return `ConnectionState(WaitPrefaceSettings)`
	case ConnectionStateOpened:
		return `ConnectionState(Opened)`
	case ConnectionStateClosed:
		return `ConnectionState(Closed)`
	default:
		return fmt.Sprintf(`ConnectionState(%d)`, c)
	}
}
