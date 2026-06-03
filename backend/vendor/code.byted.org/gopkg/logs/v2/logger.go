package logs

import (
	"fmt"
	"sync/atomic"

	"code.byted.org/gopkg/logs/v2/writer"
)

type leveledWriter struct {
	writer.LogWriter
	minLevel Level
}

type logger struct {
	writers         []leveledWriter
	middlewares     []Middleware
	padding         []byte
	closed          bool
	callDepth       int
	minLevel        Level
	fullPath        bool
	includeZoneInfo bool
	exitWhenFatal   bool // This flag indicates whether it calls os.Exit(1) when it prints fatal logs
	kvPosition      KVPosition

	rateLimiters writer.RateLimiters
}

func NewLogger() *logger {
	return &logger{
		middlewares:  make([]Middleware, 0),
		callDepth:    defaultCallDepth,
		minLevel:     FatalLevel,
		rateLimiters: writer.NewRateLimiterMap(),
	}
}

func (l *logger) addWriter(level Level, w writer.LogWriter) {
	if level < l.minLevel {
		l.minLevel = level
	}
	l.writers = append(l.writers, leveledWriter{LogWriter: w, minLevel: level})
}

func (l *logger) newLog(level Level) *Log {
	// If the level is less than the minLevel of the writers
	// We don't need to process this log, it won't output anything
	if level < l.GetLevel() {
		return nil
	}
	return newLog(level, l)
}

// Trace gets (or creates) a Log instance from the logPool
// And sets some of its fields: level and logger
func (l *logger) Trace() *Log {
	return l.newLog(TraceLevel)
}

func (l *logger) Debug() *Log {
	return l.newLog(DebugLevel)
}

func (l *logger) Info() *Log {
	return l.newLog(InfoLevel)
}

func (l *logger) Notice() *Log {
	return l.newLog(NoticeLevel)
}

func (l *logger) Warn() *Log {
	return l.newLog(WarnLevel)
}

func (l *logger) Error() *Log {
	return l.newLog(ErrorLevel)
}

func (l *logger) Fatal() *Log {
	return l.newLog(FatalLevel)
}

func (l *logger) Flush() error {
	err := make([]error, 0)
	for _, w := range l.writers {
		writerError := w.LogWriter.Flush()
		if writerError != nil {
			err = append(err, writerError)
		}
	}
	if len(err) != 0 {
		return fmt.Errorf("flush error: %#v", err)
	}
	return nil
}

func (l *logger) Close() error {
	if l.closed {
		return nil
	}
	for _, w := range l.writers {
		err := w.Close()
		if err != nil {
			return err
		}
	}
	l.closed = true
	return nil
}

// GetLevel gets the minimal level specified in logger writers.
func (l *logger) GetLevel() Level {
	level := atomic.LoadInt32((*int32)(&l.minLevel))
	return Level(level)
}

// SetLevel sets the minimal level for the logger. It is safe to increase the level.
// Please not decrease the level directly. Use SetLevelForWriters instead.
func (l *logger) SetLevel(newLevel Level) {
	atomic.StoreInt32((*int32)(&l.minLevel), int32(newLevel))
}

// SetLevelForWriters updates the minimal level for the writer. It may also update the loggers' level.
func (l *logger) SetLevelForWriters(newLevel Level, logWriters ...writer.LogWriter) {
	for _, lw := range logWriters {
		for i, _ := range l.writers {
			if l.writers[i].LogWriter == lw {
				atomic.StoreInt32((*int32)(&l.writers[i].minLevel), int32(newLevel))
				if l.GetLevel() > newLevel {
					l.SetLevel(newLevel)
				}
				break
			}
		}
	}
}

func (w *leveledWriter) getLevel() Level {
	level := atomic.LoadInt32((*int32)(&w.minLevel))
	return Level(level)
}
