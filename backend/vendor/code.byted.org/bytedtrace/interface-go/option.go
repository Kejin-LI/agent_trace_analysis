package bytedtracer

import (
	"time"

	"github.com/opentracing/opentracing-go"
)

var (
	trueValue  = true
	falseValue = false
)

const (
	// Span reference type
	ChildOfRef     = 0
	FollowsFromRef = 1
)

type ExtSpanModel byte

const (
	BaseSpanModel ExtSpanModel = iota
	BatchSpanModel
	ConsumerSpanModel
)

const (
	// SamplingTrace only happen in local txn
	LocalTraceType int32 = 1
	// SamplingTrace happen in local txn and transfer to backward
	PostTraceType int32 = 2
	// SamplingTrace happen in local txn and transfer to forward
	DebugTraceType int32 = 3
	// SamplingTrace happen in local txn and transfer to backward and forward
	BothSideTraceType int32 = 4
)

type StartSpanOption func(ops *StartSpanOptions)

func (opt StartSpanOption) Apply(o *opentracing.StartSpanOptions) {
}

type StartSpanOptions struct {
	PostTraceCtxFromChild SpanContext
	Reference             []SpanReference
	StartTime             time.Time
	PredefineTagNum       int
	// Deprecated: Only for opentracing
	Tags []TagKV

	EmitMetrics *bool
	EmitSpanLog *bool

	// whether emit remote addr in metrics, default false
	EmitRemoteAddrInMetrics *bool

	// extended span model such as batchSpan、consumerSpan
	ExtSpanModel ExtSpanModel

	// batch span option
	DisableBatchSpanTotalMetrics bool

	// check whether use light mode
	LightModeCheckComponent string

	// set span trace logger
	SpanTraceLoggerName string
}

type SpanReference struct {
	ReferenceType     int32
	ReferencedContext SpanContext
}

// Control span emit metrics when finish.
func EnableEmitSpanMetrics(ops *StartSpanOptions) {
	ops.EmitMetrics = &trueValue
}

// Disable span emit metrics when finish.
func DisableEmitSpanMetrics(ops *StartSpanOptions) {
	ops.EmitMetrics = &falseValue
}

// Control span emit as a trace log when finish.
func EnableEmitSpanLog(ops *StartSpanOptions) {
	ops.EmitSpanLog = &trueValue
}

// Disable span emit as a trace log when finish.
func DisableEmitSpanLog(ops *StartSpanOptions) {
	ops.EmitSpanLog = &falseValue
}

// The new span will be the child of the SpanContext.
// Usually don't use this, because bytedtrace will use the span in context as the new span's parent.
func ChildOf(ctx opentracing.SpanContext) StartSpanOption {
	return func(ops *StartSpanOptions) {
		switch c := ctx.(type) {
		case SpanContext:
			if c == nil {
				return
			}
			ops.Reference = append(ops.Reference, SpanReference{
				ReferenceType:     ChildOfRef,
				ReferencedContext: c,
			})
		}
	}
}

// The new span will follow from the SpanContext, usually use in asynchronous call.
func FollowsFrom(ctx opentracing.SpanContext) StartSpanOption {
	return func(ops *StartSpanOptions) {
		switch c := ctx.(type) {
		case SpanContext:
			if c == nil {
				return
			}
			ops.Reference = append(ops.Reference, SpanReference{
				ReferenceType:     FollowsFromRef,
				ReferencedContext: c,
			})
		}
	}
}

// Set the startTime of span.
func SetStartTime(t time.Time) StartSpanOption {
	return func(ops *StartSpanOptions) {
		ops.StartTime = t
	}
}

func PredefineTagNum(n int) StartSpanOption {
	return func(ops *StartSpanOptions) {
		ops.PredefineTagNum = n
	}
}

func EnableEmitRemoteAddr(opts *StartSpanOptions) {
	opts.EmitRemoteAddrInMetrics = &trueValue
}

func AsBatchExecSpan(ops *StartSpanOptions) {
	ops.ExtSpanModel = BatchSpanModel
}

func AsMqConsumerSpan(ops *StartSpanOptions) {
	ops.ExtSpanModel = ConsumerSpanModel
}

func DisableBatchSpanTotalMetrics(ops *StartSpanOptions) {
	ops.DisableBatchSpanTotalMetrics = true
}

// Using component for checking whether span should use light mode.
func LightModeCheckComponent(component string) StartSpanOption {
	return func(ops *StartSpanOptions) {
		ops.LightModeCheckComponent = component
	}
}

// Choose span trace logger for this span log.
func SetSpanTraceLogger(loggerName string) StartSpanOption {
	return func(ops *StartSpanOptions) {
		ops.SpanTraceLoggerName = loggerName
	}
}

// 兼容opentracing的traceId规范.
type TraceId struct {
	High, Low uint64
}

func (t TraceId) IsValid() bool {
	return t.High != 0 || t.Low != 0
}

type SamplingTraceOptions struct {
	// Decide sampling trace transfer direction
	SamplingTraceType int32
	// If the trace transfer to upwards, you may need to set rootSpanContext
	RootSpanContext SpanContext
}

type SamplingTraceOption func(ops *SamplingTraceOptions)

func LocalTrace(ops *SamplingTraceOptions) {
	ops.SamplingTraceType = LocalTraceType
}

func PostTrace(ops *SamplingTraceOptions) {
	ops.SamplingTraceType = PostTraceType
}

func DebugTrace(ops *SamplingTraceOptions) {
	ops.SamplingTraceType = DebugTraceType
}

func BothSideTrace(ops *SamplingTraceOptions) {
	ops.SamplingTraceType = BothSideTraceType
}

func SetSamplingTraceRootSpanContext(ctx SpanContext) SamplingTraceOption {
	return func(ops *SamplingTraceOptions) {
		ops.RootSpanContext = ctx
	}
}
