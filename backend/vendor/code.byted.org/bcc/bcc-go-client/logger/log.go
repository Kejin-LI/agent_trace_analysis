package logger

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"

	"code.byted.org/gopkg/env"
	"code.byted.org/gopkg/logs/v2"
	"code.byted.org/gopkg/logs/v2/writer"
)

var prefix = "[bccclient] "

var defaultFileName = fmt.Sprintf("/opt/tiger/toutiao/log/app/%s-bcc.log", env.PSM())
var logger atomic.Value

// 是否为 debug 模式，由 TCC_DEBUG 环境变量控制，debug 模式下会打印详细日志
var isDebugMode bool

type option struct {
	level        logs.Level
	fileName     string
	rotateWindow writer.RotationWindow
	fullOmit     bool
	writers      []writer.LogWriter
	rateLimit    int
	disableAgent bool
}

type Option func(*option)

// WithRateLimit max write frequency in ONE second, can not less than 1
func WithRateLimit(rate int) Option {
	return func(o *option) {
		o.rateLimit = rate
	}
}

// WithLogLevel log writer level
func WithLogLevel(level logs.Level) Option {
	return func(o *option) {
		o.level = level
	}
}

func WithLogPrefix(pre string) Option {
	return func(o *option) {
		prefix = pre
	}
}

func WithConsoleWriter() Option {
	return func(o *option) {
		// console writer does not need to be duplication
		for _, w := range o.writers {
			if _, ok := w.(*writer.ConsoleWriter); ok {
				return
			}
		}
		o.writers = append(o.writers, writer.NewConsoleWriter(writer.SetColorful(true)))
	}
}

// WithFileName file writer target file name
func WithFileName(fileName string) Option {
	return func(o *option) {
		o.fileName = fileName
	}
}

// WithRotateWindow file writer file rotate window
func WithRotateWindow(window writer.RotationWindow) Option {
	return func(o *option) {
		o.rotateWindow = window
	}
}

// WithFullOmit allows AsyncWriter omits the log if the buffer is full or not.
func WithFullOmit(fullOmit bool) Option {
	return func(o *option) {
		o.fullOmit = fullOmit
	}
}

// WithDisableAgentWriter allows to close the log agent writer
func WithDisableAgentWriter() Option {
	return func(o *option) {
		o.disableAgent = true
	}
}

func getDefaultOption() option {
	return option{
		level:        logs.InfoLevel,
		fileName:     defaultFileName,
		rotateWindow: writer.Hourly,
		fullOmit:     false,
		writers:      nil,
		rateLimit:    0,
		disableAgent: false,
	}
}
func getOption(opts ...Option) option {
	opt := getDefaultOption()
	for _, o := range opts {
		o(&opt)
	}
	return opt
}

func (o *option) getWriterOption() logs.Option {
	writers := o.writers
	if env.InTCE() || o.fileName != defaultFileName {
		writers = append(writers, writer.NewAsyncWriter(writer.NewFileWriter(o.fileName, o.rotateWindow), o.fullOmit))
	}
	if env.InTCE() && !o.disableAgent {
		writers = append(writers, writer.NewAgentWriter())
	}
	if o.rateLimit >= 1 {
		for i := range writers {
			writers[i] = writer.NewRateLimitWriter(writers[i], o.rateLimit)
		}
	}
	return logs.AppendWriter(o.level, writers...)
}
func init() {
	isDebugMode = os.Getenv("TCC_DEBUG") == "1"
	opt := getDefaultOption()
	setLogger(newLogger(opt))
}

func SetLogger(opts []Option) {
	opt := getOption(opts...)
	setLogger(newLogger(opt))
}

func SetLoggerRaw(l *logs.CompatLogger) {
	setLogger(l)
}

func newLogger(opt option) *logs.CompatLogger {
	if isDebugMode { // debug 模式下打印所有日志
		opt.level = logs.TraceLevel
	}
	ops := make([]logs.Option, 0)
	ops = append(ops, opt.getWriterOption())
	if needAddConsoleWriter(opt) {
		ops = append(ops, logs.AppendWriter(opt.level, writer.NewConsoleWriter(writer.SetColorful(true))))
	}
	ops = append(ops, logs.SetCallDepth(3))
	return logs.NewCompatLogger(ops...)
}
func setLogger(l *logs.CompatLogger) {
	logger.Store(l)
}

func getLogger() *logs.CompatLogger {
	return logger.Load().(*logs.CompatLogger)
}

// 是否要添加 console writer
func needAddConsoleWriter(opt option) bool {
	// 线上环境不需要
	if env.InTCE() {
		return false
	}
	// 已经有了 console writer 且 level < Error 则不要再次添加
	for _, w := range opt.writers {
		if _, ok := w.(*writer.ConsoleWriter); ok {
			if opt.level < logs.ErrorLevel {
				return false
			}
		}
	}
	return true
}

func CtxInfo(ctx context.Context, format string, args ...interface{}) {
	getLogger().CtxInfo(ctx, prefix+format, args...)
}

func CtxError(ctx context.Context, format string, args ...interface{}) {
	getLogger().CtxError(ctx, prefix+format, args...)
}
func CtxWarn(ctx context.Context, format string, args ...interface{}) {
	getLogger().CtxWarn(ctx, prefix+format, args...)
}
func CtxDebug(ctx context.Context, format string, args ...interface{}) {
	getLogger().CtxDebug(ctx, prefix+format, args...)
}
func CtxTrace(ctx context.Context, format string, args ...interface{}) {
	getLogger().CtxTrace(ctx, prefix+format, args...)
}
func CtxFatal(ctx context.Context, format string, args ...interface{}) {
	getLogger().CtxFatal(ctx, prefix+format, args...)
}
func Error(format string, args ...interface{}) {
	getLogger().Error(prefix+format, args...)
}
func Warn(format string, args ...interface{}) {
	getLogger().Warn(prefix+format, args...)
}
func Debug(format string, args ...interface{}) {
	getLogger().Debug(prefix+format, args...)
}
func Trace(format string, args ...interface{}) {
	getLogger().Trace(prefix+format, args...)
}
func Fatal(format string, args ...interface{}) {
	getLogger().Fatal(prefix+format, args...)
}
func Info(format string, args ...interface{}) {
	getLogger().Info(prefix+format, args...)
}
func Notice(format string, args ...interface{}) {
	getLogger().Notice(prefix+format, args...)
}
func Flush() {
	getLogger().Flush()
}
func Stop() {
	getLogger().Stop()
}
