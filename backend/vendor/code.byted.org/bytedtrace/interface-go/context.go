package bytedtracer

import (
	"github.com/opentracing/opentracing-go"
)

type SpanContext interface {
	opentracing.SpanContext
	ToString() string
	ToStringW3C() string
	GetTraceId() TraceId
	IsDebug() bool
	Noop() bool
	GetSpanId() uint64
}
