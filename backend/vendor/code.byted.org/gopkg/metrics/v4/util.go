package metrics

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime/debug"
	"strconv"

	"code.byted.org/aiops/metrics_codec"
	"code.byted.org/gopkg/env"
)

const (
	maxTagLen = 255
	maxASCII  = '\u007F' // unicode.MaxASCII
)

type logLevel int

var LogLevel logLevel

const (
	VerboseLevel logLevel = iota
	DebugLevel
	InfoLevel
	WarnLevel
	ErrorLevel
	NoneLevel // disable all log

	defaultLogLevel = InfoLevel
)

var (
	levelOut = map[logLevel]io.Writer{
		VerboseLevel: os.Stdout,
		DebugLevel:   os.Stdout,
		InfoLevel:    os.Stdout,
		WarnLevel:    os.Stdout,
		ErrorLevel:   os.Stderr,
	}

	mockLogOut     io.Writer
	versionPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[a-zA-Z0-9.]+)*$`)
)

func init() {
	setLogLevelByEnv()
	metricsLogFile := os.Getenv("METRICS_FILE_LOG")
	if metricsLogFile != "" {
		if env.PSM() != "-" {
			metricsLogFile = env.PSM() + "." + metricsLogFile
		}
		filename := metricsLogFile
		absFilepath := fmt.Sprintf("/opt/tiger/toutiao/log/app/%s.log", filename)
		defaultLogger = NewDefaultFileLogger(absFilepath)
	} else {
		defaultLogger = NewDefaultLogger()
	}
}

func setLogLevelByEnv() {
	switch os.Getenv(LogLevelEnvKey) {
	case strconv.Itoa(int(VerboseLevel)), "verb", "verbose":
		LogLevel = VerboseLevel
	case strconv.Itoa(int(DebugLevel)), "debug":
		LogLevel = DebugLevel
	case strconv.Itoa(int(InfoLevel)), "info":
		LogLevel = InfoLevel
	case strconv.Itoa(int(WarnLevel)), "warn", "warning":
		LogLevel = WarnLevel
	case strconv.Itoa(int(ErrorLevel)), "err", "error":
		LogLevel = ErrorLevel
	case strconv.Itoa(int(NoneLevel)), "none":
		LogLevel = NoneLevel
	default:
		LogLevel = defaultLogLevel
	}
}

func getLogOut(level logLevel) io.Writer {
	if mockLogOut != nil {
		return mockLogOut
	}
	if out, ok := levelOut[level]; ok {
		return out
	}
	return os.Stdout
}

func verboseLog(f string, p ...interface{}) {
	if LogLevel > VerboseLevel {
		return
	}

	if polishLog {
		defaultLogger.NoLevel().Logf(f, p...)
	} else {
		printLog(getLogOut(VerboseLevel), f, p...)
	}
}

func debugLog(f string, p ...interface{}) {
	if LogLevel > DebugLevel {
		return
	}

	if polishLog {
		defaultLogger.Debug().Logf(f, p...)
	} else {
		printLog(getLogOut(DebugLevel), f, p...)
	}
}

func infoLog(f string, p ...interface{}) {
	if LogLevel > InfoLevel {
		return
	}

	if polishLog {
		defaultLogger.Info().Logf(f, p...)
	} else {
		printLog(getLogOut(InfoLevel), f, p...)
	}
}

func warnLog(f string, p ...interface{}) {
	if LogLevel > WarnLevel {
		return
	}

	if polishLog {
		defaultLogger.Warn().Logf(f, p...)
	} else {
		printLog(getLogOut(WarnLevel), f, p...)
	}

}

func errorLog(f string, p ...interface{}) {
	if LogLevel > ErrorLevel {
		return
	}

	if polishLog {
		defaultLogger.Error().Logf(f, p...)
	} else {
		printLog(getLogOut(ErrorLevel), f, p...)
	}

}

func printLog(out io.Writer, f string, p ...interface{}) {
	_, _ = fmt.Fprintf(out, errorPrefix+" "+f+"\n", p...)
}

// log print info level log to stdout
// Deprecated: use infoLog instead
func log(f string, p ...interface{}) {
	infoLog(f, p...)
}

// IsValidName checks whether the string is valid for a metric name or tag name.
func IsValidName(s string) bool {
	return metrics_codec.IsValidName(s)
}

// IsValidTagValue checks whether the string is valid for a tag value.
func IsValidTagValue(s string) bool {
	return metrics_codec.IsValidTagValue(s)
}

// IsRuneSpecial checks whether the rune is a special rune for tag values, which cannot be the first or the last char of the string.
// For example, white space is a special rune. Tag values cannot start or end with ' '.
func IsRuneSpecial(b rune) bool {
	return metrics_codec.IsRuneSpecial(b)
}

// IsRuneValidForName checks whether the rune is valid for a metric name or a tag name.
func IsRuneValidForName(b rune) bool {
	return metrics_codec.IsRuneValidForName(b)
}

// IsRuneValidForValue checks whether the rune is valid for a tag value.
func IsRuneValidForValue(b rune) bool {
	return metrics_codec.IsRuneValidForValue(b)
}

func validateVersion(version string) bool {
	return versionPattern != nil && versionPattern.MatchString(version) && IsValidTagValue(version)
}

func getVersionInternal(packageName string) string {
	versionUnknown := "unknown"
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return versionUnknown
	}

	for _, dep := range info.Deps {
		if dep.Path == packageName && validateVersion(dep.Version) {
			return dep.Version
		}
	}
	return versionUnknown
}
