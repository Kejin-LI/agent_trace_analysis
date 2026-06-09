package bytedtracer

import (
	"fmt"
	"os"
	"time"
)

const (
	errorMessageKey    = "_err_msg"
	consumerGroupKey   = "_consumer_group"
	producerServiceKey = "_producer_service"
	producerClusterKey = "_producer_cluster"
	producerDcKey      = "_producer_dc"
	mqPartitionKey     = "_mq_partition"

	errorHintMsg = "[bytedtrace] function '%s' not supported, please upgrade code.byted.org/bytedtrace/bytedtrace-client-go"

	DebugModeKey = "BYTEDTRACE_DEBUG_MODE"
)

var debugMode bool

func init() {
	debugModeStr := os.Getenv(DebugModeKey)
	debugMode = debugModeStr == "true"
}

// Set StatusCode to serverSpan/clientSpan.
func SetStatusCode(sp Span, value int32) {
	getSpanHandler().SetStatusCode(sp, value)
}

// Set SpanName to serverSpan/clientSpan.
func SetSpanName(sp Span, value string) {
	getSpanHandler().SetSpanName(sp, value)
}

// Set IsError to serverSpan/clientSpan.
func SetIsError(sp Span, value bool) {
	getSpanHandler().SetIsError(sp, value)
}

// Set BusinessStatusCode to serverSpan/clientSpan.
func SetBusinessStatusCode(sp Span, value int32) {
	getSpanHandler().SetBusinessStatusCode(sp, value)
}

// Set SendSize to serverSpan/clientSpan.
func SetSendSize(sp Span, value int64) {
	getSpanHandler().SetSendSize(sp, value)
}

// Set RecvSize to serverSpan/clientSpan.
func SetRecvSize(sp Span, value int64) {
	getSpanHandler().SetRecvSize(sp, value)
}

// Set ServiceType to serverSpan/clientSpan.
func SetServiceType(sp Span, value string) {
	getSpanHandler().SetServiceType(sp, value)
}

// Set Component to serverSpan/clientSpan.
func SetComponent(sp Span, value string) {
	getSpanHandler().SetComponent(sp, value)
}

// Set FromService to serverSpan.
func SetFromService(sp Span, value string) {
	getSpanHandler().SetFromService(sp, value)
}

// Set FromMethod to serverSpan.
func SetFromMethod(sp Span, value string) {
	getSpanHandler().SetFromMethod(sp, value)
}

// Set FromCluster to serverSpan.
func SetFromCluster(sp Span, value string) {
	getSpanHandler().SetFromCluster(sp, value)
}

// Set FromAddr to serverSpan.
func SetFromAddr(sp Span, value interface{}) {
	getSpanHandler().SetFromAddr(sp, value)
}

// Set FromDc to serverSpan.
func SetFromDc(sp Span, value string) {
	getSpanHandler().SetFromDc(sp, value)
}

// Set ToMethod to clientSpan.
func SetToMethod(sp Span, value string) {
	getSpanHandler().SetToMethod(sp, value)
}

// Set ToCluster to clientSpan.
func SetToCluster(sp Span, value string) {
	getSpanHandler().SetToCluster(sp, value)
}

// Set ToAddr to clientSpan.
func SetToAddr(sp Span, value interface{}) {
	getSpanHandler().SetToAddr(sp, value)
}

// Set ToDc to clientSpan.
func SetToDc(sp Span, value string) {
	getSpanHandler().SetToDc(sp, value)
}

func SetErrorMessage(sp Span, value string) {
	if sp != nil {
		sp.SetTag(errorMessageKey, value)
	}
}

// IncrExecCount should only be used with batch span who are started with option "AsBatchExecSpan"
// https://bytedance.feishu.cn/docs/doccnYOqwx9Z4l71dF1z4jKLnyd#
func IncrExecCount(sp Span, cnt int32, success bool) {
	spHandler := getSpanHandler()
	if bsh, ok := spHandler.(BatchSpanHandler); ok {
		bsh.IncrExecCount(sp, cnt, success)
	} else if debugMode {
		fmt.Printf(errorHintMsg, "IncrExecCount")
	}
}

// RecordOneExec should only be used with batch span who are started with option "AsBatchExecSpan"
// https://bytedance.feishu.cn/docs/doccnYOqwx9Z4l71dF1z4jKLnyd#
func RecordOneExec(sp Span, duration time.Duration, success bool) {
	spHandler := getSpanHandler()
	if bsh, ok := spHandler.(BatchSpanHandler); ok {
		bsh.RecordOneExec(sp, duration, success)
	} else if debugMode {
		fmt.Printf(errorHintMsg, "RecordOneExec")
	}
}

// SetConsumerGroup only work for mq consumer span which is started with option "AsMqConsumerSpan"
func SetConsumerGroup(sp Span, value string) {
	spHandler := getSpanHandler()
	if csh, ok := spHandler.(ConsumerSpanHandler); ok {
		csh.SetConsumerGroup(sp, value)
	} else if debugMode {
		fmt.Printf(errorHintMsg, "SetConsumerGroup")
	}
}

// SetProducerService only work for mq consumer span which is started with option "AsMqConsumerSpan"
func SetProducerService(sp Span, producerService string) {
	spHandler := getSpanHandler()
	if csh, ok := spHandler.(ConsumerSpanHandler); ok {
		csh.SetProducerService(sp, producerService)
	} else if debugMode {
		fmt.Printf(errorHintMsg, "SetProducerService")
	}
}

// SetProducerCluster only work for mq consumer span which is started with option "AsMqConsumerSpan"
func SetProducerCluster(sp Span, producerCluster string) {
	spHandler := getSpanHandler()
	if csh, ok := spHandler.(ConsumerSpanHandler); ok {
		csh.SetProducerCluster(sp, producerCluster)
	} else if debugMode {
		fmt.Printf(errorHintMsg, "SetProducerCluster")
	}
}

// SetProducerAddr only work for mq consumer span which is started with option "AsMqConsumerSpan"
func SetProducerAddr(sp Span, producerAddr string) {
	spHandler := getSpanHandler()
	if csh, ok := spHandler.(ConsumerSpanHandler); ok {
		csh.SetProducerAddr(sp, producerAddr)
	} else if debugMode {
		fmt.Printf(errorHintMsg, "SetProducerAddr")
	}
}

// SetProducerDc only work for mq consumer span which is started with option "AsMqConsumerSpan"
func SetProducerDc(sp Span, producerDc string) {
	spHandler := getSpanHandler()
	if csh, ok := spHandler.(ConsumerSpanHandler); ok {
		csh.SetProducerDc(sp, producerDc)
	} else if debugMode {
		fmt.Printf(errorHintMsg, "SetProducerDc")
	}
}

// SetProduceTs only work for mq consumer span which is started with option "AsMqConsumerSpan"
func SetProduceTs(sp Span, produceTs time.Time) {
	spHandler := getSpanHandler()
	if csh, ok := spHandler.(ConsumerSpanHandler); ok {
		csh.SetProduceTs(sp, produceTs)
	} else if debugMode {
		fmt.Printf(errorHintMsg, "SetProduceTs")
	}
}

// SetMqPartition only work for mq consumer span which is started with option "AsMqConsumerSpan"
func SetMqPartition(sp Span, mqPartition string) {
	spHandler := getSpanHandler()
	if csh, ok := spHandler.(ConsumerSpanHandler); ok {
		csh.SetMqPartition(sp, mqPartition)
	} else if debugMode {
		fmt.Printf(errorHintMsg, "SetMqPartition")
	}
}

// SetMqLag only work for mq consumer span which is started with option "AsMqConsumerSpan"
// If MqLag is not set, it's computed automatically
// by (span.StartTime - ProduceTs) if ProduceTs is provided
func SetMqLag(sp Span, mqLag time.Duration) {
	spHandler := getSpanHandler()
	if csh, ok := spHandler.(ConsumerSpanHandler); ok {
		csh.SetMqLag(sp, mqLag)
	} else if debugMode {
		fmt.Printf(errorHintMsg, "SetMqLag")
	}
}

// Sampling trace.
// For example:
// 1. The local span will be report but not transfer to backward and forward.
//		 SamplingTrace(span, LocalTrace)
// 2. The local span will be report and transfer to backward, this is the same as span.SetPostTrace(spanCtx).
//		 SamplingTrace(span, PostTrace, SetSamplingTraceRootSpanContext(spanCtx))
// 3. The local span will be report and transfer to forward.
//		 SamplingTrace(span, DebugTrace)
// 4. The local span will be report and transfer to backward and forward.
//       SamplingTrace(span, BothSideTrace,SetSamplingTraceRootSpanContext(spanCtx))
// 5. If no SamplingTraceOption is provided, like SamplingTrace(span), it is the same as span.SetPostTrace(nil).
func SamplingTrace(sp Span, opt ...SamplingTraceOption) {
	spHandler := getSpanHandler()
	if ph, ok := spHandler.(SamplingTraceHandler); ok {
		ph.SamplingTrace(sp, opt...)
	} else if debugMode {
		fmt.Printf(errorHintMsg, "SamplingTrace")
	}
}

// Whether sampling transfer to backward.
func IsBackwardPropagate(sp Span) bool {
	spHandler := getSpanHandler()
	if ph, ok := spHandler.(SamplingTraceHandler); ok {
		return ph.IsBackwardPropagate(sp)
	} else if debugMode {
		fmt.Printf(errorHintMsg, "IsBackwardPropagate")
	}
	return false
}

// Whether sampling transfer to forward.
func IsForwardPropagate(sp Span) bool {
	spHandler := getSpanHandler()
	if ph, ok := spHandler.(SamplingTraceHandler); ok {
		return ph.IsForwardPropagate(sp)
	} else if debugMode {
		fmt.Printf(errorHintMsg, "IsForwardPropagate")
	}
	return false
}

// Whether the span will be reported.
func IsReported(sp Span) bool {
	spHandler := getSpanHandler()
	if ph, ok := spHandler.(SamplingTraceHandler); ok {
		return ph.IsReported(sp)
	} else if debugMode {
		fmt.Printf(errorHintMsg, "IsReported")
	}
	return false
}

// Return whether the span is noop.
func IsNoopSpan(sp Span) bool {
	if defaultNoopSpanHandler.IsNoopSpan(sp) {
		return true
	}
	spHandler := getSpanHandler()
	if ph, ok := spHandler.(SpanTypeCheckHandler); ok {
		return ph.IsNoopSpan(sp)
	} else if debugMode {
		fmt.Printf(errorHintMsg, "IsNoopSpan")
	}
	return false
}
