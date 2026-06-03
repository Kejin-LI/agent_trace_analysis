package bytedtracer

import (
	"context"
	"errors"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
	"time"
)

const (
	defaultNoopTracer      = NoopTracer(0)
	defaultNoopSpanContext = NoopSpanContext(0)
	defaultNoopSpan        = NoopSpan(0)
	defaultNoopSpanHandler = noopSpanHandler(0)
	defaultNoopEvent       = noopEvent(0)
)

var (
	emptyTracerStatusErr = errors.New("tracer is noop so there is no tracer status")
)

type NoopTracer byte

func (NoopTracer) StartServerSpan(ctx context.Context, operationName string, opts ...StartSpanOption) (Span, context.Context) {
	return defaultNoopSpan, ctx
}

func (NoopTracer) StartClientSpan(ctx context.Context, target string, opts ...StartSpanOption) (Span, context.Context) {
	return defaultNoopSpan, ctx
}

func (NoopTracer) StartCustomSpan(ctx context.Context, spanType, name string, opts ...StartSpanOption) (Span, context.Context) {
	return defaultNoopSpan, ctx
}

func (NoopTracer) GetSpanFromContext(ctx context.Context) Span {
	return defaultNoopSpan
}

func (NoopTracer) AddEvents(ctx context.Context, events ...Event) {
}

func (NoopTracer) EmitMetricsTimer(ctx context.Context, metricsName string, value float64, tagKv ...TagKV) {
}

func (NoopTracer) EmitMetricsRateCounter(ctx context.Context, metricsName string, value int64, tagKv ...TagKV) {
}

func (NoopTracer) EmitMetricsStore(ctx context.Context, metricsName string, value float64, tagKv ...TagKV) {
}

func (NoopTracer) EmitMetricsCounter(ctx context.Context, metricsName string, value int64, tagKv ...TagKV) {
}

func (NoopTracer) EmitMetricsMeter(ctx context.Context, metricsName string, value int64, tagKv ...TagKV) {
}

func (NoopTracer) Close() {
}

func (NoopTracer) BytedInject(ctx SpanContext, format, carrier interface{}) error {
	return nil
}

func (NoopTracer) BytedExtract(format, carrier interface{}) (SpanContext, error) {
	return defaultNoopSpanContext, nil
}

func (NoopTracer) GetSpanHandler() SpanHandler {
	return defaultNoopSpanHandler
}

func (NoopTracer) NewEvent(t, name string, level int) Event {
	return defaultNoopEvent
}

func (NoopTracer) EnableThriftStyleMetrics(component string) bool {
	return true
}

func (NoopTracer) EnableLightMode(component string) bool {
	return false
}

func (NoopTracer) SpanMetricsLevel() int32 {
	return 0
}

// Opentracing.
func (NoopTracer) StartSpan(operationName string, opts ...opentracing.StartSpanOption) opentracing.Span {
	return defaultNoopSpan
}

func (NoopTracer) Inject(ctx opentracing.SpanContext, format, carrier interface{}) error {
	return nil
}

func (NoopTracer) Extract(format, carrier interface{}) (opentracing.SpanContext, error) {
	return defaultNoopSpanContext, nil
}

func (NoopTracer) GetStatus() (TracerStatus, error) {
	return nil, emptyTracerStatusErr
}

func (NoopTracer) AppendEventMetricTags(eventType string, tagName ...string) error {
	return nil
}

func (NoopTracer) AppendSpanMetricTags(spanType string, extraTagName ...string) error {
	return nil
}

func (NoopTracer) AddCustomMetric(metricsName string, tagName ...string) error {
	return nil
}

func (NoopTracer) Synchronize(old Tracer) {

}

type NoopSpan byte

func (NoopSpan) IsSampled() bool {
	return false
}

func (NoopSpan) AddEvents(events ...Event) {
}

func (NoopSpan) EmitMetricsTimer(metricsName string, value float64, tagKv ...TagKV) {
}

func (NoopSpan) EmitMetricsRateCounter(metricsName string, value int64, tagKv ...TagKV) {
}

func (NoopSpan) EmitMetricsStore(metricsName string, value float64, tagKv ...TagKV) {
}

func (NoopSpan) EmitMetricsCounter(metricsName string, value int64, tagKv ...TagKV) {
}

func (NoopSpan) EmitMetricsMeter(metricsName string, value int64, tagKv ...TagKV) {
}

func (NoopSpan) SetTags(kvs ...TagKV) {
}

func (NoopSpan) GetContext() SpanContext {
	return defaultNoopSpanContext
}

func (NoopSpan) SetPostTrace(context SpanContext) {
}

func (NoopSpan) Finish() {
}

func (NoopSpan) IsPostTrace() bool {
	return false
}

func (NoopSpan) FinishWithOptions(opts opentracing.FinishOptions) {
}

func (NoopSpan) Context() opentracing.SpanContext {
	return defaultNoopSpanContext
}

func (NoopSpan) SetOperationName(operationName string) opentracing.Span {
	return defaultNoopSpan
}

func (NoopSpan) SetTag(key string, value interface{}) opentracing.Span {
	return defaultNoopSpan
}

func (NoopSpan) LogFields(fields ...log.Field) {
}

func (NoopSpan) LogKV(alternatingKeyValues ...interface{}) {
}

func (NoopSpan) SetBaggageItem(restrictedKey, value string) opentracing.Span {
	return defaultNoopSpan
}

func (NoopSpan) BaggageItem(restrictedKey string) string {
	return ""
}

func (NoopSpan) Tracer() opentracing.Tracer {
	return defaultNoopTracer
}

func (NoopSpan) LogEvent(event string) {
}

func (NoopSpan) LogEventWithPayload(event string, payload interface{}) {
}

func (NoopSpan) Log(data opentracing.LogData) {
}

func (NoopSpan) RecordTag(key string, value interface{}) {
}

func (NoopSpan) RecordLogFields(fields ...log.Field) {
}

func (NoopSpan) RecordChild(peerService string, startOpts []opentracing.StartSpanOption, finishOpts opentracing.FinishOptions) {
}

func (NoopSpan) GetStartTime() time.Time {
	return time.Time{}
}

func (NoopSpan) SetFinishTime(time.Time) {

}

func (NoopSpan) GetID() uint64 {
	return 0
}

type NoopSpanContext byte

func (n NoopSpanContext) ToString() string {
	return ""
}

func (n NoopSpanContext) ToStringW3C() string {
	return ""
}

func (n NoopSpanContext) ForeachBaggageItem(handler func(k, v string) bool) {
}

func (n NoopSpanContext) Noop() bool {
	return true
}

func (n NoopSpanContext) GetTraceId() TraceId {
	return TraceId{}
}

func (n NoopSpanContext) GetSpanId() uint64 {
	return 0
}

func (n NoopSpanContext) getParentId() uint64 {
	return 0
}

func (n NoopSpanContext) getFlags() byte {
	return 0
}

func (n NoopSpanContext) IsDebug() bool {
	return false
}

func (n NoopSpanContext) getDebugInfo() string {
	return ""
}

func (n NoopSpanContext) baggageItem(key string) string {
	return ""
}

func (n NoopSpanContext) getBaggage() map[string]string {
	return nil
}

type noopSpanHandler byte

func (noopSpanHandler) SetConsumerGroup(sp Span, consumerGroup string) {
}

func (noopSpanHandler) SetProducerService(sp Span, producerService string) {
}

func (noopSpanHandler) SetProducerCluster(sp Span, producerCluster string) {
}

func (noopSpanHandler) SetProducerAddr(sp Span, producerAddr string) {
}

func (noopSpanHandler) SetProducerDc(sp Span, producerDc string) {
}

func (noopSpanHandler) SetProduceTs(sp Span, produceTs time.Time) {
}

func (noopSpanHandler) SetMqPartition(sp Span, mqPartition string) {
}

func (noopSpanHandler) SetMqLag(sp Span, mqLag time.Duration) {
}

func (noopSpanHandler) SetStatusCode(sp Span, value int32) {
}

func (noopSpanHandler) SetSpanName(sp Span, value string) {
}

func (noopSpanHandler) SetIsError(sp Span, value bool) {
}

func (noopSpanHandler) SetBusinessStatusCode(sp Span, businessStatusCode int32) {
}

func (noopSpanHandler) SetSendSize(sp Span, sendSize int64) {
}

func (noopSpanHandler) SetRecvSize(sp Span, recvSize int64) {
}

func (noopSpanHandler) SetServiceType(sp Span, serviceType string) {
}

func (noopSpanHandler) SetComponent(sp Span, component string) {
}

func (noopSpanHandler) SetFromService(sp Span, fromService string) {
}

func (noopSpanHandler) SetFromMethod(sp Span, fromMethod string) {
}

func (noopSpanHandler) SetFromCluster(sp Span, fromCluster string) {
}

func (noopSpanHandler) SetFromAddr(sp Span, addr interface{}) {
}

func (noopSpanHandler) SetFromDc(sp Span, fromDc string) {
}

func (noopSpanHandler) SetToMethod(sp Span, toMethod string) {
}

func (noopSpanHandler) SetToCluster(sp Span, toCluster string) {
}

func (noopSpanHandler) SetToAddr(sp Span, addr interface{}) {
}

func (noopSpanHandler) SetToDc(sp Span, toDc string) {
}

func (noopSpanHandler) IncrExecCount(sp Span, cnt int32, success bool) {
}

func (noopSpanHandler) RecordOneExec(sp Span, duration time.Duration, success bool) {
}

func (noopSpanHandler) SamplingTrace(sp Span, opt ...SamplingTraceOption) {

}

func (noopSpanHandler) IsBackwardPropagate(sp Span) bool {
	return false
}

func (noopSpanHandler) IsForwardPropagate(sp Span) bool {
	return false
}

func (noopSpanHandler) IsReported(sp Span) bool {
	return false
}

func (noopSpanHandler) IsNoopSpan(sp Span) bool {
	if _, ok := sp.(NoopSpan); ok {
		return true
	}
	return false
}

type noopEvent byte

func (noopEvent) SetTimestamp(t time.Time) Event {
	return defaultNoopEvent
}

func (noopEvent) SetTag(key string, value interface{}) Event {
	return defaultNoopEvent
}

func (noopEvent) SetTags(tags ...TagKV) Event {
	return defaultNoopEvent
}

func (noopEvent) SetContent(content string) Event {
	return defaultNoopEvent
}

func (noopEvent) SetEmitLog(e bool) Event {
	return defaultNoopEvent
}

func (noopEvent) SetEmitMetrics(e bool) Event {
	return defaultNoopEvent
}

func (noopEvent) IncCallDepthForLog(depth int) Event {
	return defaultNoopEvent
}
