package h2client

import "fmt"

type (
	SettingsType uint8
)

// http://http2.github.io/http2-spec/#iana-settings

const (
	SettingsHeaderTableSize      = SettingsType(0x1)
	SettingsEnablePush           = SettingsType(0x2)
	SettingsMaxConcurrentStreams = SettingsType(0x3)
	SettingsInitialWindowSize    = SettingsType(0x4)
	SettingsMaxFrameSize         = SettingsType(0x5)
	SettingsMaxHeaderListSize    = SettingsType(0x6)
)

func (s SettingsType) String() string {
	switch s {
	case SettingsHeaderTableSize:
		return `SettingsType(SETTINGS_HEADER_TABLE_SIZE)`
	case SettingsEnablePush:
		return `SettingsType(SETTINGS_ENABLE_PUSH)`
	case SettingsMaxConcurrentStreams:
		return `SettingsType(SETTINGS_MAX_CONCURRENT_STREAMS)`
	case SettingsInitialWindowSize:
		return `SettingsType(SETTINGS_INITIAL_WINDOW_SIZE)`
	case SettingsMaxFrameSize:
		return `SettingsType(SETTINGS_MAX_FRAME_SIZE)`
	case SettingsMaxHeaderListSize:
		return `SettingsType(SETTINGS_MAX_HEADER_LIST_SIZE)`
	default:
		return fmt.Sprintf(`SettingsType(%d)`, s)
	}
}
