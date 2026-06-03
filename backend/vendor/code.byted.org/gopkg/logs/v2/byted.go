package logs

import (
	"context"
	"unsafe"

	"code.byted.org/gopkg/env"
)

// ByteDLogger is a bytedance logging handler.
// It is also the default logger in Log2.0 SDK
type ByteDLogger struct {
	logger
	psm     string
	options []Option
}

// NewByteDLogger creates a new BytedLogger with options.
func NewByteDLogger(options ...Option) *ByteDLogger {
	options = append(options, SetMiddleware(metricsMiddleware))
	logger := &ByteDLogger{*NewLogger(), env.PSM(), options}
	logger.padding = []byte(" ")
	for _, op := range options {
		op(logger)
	}
	return logger
}

func (l *ByteDLogger) GetOptions() []Option {
	return l.options
}

// Trace starts a trace level log printing.
func (l *ByteDLogger) Trace() *Log {
	return l.prefix(l.logger.Trace())
}

// Debug starts a debug level log printing.
func (l *ByteDLogger) Debug() *Log {
	return l.prefix(l.logger.Debug())
}

// Info starts a info level log printing.
func (l *ByteDLogger) Info() *Log {
	return l.prefix(l.logger.Info())
}

// Warn starts a warning level log printing.
func (l *ByteDLogger) Warn() *Log {
	return l.prefix(l.logger.Warn())
}

// Error starts a error level log printing.
func (l *ByteDLogger) Error() *Log {
	return l.prefix(l.logger.Error())
}

// Fatal starts a fatal level log printing.
func (l *ByteDLogger) Fatal() *Log {
	return l.prefix(l.logger.Fatal())
}

// Notice starts a notice level log printing.
func (l *ByteDLogger) Notice() *Log {
	return l.prefix(l.logger.Notice())
}

func (l *ByteDLogger) CtxFlushNotice(ctx context.Context) {
	ntc := GetNotice(ctx)
	if ntc == nil {
		return
	}
	kvs := ntc.KVs()
	if len(kvs) == 0 {
		return
	}
	l.Notice().CallDepth(1).KVs(kvs...).Emit()
}

// This private function will set most common fields for the log instance
func (l *ByteDLogger) prefix(log *Log) *Log {
	return (*prefixedLog)(unsafe.Pointer(log)).Level().Time(l.includeZoneInfo).Version().Location().Host().PSM(l.psm).LogID().Cluster().Stage().SpanID().End()
}

// TODO: combine getting context between v2 and v2/writer
func logIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return "-"
	}
	// Log ID is defined in https://code.byted.org/kite/kitutil. avoid dependency directly.
	val := ctx.Value("K_LOGID")
	if val != nil {
		logID := val.(string)
		return logID
	}
	return "-"
}

func spanIDFromContext(ctx context.Context) uint64 {
	if ctx == nil {
		return 0
	}
	// Span ID is injected by byted trace (https://code.byted.org/bytedtrace/bytedtrace-client-go)
	val := ctx.Value("K_SPANID")
	if val != nil {
		spanID, valid := val.(uint64)
		if valid {
			return spanID
		}
	}
	return 0
}
