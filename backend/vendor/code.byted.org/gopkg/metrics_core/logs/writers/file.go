package writers

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const dateFormat = "2006-01-02_15"

// RotationWindow allows to claim which rotation window provider uses.
type RotationWindow int8

const (
	// Daily means rotate daily.
	Daily RotationWindow = iota
	// Hourly means rotate hourly.
	Hourly
)

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
	// todo: fix me: remove dependency on env
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
				buf := make([]byte, 256)
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

type syncWriter struct {
	*bufio.Writer
	sync.Mutex
}

func newSyncWriter(w io.Writer) *syncWriter {
	return &syncWriter{
		Writer: bufio.NewWriterSize(w, 8*1024),
	}
}
