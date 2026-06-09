package logs

import (
	"context"
	"fmt"
	"os"

	"code.byted.org/gopkg/env"
	"code.byted.org/gopkg/logs/v2/writer"
)

var (
	V2                      *ByteDLogger
	V1                      *CompatLogger
	defaultCompatibleLogger *CompatLogger
)

func init() {
	// TODO: NewAgentWriter relies on init function, refactor agent SDK and supports be defined as variable
	disable := os.Getenv("_BYTED_DISABLE_LOG_AUTO_INIT")

	if disable == "True" {
		return
	}

	Init()
}

func Init() {
	// TODO: NewAgentWriter relies on init function, refactor agent SDK and supports be defined as variable
	writers := make([]writer.LogWriter, 0)
	level := DebugLevel
	ops := make([]Option, 0)
	if env.InTCE() {
		level = InfoLevel
		fileName := fmt.Sprintf("/opt/tiger/toutiao/log/app/%s.log", env.PSM())
		writers = append(writers, writer.NewAsyncWriter(writer.NewFileWriter(fileName, writer.Hourly), true))
		writers = append(writers, writer.NewAgentWriter())
	} else {
		writers = append(writers, writer.NewConsoleWriter(writer.SetColorful(true)))
	}
	ops = append(ops, SetWriter(level, writers...))
	V2 = NewByteDLogger(ops...)
	V1 = NewCompatLoggerFrom(V2)
	defaultCompatibleLogger = NewCompatLoggerFrom(V2, WithCallDepthOffset(2))
}

// SetDefaultLogger resets the default logger with specified options,
// it is not thread-safe and please only call it in program initialization.
// Libraries should not config the default logger and leaves it to the application user,
// otherwise loggers would cover each other.
func SetDefaultLogger(ops ...Option) {
	curOps := V2.GetOptions()
	curOps = append(curOps, ops...)
	*V2 = *NewByteDLogger(curOps...)
	*V1 = *NewCompatLoggerFrom(V2)
	*defaultCompatibleLogger = *NewCompatLoggerFrom(V2, WithCallDepthOffset(2))
}

// SetLevel sets the minimal level for defaultCompatibleLogger. It is safe to increase the level.
// Please not decrease the level directly. Use SetLevelForWriters instead.
func SetLevel(newLevel Level) {
	if defaultCompatibleLogger == nil {
		return
	}
	defaultCompatibleLogger.v2.SetLevel(newLevel)
}

// SetLevelForWriters updates the minimal level for the writer of the defaultCompatibleLogger.
// It may also update the loggers' level.
func SetLevelForWriters(newLevel Level, logWriters ...writer.LogWriter) {
	if defaultCompatibleLogger == nil {
		return
	}
	defaultCompatibleLogger.v2.SetLevelForWriters(newLevel, logWriters...)
}

// GetWriters returns the level writers
func GetWriters() []leveledWriter {
	if defaultCompatibleLogger == nil {
		return nil
	}
	return defaultCompatibleLogger.v2.GetWriter()
}

// EnableDynamicLogLevel enableds dynamic context log level for compatibleLogger.
func EnableDynamicLogLevel() {
	WithDynamicLevel(true)(defaultCompatibleLogger)
}

// Fatal works like logs.Fatal.
func Fatal(format string, v ...interface{}) {
	defaultCompatibleLogger.Fatal(format, v...)
}

// Error works like logs.Erorr.
func Error(format string, v ...interface{}) {
	defaultCompatibleLogger.Error(format, v...)
}

// Warn works like logs.Warn.
func Warn(format string, v ...interface{}) {
	defaultCompatibleLogger.Warn(format, v...)
}

// Notice works like logs.Notice.
func Notice(format string, v ...interface{}) {
	defaultCompatibleLogger.Notice(format, v...)
}

// Info works like logs.Info.
func Info(format string, v ...interface{}) {
	defaultCompatibleLogger.Info(format, v...)
}

// Debug works like logs.Debug.
func Debug(format string, v ...interface{}) {
	defaultCompatibleLogger.Debug(format, v...)
}

// Trace works like logs.Trace.
func Trace(format string, v ...interface{}) {
	defaultCompatibleLogger.Trace(format, v...)
}

// Fatalf works like logs.Fatalf.
func Fatalf(format string, v ...interface{}) {
	defaultCompatibleLogger.Fatal(format, v...)
}

// Errorf works like logs.Errorf.
func Errorf(format string, v ...interface{}) {
	defaultCompatibleLogger.Error(format, v...)
}

// Warnf works like logs.Warnf.
func Warnf(format string, v ...interface{}) {
	defaultCompatibleLogger.Warn(format, v...)
}

// Noticef works like logs.Noticef.
func Noticef(format string, v ...interface{}) {
	defaultCompatibleLogger.Notice(format, v...)
}

// Infof works like logs.Infof.
func Infof(format string, v ...interface{}) {
	defaultCompatibleLogger.Info(format, v...)
}

// Debugf works like logs.Debugf.
func Debugf(format string, v ...interface{}) {
	defaultCompatibleLogger.Debug(format, v...)
}

// Tracef works like logs.Tracef.
func Tracef(format string, v ...interface{}) {
	defaultCompatibleLogger.Trace(format, v...)
}

// CtxFatal works like logs.CtxFatal.
func CtxFatal(ctx context.Context, format string, v ...interface{}) {
	defaultCompatibleLogger.CtxFatal(ctx, format, v...)
}

// CtxError works like logs.CtxError.
func CtxError(ctx context.Context, format string, v ...interface{}) {
	defaultCompatibleLogger.CtxError(ctx, format, v...)
}

// CtxWarn works like logs.CtxWarn.
func CtxWarn(ctx context.Context, format string, v ...interface{}) {
	defaultCompatibleLogger.CtxWarn(ctx, format, v...)
}

// CtxNotice works like logs.CtxNotice.
func CtxNotice(ctx context.Context, format string, v ...interface{}) {
	defaultCompatibleLogger.CtxNotice(ctx, format, v...)
}

// CtxInfo works like logs.CtxInfo.
func CtxInfo(ctx context.Context, format string, v ...interface{}) {
	defaultCompatibleLogger.CtxInfo(ctx, format, v...)
}

// CtxDebug works like logs.CtxDebug.
func CtxDebug(ctx context.Context, format string, v ...interface{}) {
	defaultCompatibleLogger.CtxDebug(ctx, format, v...)
}

// CtxTrace works like logs.CtxTrace.
func CtxTrace(ctx context.Context, format string, v ...interface{}) {
	defaultCompatibleLogger.CtxTrace(ctx, format, v...)
}

// CtxFatalsf works like logs.CtxFatalsf.
func CtxFatalsf(ctx context.Context, format string, v ...string) {
	defaultCompatibleLogger.CtxFatalsf(ctx, format, v...)
}

// CtxErrorsf works like logs.CtxErrorsf.
func CtxErrorsf(ctx context.Context, format string, v ...string) {
	defaultCompatibleLogger.CtxErrorsf(ctx, format, v...)
}

// CtxErrorsf works like logs.CtxErrorsf.
func CtxWarnsf(ctx context.Context, format string, v ...string) {
	defaultCompatibleLogger.CtxWarnsf(ctx, format, v...)
}

// CtxNoticesf works like logs.CtxNoticesf.
func CtxNoticesf(ctx context.Context, format string, v ...string) {
	defaultCompatibleLogger.CtxNoticesf(ctx, format, v...)
}

// CtxErrorsf works like logs.CtxErrorsf.
func CtxInfosf(ctx context.Context, format string, v ...string) {
	defaultCompatibleLogger.CtxInfosf(ctx, format, v...)
}

// CtxDebugsf works like logs.CtxDebugsf.
func CtxDebugsf(ctx context.Context, format string, v ...string) {
	defaultCompatibleLogger.CtxDebugsf(ctx, format, v...)
}

// CtxTracesf works like logs.CtxDebugsf.
func CtxTracesf(ctx context.Context, format string, v ...string) {
	defaultCompatibleLogger.CtxTracesf(ctx, format, v...)
}

// CtxFatalKVs provides function like logs.CtxFatalKVs.
func CtxFatalKVs(ctx context.Context, kvs ...interface{}) {
	defaultCompatibleLogger.CtxFatalKVs(ctx, kvs...)
}

// CtxErrorKVs provides function like logs.CtxErrorKVs.
func CtxErrorKVs(ctx context.Context, kvs ...interface{}) {
	defaultCompatibleLogger.CtxErrorKVs(ctx, kvs...)
}

// CtxWarnKVs provides function like logs.CtxWarnKVs.
func CtxWarnKVs(ctx context.Context, kvs ...interface{}) {
	defaultCompatibleLogger.CtxWarnKVs(ctx, kvs...)
}

// CtxNoticeKVs provides function like logs.CtxNoticeKVs.
func CtxNoticeKVs(ctx context.Context, kvs ...interface{}) {
	defaultCompatibleLogger.CtxNoticeKVs(ctx, kvs...)
}

// CtxInfoKVs provides function like logs.CtxInfoKVs.
func CtxInfoKVs(ctx context.Context, kvs ...interface{}) {
	defaultCompatibleLogger.CtxInfoKVs(ctx, kvs...)
}

// CtxDebugKVs provides function like logs.CtxDebugKVs.
func CtxDebugKVs(ctx context.Context, kvs ...interface{}) {
	defaultCompatibleLogger.CtxDebugKVs(ctx, kvs...)
}

// CtxTraceKVs provides function like logs.CtxTraceKVs.
func CtxTraceKVs(ctx context.Context, kvs ...interface{}) {
	defaultCompatibleLogger.CtxTraceKVs(ctx, kvs...)
}

// CtxFlushNotice provides function like logs.CtxFlushNotice
func CtxFlushNotice(ctx context.Context) {
	ntc := GetNotice(ctx)
	if ntc == nil {
		return
	}
	kvs := ntc.KVs()
	if len(kvs) == 0 {
		return
	}
	defaultCompatibleLogger.CtxNoticeKVs(ctx, kvs...)
}

// PrintStack prints the stacks in Info level.
// If printAllGoroutines is true, it prints the stacks of all goroutines.
// Otherwise, it just prints the current goroutine's stack.
func PrintStack(printAllGoroutines bool) {
	defaultCompatibleLogger.PrintStack(printAllGoroutines)
}

func Flush() {
	defaultCompatibleLogger.Flush()
}

func Stop() {
	defaultCompatibleLogger.Close()
}
