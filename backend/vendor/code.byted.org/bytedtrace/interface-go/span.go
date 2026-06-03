package bytedtracer

import (
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
	"time"
)

const (
	// Reserved span type
	ServerSpanType = "server"
	ClientSpanType = "client"
)

type Span interface {
	// Return whether the span is sampled.
	IsSampled() bool

	// Batch add events.
	AddEvents(events ...Event)

	// Emit a timer metrics use metrics client, then the metrics will be added to span.
	EmitMetricsTimer(metricsName string, value float64, tagKv ...TagKV)

	// Emit a rate counter metrics use metrics client, then the metrics will be added to span.
	EmitMetricsRateCounter(metricsName string, value int64, tagKv ...TagKV)

	// Emit a store metrics use metrics client, then the metrics will be added to span.
	EmitMetricsStore(metricsName string, value float64, tagKv ...TagKV)

	// Emit a counter metrics use metrics client, then the metrics will be added to span.
	EmitMetricsCounter(metricsName string, value int64, tagKv ...TagKV)

	// Emit a meter metrics use metrics client, then the metrics will be added to span.
	EmitMetricsMeter(metricsName string, value int64, tagKv ...TagKV)

	// Set tags into the span.
	// For example:
	// span.SetTags(
	// 		bytedtracer.NewTagKV("key0","value0"),
	//      bytedtracer.NewTagKV("key1","value1")
	// )
	SetTags(kvs ...TagKV)

	// Get the spanContext of this span.
	GetContext() SpanContext

	// Set postTrace, learn more about postTrace: https://bytedance.feishu.cn/docs/doccnr0mfPQpRp3UP1pWI2Y919g#FhF6iz.
	// `context`: if this span is the first node, context should be nil; else context should be child span's spanContext.
	SetPostTrace(context SpanContext)

	// Finish a span.
	// Attention:
	//		- This method should only call once.
	// 		- When you create a span, do not forget to finish it.
	//		- When a span is finished, do not use it anymore.
	Finish()

	// Return whether the span is postTraced during span's life cycle.
	// Attention: if this span's parent span is postTraced, span.IsPostTrace() may not return true.
	IsPostTrace() bool

	// Opentracing interfaces.
	// Attention: this method should only call once.
	// Deprecated: if you want to set finishTime, use span.SetFinishTime() instead.
	FinishWithOptions(opts opentracing.FinishOptions)

	// Deprecated : use GetContext() instead.
	Context() opentracing.SpanContext

	// Deprecated: this method will not change span name anymore!
	SetOperationName(operationName string) opentracing.Span

	// Set a tag into the span.
	SetTag(key string, value interface{}) opentracing.Span

	// Deprecated: Use span.AddLogEvent() instead.
	LogFields(fields ...log.Field)

	// Deprecated: Use span.AddLogEvent() instead.
	LogKV(alternatingKeyValues ...interface{})

	// Set a key-value into span's baggage.
	SetBaggageItem(restrictedKey, value string) opentracing.Span

	// Get the key-value of the span's baggage.
	BaggageItem(restrictedKey string) string

	// Return span's tracer.
	Tracer() opentracing.Tracer

	// Deprecated: Use span.AddLogEvent() instead.
	LogEvent(event string)

	// Deprecated: use LogFields or LogKV.
	LogEventWithPayload(event string, payload interface{})

	// Deprecated: use LogFields or LogKV.
	Log(data opentracing.LogData)

	// Deprecated: compatible with PostTraceRecorder.
	RecordTag(key string, value interface{})

	// Use for postTrace of 1.0.
	RecordLogFields(fields ...log.Field)

	// Use for postTrace of 1.0.
	RecordChild(peerService string, startOpts []opentracing.StartSpanOption, finishOpts opentracing.FinishOptions)

	// Return span's startTime.
	GetStartTime() time.Time

	// Set span's finishTime.
	SetFinishTime(time.Time)

	// Get Span id.
	GetID() uint64
}
