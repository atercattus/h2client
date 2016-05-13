package h2client

import (
	"strconv"
	"strings"
)

type (
	FrameFlags uint8
)

const (
	FlagEndStream  = FrameFlags(0x1)
	FlagEndHeaders = FrameFlags(0x4)
	FlagPadded     = FrameFlags(0x8)
	FlagPriority   = FrameFlags(0x20)

	FlagAck = FrameFlags(0x1) // используется только для передачи ответа на SETTINGS фрейм
)

func (f FrameFlags) String() string {
	var flags []string
	if f&FlagEndStream != 0 {
		flags = append(flags, `EndStream_Ack`)
	}
	if f&FlagEndHeaders != 0 {
		flags = append(flags, `EndHeaders`)
	}
	if f&FlagPadded != 0 {
		flags = append(flags, `Padded`)
	}
	if f&FlagPriority != 0 {
		flags = append(flags, `Priority`)
	}
	if rest := f & (^(FlagEndStream | FlagEndHeaders | FlagPadded | FlagPriority)); rest != 0 {
		flags = append(flags, strconv.Itoa(int(rest)))
	}
	if len(flags) == 0 {
		flags = append(flags, `0`)
	}

	return `FrameFlags(` + strings.Join(flags, `|`) + `)`
}
