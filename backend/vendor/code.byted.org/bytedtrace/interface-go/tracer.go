package bytedtracer

import (
	"context"
	"github.com/opentracing/opentracing-go"
)

type Tracer interface {
	// Start server span for server in rpc call, this span type is 'server' as reserved.
	// `name`: local method name.
	// For example:
	// 			tracer.StartServerSpan(
	// 				ctx,
	// 				name)
	// If you want to start a span as parentSpan's child, use
	// 			tracer.StartServerSpan(
	// 				ctx,
	// 				name,
	// 				bytedtracer.ChildOf(parentSpan.GetContext()))
	StartServerSpan(ctx context.Context, name string, opts ...StartSpanOption) (Span, context.Context)

	// Start client span for client in rpc call, this span type is 'client' as reserved.
	// `name`: remote service name
	StartClientSpan(ctx context.Context, name string, opts ...StartSpanOption) (Span, context.Context)

	// Start custom span.
	// `spanType`: the type of this span. Can not be 'server' or 'client' because these type are reserved.
	// `name`: span's name.
	// For example:
	// 			tracer.StartServerSpan(
	// 				ctx,
	// 				name,
	// 				bytedtracer.EnableEmitSpanMetrics)
	StartCustomSpan(ctx context.Context, spanType, name string, opts ...StartSpanOption) (Span, context.Context)

	// Get span from context, will return nil if span not exit.
	GetSpanFromContext(ctx context.Context) Span

	// Batch add events into span in the ctx.
	AddEvents(ctx context.Context, events ...Event)

	// Emit a timer metrics use metrics client, then the metrics will be added to span in the ctx.
	// If there is no span in the ctx, metrics will be dropped after emit.
	EmitMetricsTimer(ctx context.Context, metricsName string, value float64, tagKv ...TagKV)

	// Emit a rate counter metrics use metrics client, then the metrics will be added to span in the ctx.
	// If there is no span in the ctx, metrics will be dropped after emit.
	EmitMetricsRateCounter(ctx context.Context, metricsName string, value int64, tagKv ...TagKV)

	// Emit a store metrics use metrics client, then the metrics will be added to span in the ctx.
	// If there is no span in the ctx, metrics will be dropped after emit.
	EmitMetricsStore(ctx context.Context, metricsName string, value float64, tagKv ...TagKV)

	// Emit a counter metrics use metrics client, then the metrics will be added to span in the ctx.
	// If there is no span in the ctx, metrics will be dropped after emit.
	EmitMetricsCounter(ctx context.Context, metricsName string, value int64, tagKv ...TagKV)

	// Emit a meter metrics use metrics client, then the metrics will be added to span in the ctx.
	// If there is no span in the ctx, metrics will be dropped after emit.
	EmitMetricsMeter(ctx context.Context, metricsName string, value int64, tagKv ...TagKV)

	// AddEventMetric Add event metric to specific event type
	AppendEventMetricTags(eventType string, tagName ...string) error

	// AppendSpanMetricTags Append extra metric tags to specific span metric.
	AppendSpanMetricTags(spanType string, extraTagName ...string) error

	// AppendCustomMetrics Append custom metrics, it is unsafe so please use it after the tracer initialize closely.
	AddCustomMetric(metricsName string, tagName ...string) error

	// Inject spanContext into carrier.
	// `format`: TextMap for rpc call/ HTTPHeader for http call.
	// `carrier`: is a map inside.
	// For example:
	// 		tracer.BytedInject(
	// 			context,
	// 			bytedtracer.TextMap,
	// 			bytedtracer.TextMapCarrier(make(map[string]string))
	BytedInject(ctx SpanContext, format, carrier interface{}) error

	// Extract spanContext from `carrier`.
	// `format`: TextMap for rpc call/ HTTPHeader for http call.
	// `carrier`: is a map inside.
	// For example:
	//		 spanContext, err := tracer.BytedExtract(
	// 		 bytedtracer.TextMap,
	// 		 bytedtracer.TextMapCarrier(make(map[string]string))
	BytedExtract(format, carrier interface{}) (SpanContext, error)

	// Opentracing interfaces.
	// Deprecated: use StartCustomSpan() instead.
	StartSpan(operationName string, opts ...opentracing.StartSpanOption) opentracing.Span

	// Deprecated: use BytedInject() instead.
	Inject(sm opentracing.SpanContext, format, carrier interface{}) error

	// Deprecated: use BytedExtract() instead.
	Extract(format, carrier interface{}) (opentracing.SpanContext, error)

	// Query internal status of tracer.
	GetStatus() (TracerStatus, error)

	// Close the tracer.
	// Attention:
	// 		- When you create a tracer, do not forget to close it.
	//		- When a tracer is closed, do not use it anymore.
	Close()

	// Get a span handler to set span's reserved values.
	// Use SetXXX(span,...) in ext.go instead.
	GetSpanHandler() SpanHandler

	// Returns a new event.
	// Use NewXXXEvent(eventName string) in event.go instead.
	NewEvent(t, name string, level int) Event
}

type TracerStatus interface {
	// Get reporting qps of transaction to logAgent.
	GetReportingQps() float64
	// Get reporting bps of transaction to logAgent.
	GetReportingBps() float64
	// Get the snapshot of tracer config.
	GetTracerConfigs() map[string]interface{}
}

// For client component metrics control.
type DynamicController interface {
	EnableThriftStyleMetrics(component string) bool
	EnableLightMode(component string) bool
	SpanMetricsLevel() int32
}

// TracerSynchronize
// When set a new tracer replace the global tracer, the new one should sync configs from the old one.
type TracerSynchronize interface {
	Synchronize(old Tracer)
}

// GlobalTagSetter.
type GlobalTagSetter interface {
	AddGlobalTags(tags []*GlobalTag) error
}
