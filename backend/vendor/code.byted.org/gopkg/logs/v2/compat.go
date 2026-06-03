package logs

import (
	"context"
	"fmt"
	"os"
	"sync"
)

const (
	compatCallDepthOffset = 1
)

var (
	compatPadding = []byte("")
)

func (l *Log) fastSPrintf(format string, args ...interface{}) *Log {
	if l == nil {
		return nil
	}
	inVerb := false
	lastWroteAt := 0
	verbStartAt := 0
	verbCount := 0
	for i := 0; i < len(format); i++ {
		char := format[i]
		if char != '%' {
			if !inVerb {
				continue
			}
			// fmt verbs have to end with a single alphabet
			if !('a' <= char && char <= 'z') && !('A' <= char && char <= 'Z') {
				continue
			}
			inVerb = false
			l.Str(format[lastWroteAt:verbStartAt])
			var arg interface{}
			if verbCount > len(args)-1 {
				arg = "empty?"
			} else {
				arg = args[verbCount]
			}
			switch a := arg.(type) {
			case Lazier:
				arg = a()
			default:
			}
			switch char {
			case 'd':
				switch v := arg.(type) {
				case int64:
					l.Int(int(v))
				case int32:
					l.Int(int(v))
				case int:
					l.Int(v)
				default:
					l.Str(fmt.Sprintf("%d", v))
				}
			case 'f':
				switch v := arg.(type) {
				case float64:
					l.Float(v)
				case float32:
					l.Float(float64(v))
				default:
					l.Str(fmt.Sprintf("%f", v))
				}
			case 't':
				switch v := arg.(type) {
				case bool:
					l.Bool(v)
				default:
					l.Str(fmt.Sprintf("%t", v))
				}
			case 's':
				switch v := arg.(type) {
				case string:
					l.Str(v)
				case fmt.Stringer:
					l.Str(v.String())
				case error:
					l.Str(v.Error())
				default:
					l.Str(fmt.Sprintf("%s", v))
				}
			case 'x':
				l.Str(fmt.Sprintf("%x", arg))
			case 'v':
				if i >= 1 && format[i-1] == '+' {
					l.Str(fmt.Sprintf("%+v", arg))
				} else {
					l.Obj(arg)
				}
			default:
				l.Obj(arg)
			}
			verbCount++
			lastWroteAt = i + 1
			continue
		}
		// handle '%%'
		if i < len(format)-1 && format[i+1] == '%' {
			l.Str(format[lastWroteAt:i])
			l.Str("%")
			lastWroteAt = i + 2
			i++
			inVerb = false
			continue
		}

		inVerb = true
		verbStartAt = i
	}
	if lastWroteAt < len(format) {
		l.Str(format[lastWroteAt:])
	}
	return l
}

// CompatLogger is a v1 compatible ByteDLogger, you can use most of the methods of v1 on it.
// However, v2 does not support std string format originally,
// CompatLogger creates a simple fast approach fmt.Sprintf,
// map %s to Log.Str, %f to Log.Float, %v to json.Marshal and ignore any prefix on verbs.
// it is not only able to boost the format performance but also keep the compatibility.
type CompatLogger struct {
	v2 *ByteDLogger
}

// NewCompatLogger creates a v1 compatible logger CompatLogger,
// it supports Info, CtxInfo, CtxInfosf, CtxInfoKVs and other level methods.
func NewCompatLogger(ops ...Option) *CompatLogger {
	return &CompatLogger{
		NewByteDLogger(ops...),
	}
}

// NewCompatLoggerFrom creates a v1 compatible logger CompatLogger from a ByteDLogger.
func NewCompatLoggerFrom(logger *ByteDLogger) *CompatLogger {
	return &CompatLogger{
		logger,
	}
}

// Fatal prints a log similar as code.byted.org/gopkg/logs.Fatal.
func (l *CompatLogger) Fatal(format string, v ...interface{}) {
	l.v2.Fatal().v1Compat().fastSPrintf(format, v...).Emit()
}

// CtxFatal prints a log similar as code.byted.org/gopkg/logs.CtxFatal.
func (l *CompatLogger) CtxFatal(ctx context.Context, format string, v ...interface{}) {
	l.v2.Fatal().With(ctx).v1Compat().fastSPrintf(format, v...).Emit()
}

// Error prints a log similar as code.byted.org/gopkg/logs.Error.
func (l *CompatLogger) Error(format string, v ...interface{}) {
	l.v2.Error().v1Compat().fastSPrintf(format, v...).Emit()
}

// CtxError prints a log similar as code.byted.org/gopkg/logs.CtxError.
func (l *CompatLogger) CtxError(ctx context.Context, format string, v ...interface{}) {
	l.v2.Error().With(ctx).v1Compat().fastSPrintf(format, v...).Emit()
}

// Warn prints a log similar as code.byted.org/gopkg/logs.Warn.
func (l *CompatLogger) Warn(format string, v ...interface{}) {
	l.v2.Warn().v1Compat().fastSPrintf(format, v...).Emit()
}

// CtxWarn prints a log similar as code.byted.org/gopkg/logs.CtxWarn.
func (l *CompatLogger) CtxWarn(ctx context.Context, format string, v ...interface{}) {
	l.v2.Warn().With(ctx).v1Compat().fastSPrintf(format, v...).Emit()
}

// Notice prints a log similar as code.byted.org/gopkg/logs.Notice.
func (l *CompatLogger) Notice(format string, v ...interface{}) {
	l.v2.Notice().v1Compat().fastSPrintf(format, v...).Emit()
}

// CtxNotice prints a log similar as code.byted.org/gopkg/logs.CtxNotice.
func (l *CompatLogger) CtxNotice(ctx context.Context, format string, v ...interface{}) {
	l.v2.Notice().With(ctx).v1Compat().fastSPrintf(format, v...).Emit()
}

// Info prints a log similar as code.byted.org/gopkg/logs.Info.
func (l *CompatLogger) Info(format string, v ...interface{}) {
	l.v2.Info().v1Compat().fastSPrintf(format, v...).Emit()
}

// CtxInfo prints a log similar as code.byted.org/gopkg/logs.CtxInfo.
func (l *CompatLogger) CtxInfo(ctx context.Context, format string, v ...interface{}) {
	l.v2.Info().With(ctx).v1Compat().fastSPrintf(format, v...).Emit()
}

// Trace prints a log similar as code.byted.org/gopkg/logs.Trace.
func (l *CompatLogger) Trace(format string, v ...interface{}) {
	l.v2.Trace().v1Compat().fastSPrintf(format, v...).Emit()
}

// CtxTrace prints a log similar as code.byted.org/gopkg/logs.CtxTrace.
func (l *CompatLogger) CtxTrace(ctx context.Context, format string, v ...interface{}) {
	l.v2.Trace().With(ctx).v1Compat().fastSPrintf(format, v...).Emit()
}

// Debug prints a log similar as code.byted.org/gopkg/logs.Debug.
func (l *CompatLogger) Debug(format string, v ...interface{}) {
	l.v2.Debug().v1Compat().fastSPrintf(format, v...).Emit()
}

// CtxDebug prints a log similar as code.byted.org/gopkg/logs.CtxDebug.
func (l *CompatLogger) CtxDebug(ctx context.Context, format string, v ...interface{}) {
	l.v2.Debug().With(ctx).v1Compat().fastSPrintf(format, v...).Emit()
}

// CtxFatalsf works like logs.CtxFatalsf.
func (l *CompatLogger) CtxFatalsf(ctx context.Context, format string, v ...string) {
	strSlice := make([]interface{}, 0, len(v))
	for _, s := range v {
		strSlice = append(strSlice, s)
	}
	l.v2.Fatal().With(ctx).v1Compat().fastSPrintf(format, strSlice...).Emit()
}

// CtxErrorsf works like logs.CtxErrorsf.
func (l *CompatLogger) CtxErrorsf(ctx context.Context, format string, v ...string) {
	strSlice := make([]interface{}, 0, len(v))
	for _, s := range v {
		strSlice = append(strSlice, s)
	}
	l.v2.Error().With(ctx).v1Compat().fastSPrintf(format, strSlice...).Emit()
}

// CtxWarnsf works like logs.CtxWarnsf.
func (l *CompatLogger) CtxWarnsf(ctx context.Context, format string, v ...string) {
	strSlice := make([]interface{}, 0, len(v))
	for _, s := range v {
		strSlice = append(strSlice, s)
	}
	l.v2.Warn().With(ctx).v1Compat().fastSPrintf(format, strSlice...).Emit()
}

// CtxNoticesf works like logs.CtxNoticesf.
func (l *CompatLogger) CtxNoticesf(ctx context.Context, format string, v ...string) {
	strSlice := make([]interface{}, 0, len(v))
	for _, s := range v {
		strSlice = append(strSlice, s)
	}
	l.v2.Notice().With(ctx).v1Compat().fastSPrintf(format, strSlice...).Emit()
}

// CtxInfosf works like logs.CtxInfosf.
func (l *CompatLogger) CtxInfosf(ctx context.Context, format string, v ...string) {
	strSlice := make([]interface{}, 0, len(v))
	for _, s := range v {
		strSlice = append(strSlice, s)
	}
	l.v2.Info().With(ctx).v1Compat().fastSPrintf(format, strSlice...).Emit()
}

// CtxDebugsf works like logs.CtxDebugsf.
func (l *CompatLogger) CtxDebugsf(ctx context.Context, format string, v ...string) {
	strSlice := make([]interface{}, 0, len(v))
	for _, s := range v {
		strSlice = append(strSlice, s)
	}
	l.v2.Debug().With(ctx).v1Compat().fastSPrintf(format, strSlice...).Emit()
}

// CtxFatalKVs provides function like logs.CtxFatalKVs.
func (l *CompatLogger) CtxFatalKVs(ctx context.Context, kvs ...interface{}) {
	l.v2.Fatal().With(ctx).v1Compat().KVs(kvs...).Emit()
}

// CtxErrorKVs provides function like logs.CtxErrorKVs.
func (l *CompatLogger) CtxErrorKVs(ctx context.Context, kvs ...interface{}) {
	l.v2.Error().With(ctx).v1Compat().KVs(kvs...).Emit()
}

// CtxWarnKVs provides function like logs.CtxWarnKVs.
func (l *CompatLogger) CtxWarnKVs(ctx context.Context, kvs ...interface{}) {
	l.v2.Warn().With(ctx).v1Compat().KVs(kvs...).Emit()
}

// CtxNoticeKVs provides function like logs.CtxNoticeKVs.
func (l *CompatLogger) CtxNoticeKVs(ctx context.Context, kvs ...interface{}) {
	l.v2.Notice().With(ctx).v1Compat().KVs(kvs...).Emit()
}

// CtxInfoKVs provides function like logs.CtxInfoKVs.
func (l *CompatLogger) CtxInfoKVs(ctx context.Context, kvs ...interface{}) {
	l.v2.Info().With(ctx).v1Compat().KVs(kvs...).Emit()
}

// CtxDebugKVs provides function like logs.CtxDebugKVs.
func (l *CompatLogger) CtxDebugKVs(ctx context.Context, kvs ...interface{}) {
	l.v2.Debug().With(ctx).v1Compat().KVs(kvs...).Emit()
}

// CtxTraceKVs provides function like logs.CtxTraceKVs.
func (l *CompatLogger) CtxTraceKVs(ctx context.Context, kvs ...interface{}) {
	l.v2.Trace().With(ctx).v1Compat().KVs(kvs...).Emit()
}

func (l *CompatLogger) CtxFlushNotice(ctx context.Context) {
	var kvs []interface{}
	ntc := GetNotice(ctx)
	if ntc != nil {
		kvs = ntc.KVs()
	}

	l.v2.Notice().CallDepth(1).KVs(kvs...).Emit()
}

// PrintStack prints the stacks in Info level.
// If printAllGoroutines is true, it prints the stacks of all goroutines.
// Otherwise, it just prints the current goroutine's stack.
func (l *CompatLogger) PrintStack(printAllGoroutines bool) {
	ctx := context.Background()
	if printAllGoroutines {
		ctx = CtxStackInfo(ctx, AllGoroutines)
	} else {
		ctx = CtxStackInfo(ctx, CurrGoroutine)
	}
	l.CtxInfo(ctx, "")
}

// Flush blocks logger and flush all buffered logs.
func (l *CompatLogger) Flush() {
	err := l.v2.Flush()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "logs v2 client flush error: %s\n", err)
	}
}

// Close flushes logger and graceful exit.
func (l *CompatLogger) Close() error {
	return l.v2.Close()
}

// Stop graceful exit with no error returned.
func (l *CompatLogger) Stop() {
	err := l.v2.Close()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "logs v2 client stop error: %s\n", err)
	}
}

type LocationCtxKey struct{}

func (l *Log) v1CtxLocation() *Log {
	if l == nil {
		return nil
	}
	if l.ctx != nil {
		if value := l.ctx.Value(LocationCtxKey{}); value != nil {
			return l.Line(&Line{literal: []byte(value.(string))})
		}
	}
	return l
}

func (l *Log) v1Compat() *Log {
	if l == nil {
		return nil
	}
	return l.CallDepth(compatCallDepthOffset).setPadding(compatPadding).v1CtxLocation()
}

func (l *Log) setKVToMap(m interface{}, kvs ...interface{}) *Log {
	if l == nil {
		return nil
	}
	if m != nil {
		m.(*sync.Map).Range(func(key, value interface{}) bool {
			l = l.KV(key, value)
			return true
		})
	}
	for i := 0; i+1 < len(kvs); i += 2 {
		k := kvs[i]
		v := kvs[i+1]
		l = l.KV(k, v)
	}
	return l
}
