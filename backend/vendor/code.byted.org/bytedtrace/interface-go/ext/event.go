package ext

import (
	bt "code.byted.org/bytedtrace/interface-go"
	"time"
)

const (
	perfTEventType = "perfT"
)

var (
	ConnStartEvent = &perfTEvent{event: "ConnStart"}
	ConnEndEvent   = &perfTEvent{event: "ConnEnd"}

	SendStartEvent = &perfTEvent{event: "SendStart"}
	SendEndEvent   = &perfTEvent{event: "SendEnd"}

	TransportOpenEvent  = &perfTEvent{event: "TransportOpen"}
	TransportFlushEvent = &perfTEvent{event: "TransportFlush"}
	TransportCloseEvent = &perfTEvent{event: "TransportClose"}

	RecvStartEvent = &perfTEvent{event: "RecvStart"}
	RecvEndEvent   = &perfTEvent{event: "RecvEnd"}
)

type perfTEvent struct {
	event string
}

func (p *perfTEvent) Set(span bt.Span) {
	span.AddEvents(
		bt.NewEvent(perfTEventType, p.event).
			SetTimestamp(time.Now()).
			SetEmitLog(false).
			SetEmitMetrics(false),
	)
}

func (p *perfTEvent) SetWithTime(span bt.Span, t time.Time) {
	span.AddEvents(
		bt.NewEvent(perfTEventType, p.event).
			SetTimestamp(t).
			SetEmitLog(false).
			SetEmitMetrics(false),
	)
}
