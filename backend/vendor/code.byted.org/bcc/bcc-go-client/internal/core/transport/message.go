package transport

import (
	"context"
	"time"
)

type StreamCloseMsg struct {
	Ctx    context.Context
	Err    error
	Normal bool
	Dur    time.Duration
	stream *StreamClient
}

type TimerMsg struct {
	idx int64
}
