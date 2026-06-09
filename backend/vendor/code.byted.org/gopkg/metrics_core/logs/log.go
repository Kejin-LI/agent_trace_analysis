package logs

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
)

const ErrorPrefix = "[Metrics(v4)]"

type LogLevel int

const (
	VerboseLevel LogLevel = iota
	DebugLevel
	InfoLevel
	WarnLevel
	ErrorLevel
	NoneLevel // disable all log

	defaultLogLevel = InfoLevel
)

var (
	levelOut = map[LogLevel]io.Writer{
		VerboseLevel: os.Stdout,
		DebugLevel:   os.Stdout,
		InfoLevel:    os.Stdout,
		WarnLevel:    os.Stdout,
		ErrorLevel:   os.Stderr,
	}

	mockLogOut io.Writer
)

var (
	once           sync.Once
	logLevel       LogLevel
	polishStyle    bool
	metricsLogFile string
)

func Verbose(f string, p ...interface{}) {
	if logLevel > VerboseLevel {
		return
	}

	if polishLog() {
		defaultLogger.NoLevel().Logf(f, p...)
	} else {
		printLog(getLogOut(VerboseLevel), f, p...)
	}
}

func Debug(f string, p ...interface{}) {
	if logLevel > DebugLevel {
		return
	}

	if polishLog() {
		defaultLogger.Debug().Logf(f, p...)
	} else {
		printLog(getLogOut(DebugLevel), f, p...)
	}
}

func Info(f string, p ...interface{}) {
	if logLevel > InfoLevel {
		return
	}

	if polishLog() {
		defaultLogger.Info().Logf(f, p...)
	} else {
		printLog(getLogOut(InfoLevel), f, p...)
	}
}

func Warn(f string, p ...interface{}) {
	if logLevel > WarnLevel {
		return
	}

	if polishLog() {
		defaultLogger.Warn().Logf(f, p...)
	} else {
		printLog(getLogOut(WarnLevel), f, p...)
	}
}

func Error(f string, p ...interface{}) {
	if logLevel > ErrorLevel {
		return
	}

	if polishLog() {
		defaultLogger.Error().Logf(f, p...)
	} else {
		printLog(getLogOut(ErrorLevel), f, p...)
	}
}

func getLogOut(level LogLevel) io.Writer {
	if mockLogOut != nil {
		return mockLogOut
	}
	if out, ok := levelOut[level]; ok {
		return out
	}
	return os.Stdout
}

func printLog(out io.Writer, f string, p ...interface{}) {
	_, _ = fmt.Fprintf(out, ErrorPrefix+" "+f+"\n", p...)
}

func polishLog() bool {
	return polishStyle
}

func setLogLevel(level string) {
	switch level {
	case strconv.Itoa(int(VerboseLevel)), "verb", "verbose":
		logLevel = VerboseLevel
	case strconv.Itoa(int(DebugLevel)), "debug":
		logLevel = DebugLevel
	case strconv.Itoa(int(InfoLevel)), "info":
		logLevel = InfoLevel
	case strconv.Itoa(int(WarnLevel)), "warn", "warning":
		logLevel = WarnLevel
	case strconv.Itoa(int(ErrorLevel)), "err", "error":
		logLevel = ErrorLevel
	case strconv.Itoa(int(NoneLevel)), "none":
		logLevel = NoneLevel
	default:
		logLevel = defaultLogLevel
	}
}

func Setup(logLevel string, polishLog bool, logFile string) {
	once.Do(func() {
		polishStyle = polishLog
		setLogLevel(logLevel)
		metricsLogFile := logFile
		if metricsLogFile != "" {
			defaultLogger = NewDefaultFileLogger(metricsLogFile)
		}
	})
}

func init() {
	// init the defaultLogger, make sure logger functions available anywhere
	logLevel = InfoLevel
	defaultLogger = NewDefaultLogger()
}
