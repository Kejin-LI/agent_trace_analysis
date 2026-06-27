package bytedtracer

import "time"

const (
	// Event type
	LogEventType = "log"

	// Log level key in event's tags
	LogLevelKey = "_log_level"

	DefaultEventFactory = eventFactory(0)

	LevelNoDefine = -1
	LevelTrace    = 0
	LevelDebug    = 1
	LevelInfo     = 2
	LevelNotice   = 3
	LevelWarn     = 4
	LevelError    = 5
	LevelFatal    = 6
)

type Event interface {
	SetTimestamp(t time.Time) Event
	SetTag(key string, value interface{}) Event
	SetTags(tags ...TagKV) Event
	SetContent(content string) Event
	SetEmitLog(e bool) Event
	SetEmitMetrics(e bool) Event
	IncCallDepthForLog(depth int) Event
}

type EventFactory interface {
	NewInfoLogEvent(eventName string) Event
	NewWarnLogEvent(eventName string) Event
	NewErrorLogEvent(eventName string) Event
	NewFatalLogEvent(eventName string) Event
	NewPanicEvent(panicMsg string) Event
	NewEvent(eventType, eventName string) Event
}

type eventFactory byte

func newLogEvent(name string, logLevel int, emitMetrics bool) Event {
	return GlobalTracer().NewEvent(LogEventType, name, logLevel).SetEmitMetrics(emitMetrics).SetEmitLog(true)
}

func (eventFactory) NewInfoLogEvent(eventName string) Event {
	return newLogEvent(eventName, LevelInfo, false)
}

func (eventFactory) NewWarnLogEvent(eventName string) Event {
	return newLogEvent(eventName, LevelWarn, true)
}

func (eventFactory) NewErrorLogEvent(eventName string) Event {
	return newLogEvent(eventName, LevelError, true)
}

func (eventFactory) NewFatalLogEvent(eventName string) Event {
	return newLogEvent(eventName, LevelFatal, true)
}

func (eventFactory) NewPanicEvent(panicMsg string) Event {
	return newLogEvent("panic", LevelFatal, true).SetContent(panicMsg)
}

func (eventFactory) NewEvent(eventType, eventName string) Event {
	return GlobalTracer().NewEvent(eventType, eventName, LevelNoDefine)
}

func NewInfoLogEvent(eventName string) Event {
	return DefaultEventFactory.NewInfoLogEvent(eventName)
}

func NewWarnLogEvent(eventName string) Event {
	return DefaultEventFactory.NewWarnLogEvent(eventName)
}

func NewErrorLogEvent(eventName string) Event {
	return DefaultEventFactory.NewErrorLogEvent(eventName)
}

func NewFatalLogEvent(eventName string) Event {
	return DefaultEventFactory.NewFatalLogEvent(eventName)
}

func NewPanicEvent(panicMsg string) Event {
	return DefaultEventFactory.NewPanicEvent(panicMsg)
}

func NewEvent(eventType, eventName string) Event {
	return DefaultEventFactory.NewEvent(eventType, eventName)
}
