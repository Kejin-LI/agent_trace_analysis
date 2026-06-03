package metrics

import (
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"strconv"
	"unsafe"

	"code.byted.org/gopkg/env"
	"code.byted.org/gopkg/metrics/v3/msgpack"
)

const (
	maxTagLen = 255
	maxASCII  = '\u007F' // unicode.MaxASCII
)

const (
	mask64 uint64 = 0x8080808080808080
	mask32 uint32 = 0x80808080
)

func isValidString(s string) bool {
	if len(s) == 0 || len(s) > maxTagLen {
		return false
	}

	ptr := unsafe.Pointer((*reflect.StringHeader)(unsafe.Pointer(&s)).Data)
	size := len(s)
	offset := 0

	for ; offset+8 <= size; offset += 8 {
		d := *(*uint64)(unsafe.Pointer(uintptr(ptr) + uintptr(offset)))
		if d&mask64 != 0 {
			return false
		}
	}

	if offset+4 <= size {
		d := *(*uint32)(unsafe.Pointer(uintptr(ptr) + uintptr(offset)))
		if d&mask32 != 0 {
			return false
		}
		offset += 4
	}

	// rest bytes
	for ; offset < size; offset++ {
		b := *(*byte)(unsafe.Pointer(uintptr(ptr) + uintptr(offset)))
		if b > maxASCII {
			return false
		}
	}
	return true
}

func isAgentSidecar() bool {
	if sidecar := os.Getenv("METRICSERVER2_SIDECAR"); sidecar == "1" {
		return true
	}

	return false
}

func appendTags(b []byte, tt []T) []byte {
	sep := len(b) > 0
	for _, t := range tt {
		if !isValidString(t.Name) || !isValidString(t.Value) {
			continue
		}
		if sep {
			b = append(b, '|')
		}
		b = append(b, t.Name...)
		b = append(b, '=')
		b = append(b, t.Value...)
		sep = true
	}
	return b
}

func appendTagsWithoutValidationByRaw(b []byte, name, value string) []byte {
	if len(b) > 0 {
		b = append(b, '|')
	}
	b = append(b, name...)
	b = append(b, '=')
	b = append(b, value...)
	return b
}

func int64StrSize(n int64) int {
	sz := 1
	if n < 0 {
		sz++
		n = -n
	}
	for n >= 10 {
		sz++
		n /= 10
	}
	return sz
}

func appendInt64(b []byte, n int64) []byte {
	if n == 0 {
		return append(b, '0')
	}
	if n < 0 {
		b = append(b, '-')
		n = -n
	}
	var tmp [32]byte
	buf := tmp[:]
	i := len(buf)
	for q := int64(0); n >= 10; {
		i--
		q = n / 10
		buf[i] = '0' + byte(n-q*10)
		n = q
	}
	i--
	buf[i] = '0' + byte(n)
	return append(b, buf[i:]...)
}

func floatStrSize(f float64) int {
	i := 0
	n := int64(f)
	if n == math.MinInt64 || n == math.MaxInt64 {
		return 1
	}
	if float64(n) == f {
		return int64StrSize(n)
	}
	if n < 0 || f < 0 {
		i++
		n = -n
		f = -f
	}
	if n == 0 {
		i++
	}
	for n > 0 {
		i++
		n = n / 10
	}
	n = int64(f * 100000) // with 5 prec
	if n == math.MaxInt64 || n <= 0 {
		return i
	}
	n = n % 100000 // 99999
	if n == 0 {
		return i
	}
	j := 5
	for n%10 == 0 {
		n = n / 10
		j--
	}
	i += 1 // '.'
	for n > 0 {
		n = n / 10
		i++
		j--
	}
	i += j // 0.000...
	return i
}

func appendFloat64(b []byte, f float64) []byte {
	n := int64(f)
	if n == math.MinInt64 || n == math.MaxInt64 {
		return append(b, '0')
	}
	if float64(n) == f {
		return appendInt64(b, n)
	}
	if f < 0 || n < 0 {
		b = append(b, '-')
		n = -n
		f = -f
	}
	b = appendInt64(b, n)

	n = int64(f * 100000)
	if n == math.MaxInt64 || n <= 0 {
		return b
	}
	n = n % 100000
	if n == 0 {
		return b
	}
	j := 5
	for n%10 == 0 {
		n = n / 10
		j--
	}
	b = append(b, '.')
	var tmp [8]byte
	buf := tmp[:]
	buf = appendInt64(buf[:0], n)
	for i := 0; i < j-len(buf); i++ {
		b = append(b, '0')
	}
	return append(b, buf...)
}

func appendFloat64WithSize(p []byte, f float64) []byte {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		p = msgpack.AppendStringHeader(p, 1)
		p = append(p, '0')
		return p
	}
	headerOffset := len(p)
	p = msgpack.AppendStringHeader(p, 1)
	startOffset := len(p)
	p = strconv.AppendFloat(p, f, 'f', -1, 64)
	length := len(p) - startOffset
	msgpack.FillStringHeader(p, headerOffset, uint16(length))
	return p
}

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

	mockLogOut io.Writer
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
// Depecated: use infoLog instead
func log(f string, p ...interface{}) {
	infoLog(f, p...)
}

const (
	offset32 = 2166136261
	prime32  = 16777619
)

// hashNew initializes a new fnv64a hash value.
func hashNew() uint32 {
	return offset32
}

// hashAdd adds a string to a fnv32a hash value, returning the updated hash.
func hashAdd(h uint32, s string) uint32 {
	for _, char := range s {
		h ^= uint32(char)
		h *= prime32
	}
	return h
}
