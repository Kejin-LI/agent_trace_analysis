package metrics

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	klog "github.com/go-kit/log"
	"github.com/go-logfmt/logfmt"
)

const (
	LevelDebug = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
	LevelNo
)

var (
	levels              = []string{"Debug", "Info", "Warn", "Error", "Fatal", "NoLevel"}
	logPrefix           = []byte("[Metrics(v3)] ")
	tsFormat            = "2006-01-02 15:04:05.000"
	defaultLogger       MetricLogger
	errOddKeyValueCount = fmt.Errorf("the size of key-value list is odd")
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

func NewLogger(w io.Writer, keyvalues ...interface{}) MetricLogger {
	kvs := make([]interface{}, 0, len(keyvalues))
	kvs = append(kvs, keyvalues...)
	l := &contextualLogger{
		kvs: kvs,
		w:   w,
	}

	for i := 0; i < 5; i++ {
		kvlist := append([]interface{}{}, "level", levels[i])
		kvlist = append(kvlist, keyvalues...)
		l.loggers[i] = newComponentLogger(w, kvlist...)
	}

	kvlist := append([]interface{}{}, keyvalues...)
	l.loggers[LevelNo] = newComponentLogger(w, kvlist...)
	return l
}

func NewDefaultLogger() MetricLogger {
	return NewLogger(
		NewConsoleWriter(),
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
		NewFileWriter(filename, Daily),
		"ts",
		klog.TimestampFormat(
			func() time.Time { return time.Now().Local() },
			tsFormat,
		),
		"caller",
		klog.Caller(4),
	)
}

func With(logger MetricLogger, keyvalues ...interface{}) MetricLogger {
	kvs := make([]interface{}, 0, len(logger.kvlist())+len(keyvalues))
	kvs = append(kvs, logger.kvlist()...)
	kvs = append(kvs, keyvalues...)
	return NewLogger(logger.writer(), kvs...)
}

func (s *contextualLogger) kvlist() []interface{} {
	return s.kvs
}

func (s *contextualLogger) writer() io.Writer {
	return s.w
}

func (s *contextualLogger) Debug() ComponentLogger {
	return s.loggers[LevelDebug]
}

func (s *contextualLogger) Info() ComponentLogger {
	return s.loggers[LevelInfo]
}

func (s *contextualLogger) Warn() ComponentLogger {
	return s.loggers[LevelWarn]
}

func (s *contextualLogger) Error() ComponentLogger {
	return s.loggers[LevelError]
}

func (s *contextualLogger) Fatal() ComponentLogger {
	return s.loggers[LevelFatal]
}

func (s *contextualLogger) NoLevel() ComponentLogger {
	return s.loggers[LevelNo]
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

const dateFormat = "2006-01-02_15"

// RotationWindow allows to claim which rotation window provider uses.
type RotationWindow int8

const (
	// Daily means rotate daily.
	Daily RotationWindow = iota
	// Hourly means rotate hourly.
	Hourly
)

type ConsoleWriter struct {
	prefix []byte
	io.Writer
}

func NewConsoleWriter() *ConsoleWriter {
	return &ConsoleWriter{
		prefix: nil,
		Writer: os.Stdout,
	}
}

func (w *ConsoleWriter) Write(bytes []byte) (int, error) {
	if len(w.prefix) == 0 {
		if len(bytes) > 0 && bytes[len(bytes)-1] != '\n' {
			bytes = append(bytes, '\n')
		}
		return w.Writer.Write(bytes)
	}

	p := packetPool.Get().(*packet)
	*p = (*p)[:0]
	defer p.Recycle()
	*p = append(*p, w.prefix...)
	*p = append(*p, bytes...)
	if len(*p) > 0 && (*p)[len(*p)-1] != '\n' {
		*p = append(*p, '\n')
	}
	return w.Writer.Write(*p)
}

// FileWriter provides a file rotated output to loggers,
// it is thread-safe and uses memory buffer to boost file writing performance.
type FileWriter struct {
	file           *rotatedFile
	filename       string
	rotationWindow RotationWindow
	fileCountLimit int

	currentTimeSeg time.Time
	sync.RWMutex
}

// NewFileWriter creates a FileWriter.
func NewFileWriter(filename string, window RotationWindow, options ...FileOption) io.Writer {
	w := &FileWriter{
		filename:       filename,
		rotationWindow: window,
	}
	file, err := w.loadFile()
	if err != nil {
		panic(err)
	}
	w.file = newRotatedFile(file)
	for _, op := range options {
		op(w)
	}
	return w
}

func (w *FileWriter) loadFile() (io.WriteCloser, error) {
	timedName, currentTimeSeg, err := timedFilename(w.filename)
	if err != nil {
		return nil, err
	}
	err = os.MkdirAll(filepath.Dir(timedName), os.ModeDir|os.ModePerm)
	if err != nil {
		return nil, err
	}
	var file *os.File
	if env := os.Getenv("IS_PROD_RUNTIME"); len(env) == 0 {
		file, err = os.OpenFile(timedName, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	} else {
		file, err = os.OpenFile(timedName, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
	}
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(w.filename); err == nil {
		_ = os.Remove(w.filename)
	}
	_ = os.Symlink(filepath.Base(timedName), w.filename)
	w.currentTimeSeg = currentTimeSeg
	return file, nil
}

func (w *FileWriter) checkIfNeedRotate(logTime time.Time) error {
	var needRotate bool

	switch w.rotationWindow {
	case Daily:
		if w.currentTimeSeg.YearDay() != logTime.YearDay() {
			needRotate = true
		}
	case Hourly:
		if w.currentTimeSeg.Hour() != logTime.Hour() || w.currentTimeSeg.YearDay() != logTime.YearDay() {
			needRotate = true
		}
	default:
		needRotate = false
	}

	if needRotate {
		defer func() {
			go w.cleanFiles(w.fileCountLimit)
		}()
		if err := w.rotate(); err != nil {
			return err
		}
	}
	return nil
}

func (w *FileWriter) cleanFiles(limit int) {
	if limit <= 0 {
		return
	}
	logs := make([]string, 0)
	_ = filepath.Walk(filepath.Dir(w.filename), func(path string, info os.FileInfo, err error) error {
		if strings.HasPrefix(path, w.filename+".") {
			logs = append(logs, path)
		}
		return nil
	})

	if len(logs) <= limit {
		return
	}
	sort.Slice(logs, func(i, j int) bool {
		return getFileDate(logs[i]).After(getFileDate(logs[j]))
	})
	for _, f := range logs[limit:] {
		_ = os.Remove(f)
	}
}

func (w *FileWriter) rotate() error {
	file, err := w.loadFile()
	if err != nil {
		return err
	}
	w.file.Rotate(file)
	return nil
}

func (w *FileWriter) Write(data []byte) (int, error) {
	w.Lock()
	err := w.checkIfNeedRotate(time.Now())
	w.Unlock()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "write file %s error: %s\n", w.filename, err)
	}
	return w.file.Write(data)
}

func (w *FileWriter) Close() error {
	return w.file.Close()
}

func (w *FileWriter) Flush() error {
	return w.file.Flush()
}

func timedFilename(filename string) (string, time.Time, error) {
	var now time.Time
	absPath, err := filepath.Abs(filename)
	if err != nil {
		return "", now, err
	}
	now = time.Now()
	return absPath + "." + now.Format(dateFormat), now, nil
}

type syncWriter struct {
	*bufio.Writer
	sync.Mutex
}

func newSyncWriter(w io.Writer) *syncWriter {
	return &syncWriter{
		Writer: bufio.NewWriterSize(w, 8*1024),
	}
}

type rotatedFile struct {
	w *syncWriter
	sync.WaitGroup
	done chan bool
}

func newRotatedFile(file io.WriteCloser) *rotatedFile {
	f := &rotatedFile{
		newSyncWriter(file),
		sync.WaitGroup{},
		make(chan bool),
	}
	f.Add(1)
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		defer func() {
			if err := recover(); err != nil {
				buf := make([]byte, 4096)
				n := runtime.Stack(buf, false)
				_, _ = fmt.Fprintf(os.Stderr, string(buf[:n]))
			}
		}()

		for {
			select {
			case <-f.done:
				ticker.Stop()
				_ = f.Flush()
				f.Done()
				return
			case <-ticker.C:
				err := f.Flush()
				if err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "log writes file error: %s", err)
				}
			}
		}
	}()
	return f
}

func (f *rotatedFile) Close() error {
	f.done <- true
	f.Wait()
	return nil
}

func (f *rotatedFile) Rotate(w io.WriteCloser) {
	f.w.Lock()
	defer f.w.Unlock()
	_ = f.w.Flush()
	f.w.Reset(w)
}

func (f *rotatedFile) Flush() error {
	var err error
	f.w.Lock()
	defer f.w.Unlock()
	err = f.w.Writer.Flush()
	if err != nil {
		return err
	}
	return nil
}

func (f *rotatedFile) Write(c []byte) (int, error) {
	f.w.Lock()
	defer f.w.Unlock()
	if f.w.Buffered()+len(c) > 4096 {
		_ = f.w.Flush()
	}
	n, err := f.w.Write(c)
	if err != nil {
		return n, err
	}
	if len(c) == 0 || c[len(c)-1] != '\n' {
		_, _ = f.w.Write([]byte{'\n'})
	}
	return n + 1, nil
}

func getFileDate(name string) time.Time {
	sn := strings.Split(name, ".")
	t, _ := time.Parse(dateFormat, sn[len(sn)-1])
	return t
}

type FileOption func(writer *FileWriter)

func SetKeepFiles(n int) FileOption {
	return func(writer *FileWriter) {
		writer.fileCountLimit = n
	}
}
