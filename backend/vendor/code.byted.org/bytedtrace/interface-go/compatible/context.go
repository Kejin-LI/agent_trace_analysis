package compatible

import (
	bytedtrace "code.byted.org/bytedtrace/interface-go"
	"context"
	"github.com/opentracing/opentracing-go"
)

// TraceType describes the type of trace. eg. bytedtrace, opentracing
type TraceType int

const (
	TraceTypeNone TraceType = iota
	TraceTypeOnlyOpentracing
	TraceTypeBytedtrace
)

func TraceTypeFromContext(ctx context.Context) TraceType {
	ospan := opentracing.SpanFromContext(ctx)
	if ospan != nil {
		if _, ok := ospan.(bytedtrace.Span); ok {
			return TraceTypeBytedtrace
		}
		return TraceTypeOnlyOpentracing
	}
	return TraceTypeNone
}
