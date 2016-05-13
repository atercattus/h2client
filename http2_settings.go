package h2client

import "math"

// http://http2.github.io/http2-spec/#SettingValues

type (
	Settings struct {
		HeaderTableSize      uint32
		EnablePush           bool
		MaxConcurrentStreams uint32
		InitialWindowSize    uint32
		MaxFrameSize         uint32
		MaxHeaderListSize    uint32
	}
)

func GetDefaultSettings() Settings {
	return Settings{
		HeaderTableSize:      4096,
		EnablePush:           true,
		MaxConcurrentStreams: 512, // рекомендовано не менее 100
		InitialWindowSize:    65535,
		MaxFrameSize:         16384,
		MaxHeaderListSize:    math.MaxInt32, // unlimited
	}
}

func (s *Settings) UpdateFromSettingsFrame(frame *SettingsFrame) {
	for _, param := range frame.Params {
		switch param.Id {
		case SettingsHeaderTableSize:
			s.HeaderTableSize = param.Value
		case SettingsEnablePush:
			s.EnablePush = param.Value != 0
		case SettingsMaxConcurrentStreams:
			s.MaxConcurrentStreams = param.Value
		case SettingsInitialWindowSize:
			s.InitialWindowSize = param.Value
		case SettingsMaxFrameSize:
			s.MaxFrameSize = param.Value
		case SettingsMaxHeaderListSize:
			s.MaxHeaderListSize = param.Value
		}
	}
}
