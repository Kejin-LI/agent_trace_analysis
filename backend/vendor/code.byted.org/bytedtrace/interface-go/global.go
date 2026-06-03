package bytedtracer

import (
	"context"
	"sync"

	"github.com/opentracing/opentracing-go"
)

const postTraceRecorderCtxKey = "PostTraceRecorder"

type registerTracer struct {
	tracer   Tracer
	register bool
}

type predefine struct {
	eventMetricsTags          map[string][]string
	spanMetricsTags           map[string][]string
	customMetrics             map[string][]string
	consumerSpanTagRegistered bool
	lock                      sync.RWMutex
}

var (
	globalTracer  = registerTracer{defaultNoopTracer, false}
	predefineTags = predefine{}
)

// Set global tracer.
// Attention: will replace the original one so be careful.
func SetGlobalTracer(tracer Tracer) {
	predefineTags.lock.Lock()
	defer predefineTags.lock.Unlock()

	for key, tags := range predefineTags.eventMetricsTags {
		_ = tracer.AppendEventMetricTags(key, tags...)
	}

	for key, tags := range predefineTags.spanMetricsTags {
		_ = tracer.AppendSpanMetricTags(key, tags...)
	}

	for key, tags := range predefineTags.customMetrics {
		_ = tracer.AddCustomMetric(key, tags...)
	}

	if predefineTags.consumerSpanTagRegistered {
		_ = registerMqConsumerSpanForMetrics(tracer)
	}

	if syn, ok := tracer.(TracerSynchronize); ok {
		syn.Synchronize(globalTracer.tracer)
	}

	globalTracer.tracer.Close()
	globalTracer = registerTracer{tracer, true}
	// 兼容1.0的接口,设置opentracing的全局tracer
	if !opentracing.IsGlobalTracerRegistered() {
		opentracing.SetGlobalTracer(tracer)
	}
}

// Get the global tracer.
func GlobalTracer() Tracer {
	return globalTracer.tracer
}

// Return whether global tracer is registered.
// If not, will return degraded tracer.
func IsGlobalTracerRegistered() bool {
	return globalTracer.register && opentracing.IsGlobalTracerRegistered()
}

// Get span from context, will return nil if span not exit.
func GetSpanFromContext(ctx context.Context) Span {
	return globalTracer.tracer.GetSpanFromContext(ctx)
}

// Start server span for server in rpc call.
// `name`: remote method name.
// For example:
// 			tracer.StartServerSpan(
// 				ctx,
// 				operationName)
// If you want to start a span as parentSpan's child, use
// 			tracer.StartServerSpan(
// 				ctx,
// 				name,
// 				bytedtracer.ChildOf(parentSpan.GetContext()))
func StartServerSpan(ctx context.Context, operationName string, opts ...StartSpanOption) (Span, context.Context) {
	return globalTracer.tracer.StartServerSpan(ctx, operationName, opts...)
}

// Start client span for client in rpc call.
// `name`: remote service name.
func StartClientSpan(ctx context.Context, name string, opts ...StartSpanOption) (Span, context.Context) {
	return globalTracer.tracer.StartClientSpan(ctx, name, opts...)
}

// Start custom span.
// `spanType`: the type of this span.
// `name`: span's name.
// For example:
// 			tracer.StartServerSpan(
// 				ctx,
// 				operationName,
// 				bytedtracer.EnableEmitSpanMetrics)
func StartCustomSpan(ctx context.Context, spanType, name string, opts ...StartSpanOption) (Span, context.Context) {
	return globalTracer.tracer.StartCustomSpan(ctx, spanType, name, opts...)
}

// Batch add events into span in the ctx.
func AddEvents(ctx context.Context, events ...Event) {
	for _, e := range events {
		e.IncCallDepthForLog(1)
	}
	globalTracer.tracer.AddEvents(ctx, events...)
}

// Emit a timer metrics use metrics client, then the metrics will be added to span in the ctx.
// If there is no span in the ctx, metrics will be dropped after emit.
func EmitMetricsTimer(ctx context.Context, metricsName string, value float64, tagKv ...TagKV) {
	globalTracer.tracer.EmitMetricsTimer(ctx, metricsName, value, tagKv...)
}

// Emit a rate counter metrics use metrics client, then the metrics will be added to span in the ctx.
// If there is no span in the ctx, metrics will be dropped after emit.
func EmitMetricsRateCounter(ctx context.Context, metricsName string, value int64, tagKv ...TagKV) {
	globalTracer.tracer.EmitMetricsRateCounter(ctx, metricsName, value, tagKv...)
}

// Emit a store metrics use metrics client, then the metrics will be added to span in the ctx.
// If there is no span in the ctx, metrics will be dropped after emit.
func EmitMetricsStore(ctx context.Context, metricsName string, value float64, tagKv ...TagKV) {
	globalTracer.tracer.EmitMetricsStore(ctx, metricsName, value, tagKv...)
}

// Emit a counter metrics use metrics client, then the metrics will be added to span in the ctx.
// If there is no span in the ctx, metrics will be dropped after emit.
func EmitMetricsCounter(ctx context.Context, metricsName string, value int64, tagKv ...TagKV) {
	globalTracer.tracer.EmitMetricsCounter(ctx, metricsName, value, tagKv...)
}

// Emit a meter metrics use metrics client, then the metrics will be added to span in the ctx.
// If there is no span in the ctx, metrics will be dropped after emit.
func EmitMetricsMeter(ctx context.Context, metricsName string, value int64, tagKv ...TagKV) {
	globalTracer.tracer.EmitMetricsMeter(ctx, metricsName, value, tagKv...)
}

// Deprecated: Do not use this func, unless you know what you are doing.
func StartServerSpanWithPostTraceRecorder(ctx context.Context, operationName string, opts ...StartSpanOption) (sp Span, newctx context.Context) {
	sp, newctx = globalTracer.tracer.StartServerSpan(ctx, operationName, opts...)
	if sp == nil || sp.IsSampled() {
		return
	}
	newctx = context.WithValue(newctx, postTraceRecorderCtxKey, sp)
	return
}

// AppendEventMetricTags Add event metric to specific event type
func AppendEventMetricTags(eventType string, extraTagNames ...string) error {
	predefineTags.lock.Lock()
	defer predefineTags.lock.Unlock()
	if predefineTags.eventMetricsTags == nil {
		predefineTags.eventMetricsTags = make(map[string][]string)
	}

	predefineTags.eventMetricsTags[eventType] = MergeSlice(predefineTags.eventMetricsTags[eventType], extraTagNames)

	return globalTracer.tracer.AppendEventMetricTags(eventType, extraTagNames...)
}

// AppendSpanMetricTags Append extra metric tags to specific span metric.
func AppendSpanMetricTags(spanType string, extraTagNames ...string) error {
	predefineTags.lock.Lock()
	defer predefineTags.lock.Unlock()
	if predefineTags.spanMetricsTags == nil {
		predefineTags.spanMetricsTags = make(map[string][]string)
	}

	predefineTags.spanMetricsTags[spanType] = MergeSlice(predefineTags.spanMetricsTags[spanType], extraTagNames)

	return globalTracer.tracer.AppendSpanMetricTags(spanType, extraTagNames...)
}

// RegisterMqConsumerSpanForMetrics registers consumer span tags for metrics emitting
func RegisterMqConsumerSpanForMetrics() error {
	predefineTags.lock.Lock()
	defer predefineTags.lock.Unlock()

	predefineTags.consumerSpanTagRegistered = true

	return registerMqConsumerSpanForMetrics(GlobalTracer())
}

func registerMqConsumerSpanForMetrics(tracer Tracer) error {
	return tracer.AppendSpanMetricTags(ServerSpanType, consumerGroupKey, producerServiceKey,
		producerClusterKey, producerDcKey, mqPartitionKey)
}

// RegisterCustomMetric register custom metrics, it is unsafe so please use it after the tracer initialize closely.
func RegisterCustomMetric(metricsName string, tagNames ...string) error {
	predefineTags.lock.Lock()
	defer predefineTags.lock.Unlock()
	if predefineTags.customMetrics == nil {
		predefineTags.customMetrics = make(map[string][]string)
	}
	predefineTags.customMetrics[metricsName] = tagNames
	return globalTracer.tracer.AddCustomMetric(metricsName, tagNames...)
}

// Inject spanContext into carrier.
// `format`: TextMap for rpc call/ HTTPHeader for http call.
// `carrier`: always a map.
// For example:
// 		bytedtracer.Inject(
// 			context,
// 			bytedtracer.TextMap,
// 			bytedtracer.TextMapCarrier(make(map[string]string))
func Inject(ctx SpanContext, format interface{}, carrier interface{}) error {
	return globalTracer.tracer.BytedInject(ctx, format, carrier)
}

// Extract spanContext from `carrier`.
// `format`: TextMap for rpc call/ HTTPHeader for http call.
// `carrier`: always a map.
// For example:
//		 spanContext, err := bytedtracer.Extract(
// 		 bytedtracer.TextMap,
// 		 bytedtracer.TextMapCarrier(make(map[string]string))
func Extract(format interface{}, carrier interface{}) (SpanContext, error) {
	return globalTracer.tracer.BytedExtract(format, carrier)
}

// Add an info log event to the span in ctx and gen an info log.
func AddInfoLogEvent(ctx context.Context, eventName, content string, tags ...TagKV) {
	globalTracer.tracer.AddEvents(ctx, NewInfoLogEvent(eventName).SetTags(tags...).SetContent(content).
		IncCallDepthForLog(1))
}

// Add a warn log event to the span in ctx and gen a warn log.
func AddWarnLogEvent(ctx context.Context, eventName, content string, tags ...TagKV) {
	globalTracer.tracer.AddEvents(ctx, NewWarnLogEvent(eventName).SetTags(tags...).SetContent(content).
		IncCallDepthForLog(1))
}

// Add an error log event to the span in ctx and gen an error log.
func AddErrorLogEvent(ctx context.Context, eventName, content string, tags ...TagKV) {
	globalTracer.tracer.AddEvents(ctx, NewErrorLogEvent(eventName).SetTags(tags...).SetContent(content).
		IncCallDepthForLog(1))
}

// Add a fatal log event to the span in ctx and gen a fatal log.
func AddFatalLogEvent(ctx context.Context, eventName, content string, tags ...TagKV) {
	globalTracer.tracer.AddEvents(ctx, NewFatalLogEvent(eventName).SetTags(tags...).SetContent(content).
		IncCallDepthForLog(1))
}

// Add an panic log event to the span in ctx and gen a fatal log.
func AddPanicEvent(ctx context.Context, panicMsg string, tags ...TagKV) {
	globalTracer.tracer.AddEvents(ctx, NewPanicEvent(panicMsg).SetTags(tags...).IncCallDepthForLog(1))
}

// Add an custom event to the span in ctx but not gen log.
func AddEvent(ctx context.Context, eventType, eventName, content string, tags ...TagKV) {
	globalTracer.tracer.AddEvents(ctx, NewEvent(eventType, eventName).SetTags(tags...).SetContent(content))
}

// You can inject span into context using this method.
func ContextWithSpan(ctx context.Context, span Span) context.Context {
	// Inject span_id into ctx to link trace with app logs
	ctx = context.WithValue(ctx, SpanIDKey, span.GetID())

	// Adapt opentracing
	return opentracing.ContextWithSpan(ctx, span)
}

func getSpanHandler() SpanHandler {
	return globalTracer.tracer.GetSpanHandler()
}

// Return whether this component should emit thrift style metrics. Using dynamic control.
func EnableThriftStyleMetrics(component string) bool {
	if t, ok := globalTracer.tracer.(DynamicController); ok {
		return t.EnableThriftStyleMetrics(component)
	}
	return true
}

// Return whether this component should enable light mode. Using dynamic control.
func EnableLightMode(component string) bool {
	if t, ok := globalTracer.tracer.(DynamicController); ok {
		return t.EnableLightMode(component)
	}
	return false
}

// Return span metrics level. Using dynamic control.
func GetSpanMetricsLevel() int32 {
	if t, ok := globalTracer.tracer.(DynamicController); ok {
		return t.SpanMetricsLevel()
	}
	return 0
}
