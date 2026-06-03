package logs

import (
	"context"

	"code.byted.org/gopkg/env"
	"code.byted.org/gopkg/logs/utils"
)

var (
	defaultLogger      *Logger
	cluster            = env.Cluster()
	localIP            = env.HostIP()
	DynamicLogLevelKey = "K_DYNAMIC_LOG_LEVEL"
	enableSecMark      bool
	currVersion        string
)

func init() {
	defaultLogger = NewConsoleLogger()
	defaultLogger.StartLogger()
	defaultLogger.SetCallDepth(3)

	enableSecMark = utils.DetermineRegion() == utils.REGION_TTP || utils.DetermineRegion() == utils.REGION_GCP
	currVersion = env.ImageVersion()
}

func InitLogger(logger *Logger) {
	defaultLogger.Stop()
	defaultLogger = logger
	defaultLogger.StartLogger()
	defaultLogger.SetCallDepth(3)
}

func AddProvider(p LogProvider) {
	defaultLogger.AddProvider(p)
}

func AddProcessor(p Processor) {
	defaultLogger.AddProcessor(p)
}

func SetLevel(l int) {
	defaultLogger.SetLevel(l)
}

func SetSyncProcessLevel(l int) {}

func EnableDynamicLogLevel() {
	defaultLogger.EnableDynamicLogLevel()
}

func SetCallDepth(depth int) {
	defaultLogger.SetCallDepth(depth)
}

// SetSecMark enables the default logger to add security marks to key-value pairs
// Meantime, it also forces the logger to use the full path and enables the logger
// to output the current app version in the log.
func SetSecMark(isEnable bool) {
	enableSecMark = isEnable
	if isEnable {
		defaultLogger.UseFullPath(true)
	}
}

// UseFullPath sets whether to print the full filepath in the log
func UseFullPath(isEnable bool) {
	defaultLogger.UseFullPath(isEnable)
}

func IsSecMarkEnabled() bool {
	return enableSecMark
}

// SetCurrentVersion set the version of current app
func SetCurrentVersion(version string) {
	currVersion = version
}

func Stop() {
	defaultLogger.Stop()
}

func DefaultLogger() *Logger {
	return defaultLogger
}

func Fatalf(format string, v ...interface{}) {
	defaultLogger.Fatal(format, v...)
}

func Errorf(format string, v ...interface{}) {
	defaultLogger.Error(format, v...)
}

func Warnf(format string, v ...interface{}) {
	defaultLogger.Warn(format, v...)
}

func Noticef(format string, v ...interface{}) {
	defaultLogger.Notice(format, v...)
}

func Infof(format string, v ...interface{}) {
	defaultLogger.Info(format, v...)
}

func Debugf(format string, v ...interface{}) {
	if defaultLogger.level > LevelDebug {
		return
	}
	defaultLogger.Debug(format, v...)
}

func Tracef(format string, v ...interface{}) {
	defaultLogger.Trace(format, v...)
}

func Fatal(format string, v ...interface{}) {
	defaultLogger.Fatal(format, v...)
}

func Error(format string, v ...interface{}) {
	defaultLogger.Error(format, v...)
}

func Warn(format string, v ...interface{}) {
	defaultLogger.Warn(format, v...)
}

func Notice(format string, v ...interface{}) {
	defaultLogger.Notice(format, v...)
}

func Info(format string, v ...interface{}) {
	defaultLogger.Info(format, v...)
}

func Debug(format string, v ...interface{}) {
	if defaultLogger.level > LevelDebug {
		return
	}
	defaultLogger.Debug(format, v...)
}

func Trace(format string, v ...interface{}) {
	defaultLogger.Trace(format, v...)
}

func Flush() {
	defaultLogger.Flush()
}

func CtxFatal(ctx context.Context, format string, v ...interface{}) {
	defaultLogger.CtxFatal(ctx, format, v...)
}

func CtxError(ctx context.Context, format string, v ...interface{}) {
	defaultLogger.CtxError(ctx, format, v...)
}

func CtxWarn(ctx context.Context, format string, v ...interface{}) {
	defaultLogger.CtxWarn(ctx, format, v...)
}

func CtxNotice(ctx context.Context, format string, v ...interface{}) {
	defaultLogger.CtxNotice(ctx, format, v...)
}

func CtxInfo(ctx context.Context, format string, v ...interface{}) {
	defaultLogger.CtxInfo(ctx, format, v...)
}

func CtxDebug(ctx context.Context, format string, v ...interface{}) {
	defaultLogger.CtxDebug(ctx, format, v...)
}

func CtxTrace(ctx context.Context, format string, v ...interface{}) {
	defaultLogger.CtxTrace(ctx, format, v...)
}

func CtxFatalKvs(ctx context.Context, kvs ...interface{}) {
	defaultLogger.CtxFatalKVs(ctx, kvs...)
}

func CtxErrorKvs(ctx context.Context, kvs ...interface{}) {
	defaultLogger.CtxErrorKVs(ctx, kvs...)
}

func CtxWarnKvs(ctx context.Context, kvs ...interface{}) {
	defaultLogger.CtxWarnKVs(ctx, kvs...)
}

func CtxNoticeKvs(ctx context.Context, kvs ...interface{}) {
	defaultLogger.CtxNoticeKVs(ctx, kvs...)
}

func CtxInfoKvs(ctx context.Context, kvs ...interface{}) {
	defaultLogger.CtxInfoKVs(ctx, kvs...)
}

func CtxDebugKvs(ctx context.Context, kvs ...interface{}) {
	defaultLogger.CtxDebugKVs(ctx, kvs...)
}

func CtxTraceKvs(ctx context.Context, kvs ...interface{}) {
	defaultLogger.CtxTraceKVs(ctx, kvs...)
}

func CtxPushNotice(ctx context.Context, k, v interface{}) {
	ntc := GetNotice(ctx)
	if ntc == nil {
		return
	}
	ntc.PushNotice(k, v)
}

func CtxFlushNotice(ctx context.Context) {
	ntc := GetNotice(ctx)
	if ntc == nil {
		return
	}
	kvs := ntc.KVs()
	if len(kvs) == 0 {
		return
	}
	defaultLogger.CtxNoticeKVs(ctx, kvs...)
}

func NewNoticeCtx(ctx context.Context) context.Context {
	ntc := NewNoticeKVs()
	return context.WithValue(ctx, noticeCtxKey, ntc)
}

func CtxAddKVs(ctx context.Context, kvs ...interface{}) context.Context {
	return ctxAddKVs(ctx, kvs...)
}

// SecMark add double brackets to the key-value pairs and return the generated string.
// This is a function that directly exported to users. So we need to check the type of the parameters
func SecMark(key, val interface{}) string {
	keyStr := utils.Value2Str(key)
	valStr := utils.Value2Str(val)

	if enableSecMark {
		return "{{" + keyStr + "=" + valStr + "}}"
	}

	return keyStr + "=" + valStr
}

// PrintStack prints the stacks in Info level.
// If printAllGoroutines is true, it prints the stacks of all goroutines.
// Otherwise, it just prints the current goroutine's stack.
func PrintStack(printAllGoroutines bool) {
	ctx := context.Background()
	if printAllGoroutines {
		ctx = CtxStackInfo(ctx, AllGoroutines)
	} else {
		ctx = CtxStackInfo(ctx, CurrGoroutine)
	}
	defaultLogger.CtxInfo(ctx, "")
}

// AddLogMsgProcessor adds a LogMsgProcessor to the default logger.
// You should call this function in the init function.
func AddLogMsgProcessor(p LogMsgProcessor) {
	defaultLogger.AddLogMsgProcessor(p)
}
