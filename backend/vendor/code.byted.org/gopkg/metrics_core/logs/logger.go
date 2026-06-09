package logs

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"time"

	"code.byted.org/gopkg/metrics_core/logs/writers"
	klog "github.com/go-kit/log"
	"github.com/go-logfmt/logfmt"
)

const (
	levelDebug = iota
	levelInfo
	levelWarn
	levelError
	levelFatal
	levelNo
)

var (
	levels              = []string{"Debug", "Info", "Warn", "Error", "Fatal", "NoLevel"}
	tsFormat            = "2006-01-02 15:04:05.000"
	errOddKeyValueCount = fmt.Errorf("the size of key-value list is odd")
	logPrefix           = []byte(fmt.Sprintf("%s ", ErrorPrefix))

	defaultLogger MetricLogger
)

type MetricLogger interface {
	kvlist() []interface{}
	writer() io.Writer
	Debug() ComponentLogger
	Info() ComponentLogger
	Warn() ComponentLogger
	Error() ComponentLogger
	Fatal() ComponentLogger
	NoLevel() ComponentLogger
}

type ComponentLogger interface {
	klog.Logger
	Logf(format string, a ...interface{}) error
}

type contextualLogger struct {
	kvs     []interface{}
	w       io.Writer
	loggers [6]ComponentLogger
}

func NewLogger(w io.Writer, keyValues ...interface{}) MetricLogger {
	kvs := make([]interface{}, 0, len(keyValues))
	kvs = append(kvs, keyValues...)
	l := &contextualLogger{
		kvs: kvs,
		w:   w,
	}

	for i := 0; i < 5; i++ {
		kvlist := append([]interface{}{}, "level", levels[i])
		kvlist = append(kvlist, keyValues...)
		l.loggers[i] = newComponentLogger(w, kvlist...)
	}

	kvlist := append([]interface{}{}, keyValues...)
	l.loggers[levelNo] = newComponentLogger(w, kvlist...)
	return l
}

func NewDefaultLogger() MetricLogger {
	return NewLogger(
		writers.NewConsoleWriter(),
		"ts",
		klog.TimestampFormat(
			func() time.Time { return time.Now().Local() },
			tsFormat,
		),
		"caller",
		klog.Caller(4),
	)
}

func NewDefaultFileLogger(filename string) MetricLogger {
	return NewLogger(
		writers.NewFileWriter(filename, writers.Daily),
		"ts",
		klog.TimestampFormat(
			func() time.Time { return time.Now().Local() },
			tsFormat,
		),
		"caller",
		klog.Caller(4),
	)
}

func With(logger MetricLogger, keyValues ...interface{}) MetricLogger {
	kvs := make([]interface{}, 0, len(logger.kvlist())+len(keyValues))
	kvs = append(kvs, logger.kvlist()...)
	kvs = append(kvs, keyValues...)
	return NewLogger(logger.writer(), kvs...)
}

func (s *contextualLogger) kvlist() []interface{} {
	return s.kvs
}

func (s *contextualLogger) writer() io.Writer {
	return s.w
}

func (s *contextualLogger) Debug() ComponentLogger {
	return s.loggers[levelDebug]
}

func (s *contextualLogger) Info() ComponentLogger {
	return s.loggers[levelInfo]
}

func (s *contextualLogger) Warn() ComponentLogger {
	return s.loggers[levelWarn]
}

func (s *contextualLogger) Error() ComponentLogger {
	return s.loggers[levelError]
}

func (s *contextualLogger) Fatal() ComponentLogger {
	return s.loggers[levelFatal]
}

func (s *contextualLogger) NoLevel() ComponentLogger {
	return s.loggers[levelNo]
}

type componentLogger struct {
	*logHandler
	kvs []interface{}
}

func newComponentLogger(w io.Writer, keyvalues ...interface{}) *componentLogger {
	l := &componentLogger{
		logHandler: newLogHandler(w),
		kvs:        nil,
	}
	l.kvs = append(l.kvs, keyvalues...)
	return l
}

func (l *componentLogger) Log(keyvalues ...interface{}) error {
	if len(keyvalues)%2 != 0 {
		return errOddKeyValueCount
	}

	kvs := make([]interface{}, 0, len(l.kvs))
	kvs = append(kvs, l.kvs...)
	kvs = append(kvs, keyvalues...)
	evaluate(kvs)
	return l.logHandler.writeKVs(kvs)
}

func (l *componentLogger) Logf(s string, v ...interface{}) error {
	message := fmt.Sprintf(s, v...)
	kvs := make([]interface{}, 0, len(l.kvs)+2)
	kvs = append(kvs, l.kvs...)
	kvs = append(kvs, "msg", message)
	evaluate(kvs)
	return l.logHandler.writeKVs(kvs)
}

func evaluate(keyvalues []interface{}) {
	for i, value := range keyvalues {
		if i&1 == 0 {
			continue
		}
		if v, ok := value.(klog.Valuer); ok {
			keyvalues[i] = v()
		}
	}
}

type logHandler struct {
	io.Writer
}

func newLogHandler(w io.Writer) *logHandler {
	return &logHandler{w}
}

func (l *logHandler) writeKVs(keyvalues []interface{}) error {
	enc := kvEncoderPool.Get().(*kvEncoder)
	enc.Reset()
	defer func() {
		kvEncoderPool.Put(enc)
	}()

	var err error

	if err = enc.EncodeKeyvals(keyvalues...); err != nil {
		return err
	}

	if err = enc.EndRecord(); err != nil {
		return err
	}
	_, err = l.Write(enc.buf.Bytes())
	return err
}

type kvEncoder struct {
	*logfmt.Encoder
	buf    bytes.Buffer
	prefix []byte
}

func (l *kvEncoder) Reset() {
	l.Encoder.Reset()
	l.buf.Reset()
	l.buf.Write(l.prefix)
}

var kvEncoderPool = sync.Pool{
	New: func() interface{} {
		var enc kvEncoder
		enc.Encoder = logfmt.NewEncoder(&enc.buf)
		enc.prefix = logPrefix
		return &enc
	},
}
