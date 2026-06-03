package bytedtracer

import "time"

type SpanHandler interface {
	// For serverSpan and clientSpan
	SetStatusCode(sp Span, value int32)
	SetSpanName(sp Span, value string)
	SetIsError(sp Span, value bool)
	SetBusinessStatusCode(sp Span, businessStatusCode int32)
	SetSendSize(sp Span, sendSize int64)
	SetRecvSize(sp Span, recvSize int64)
	SetServiceType(sp Span, serviceType string)
	SetComponent(sp Span, component string)
	// Just for serverSpan
	SetFromService(sp Span, fromService string)
	SetFromMethod(sp Span, fromMethod string)
	SetFromCluster(sp Span, fromCluster string)
	SetFromAddr(sp Span, addr interface{})
	SetFromDc(sp Span, fromDc string)
	// Just for clientSpan
	SetToMethod(sp Span, toMethod string)
	SetToCluster(sp Span, toCluster string)
	SetToAddr(sp Span, addr interface{})
	SetToDc(sp Span, toDc string)
}

type ConsumerSpanHandler interface {
	SetConsumerGroup(sp Span, consumerGroup string)
	SetProducerService(sp Span, producerService string)
	SetProducerCluster(sp Span, producerCluster string)
	SetProducerAddr(sp Span, producerAddr string)
	SetProducerDc(sp Span, producerDc string)
	SetProduceTs(sp Span, produceTs time.Time)
	SetMqPartition(sp Span, mqPartition string)
	SetMqLag(sp Span, mqLag time.Duration)
}

type BatchSpanHandler interface {
	IncrExecCount(sp Span, cnt int32, success bool)
	RecordOneExec(sp Span, duration time.Duration, success bool)
}

type SamplingTraceHandler interface {
	SamplingTrace(sp Span, opt ...SamplingTraceOption)
	IsBackwardPropagate(sp Span) bool
	IsForwardPropagate(sp Span) bool
	IsReported(sp Span) bool
}

type SpanTypeCheckHandler interface {
	IsNoopSpan(sp Span) bool
}
