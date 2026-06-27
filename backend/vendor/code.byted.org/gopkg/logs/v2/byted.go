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
	psm                          string
	disableBytedMetricMiddleware bool
	options                      []Option
}

// NewByteDLogger creates a new BytedLogger with options.
func NewByteDLogger(options ...Option) *ByteDLogger {
	logger := &ByteDLogger{logger: *NewLogger(), psm: env.PSM(), options: options}
	logger.padding = []byte(" ")
	for _, op := range options {
		op(logger)
	}

	if !logger.disableBytedMetricMiddleware {
		// Append the errorlog middleware.
		SetMiddleware(getErrorLogMiddleware(logger.psm))(logger)
	}

	return logger
}

func (l *ByteDLogger) GetOptions() []Option {
	return l.options
}

// Trace starts a trace level log printing.
func (l *ByteDLogger) Trace(ops ...loggerOption) *Log {
	return l.prefix(l.logger.Trace(ops...))
}

// Debug starts a debug level log printing.
func (l *ByteDLogger) Debug(ops ...loggerOption) *Log {
	return l.prefix(l.logger.Debug(ops...))
}

// Info starts a info level log printing.
func (l *ByteDLogger) Info(ops ...loggerOption) *Log {
	return l.prefix(l.logger.Info(ops...))
}

// Warn starts a warning level log printing.
func (l *ByteDLogger) Warn(ops ...loggerOption) *Log {
	return l.prefix(l.logger.Warn(ops...))
}

// Error starts a error level log printing.
func (l *ByteDLogger) Error(ops ...loggerOption) *Log {
	return l.prefix(l.logger.Error(ops...))
}

// Fatal starts a fatal level log printing.
func (l *ByteDLogger) Fatal(ops ...loggerOption) *Log {
	return l.prefix(l.logger.Fatal(ops...))
}

// Notice starts a notice level log printing.
func (l *ByteDLogger) Notice(ops ...loggerOption) *Log {
	return l.prefix(l.logger.Notice(ops...))
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
	l.Notice().With(ctx).CallDepth(1).KVs(kvs...).Emit()
}

// This private function will set most common fields for the log instance
func (l *ByteDLogger) prefix(log *Log) *Log {
	return (*prefixedLog)(unsafe.Pointer(log)).Level().Time().Version().Location().Host().PSM(l.psm).LogID().Cluster().Stage().SpanID().End()
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
