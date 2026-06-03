package logs

import (
	"code.byted.org/gopkg/logs/v2/writer"
)

// Option provides some options to config ByteDLogger.
type Option func(*ByteDLogger)

// SetPSM sets psm to ByteDLogger, logger would read env.PSM as default.
func SetPSM(psm string) Option {
	return func(logger *ByteDLogger) {
		logger.psm = psm
	}
}

// SetWriter sets ByteDLogger outputs to which writers.
func SetWriter(level Level, ws ...writer.LogWriter) Option {
	return func(logger *ByteDLogger) {
		logger.minLevel = FatalLevel
		logger.writers = logger.writers[:0]
		for _, w := range ws {
			logger.addWriter(level, w)
		}
	}
}

// SetPadding sets ByteDLogger padding between each element.
func SetPadding(padding string) Option {
	return func(logger *ByteDLogger) {
		logger.padding = []byte(padding)
	}
}

// SetCallDepth sets ByteDLogger call depth while logging file location.
func SetCallDepth(c int) Option {
	return func(logger *ByteDLogger) {
		logger.callDepth = c
	}
}

// SetTracing sets a compatible tracing writer to trace level logs,
// it is only used in compatible cases,
// you have to migrate to Trace 2.0 as soon as possible.
func SetTracing() Option {
	return func(logger *ByteDLogger) {
		logger.addWriter(TraceLevel, writer.NewTraceAgentWriter())
	}
}

func SetMiddleware(m ...Middleware) Option {
	return func(logger *ByteDLogger) {
		logger.middlewares = append(logger.middlewares, m...)
	}
}

// SetFullPath sets print the full path of log file location.
func SetFullPath() Option {
	return func(logger *ByteDLogger) {
		logger.fullPath = true
	}
}

// SetZoneInfo sets if the time string include zone info
// example:
//   include=false:  2021-07-15 15:19:14,161
//   include=true:   2021-07-15 15:19:14,161 +0800
func SetZoneInfo(include bool) Option {
	return func(logger *ByteDLogger) {
		logger.includeZoneInfo = include
	}
}

// AppendWriter sets ByteDLogger outputs to which writers.
// It will not remove existing writers.
func AppendWriter(level Level, ws ...writer.LogWriter) Option {
	return func(logger *ByteDLogger) {
		for _, w := range ws {
			logger.addWriter(level, w)
		}
	}
}

// ConfigSecMark sets if the logger needs to add double brackets to key-value pairs
// example:
//   isEnabled=false:  count=100
//   isEnabled=true:   {{count=100}}
// If you also specify the current version of your app, otherwise it will use
// the image tag by default
func ConfigSecMark(isEnabled bool) Option {
	return func(logger *ByteDLogger) {
		enableSecMark = isEnabled
		if isEnabled {
			logger.fullPath = true
		}
	}
}

// SetCurrentVersion sets the current version. Call it in program initialization.
func SetCurrentVersion(version string) {
	currVersion = version
}

// Deprecated: KVList is always enabled.
func SetEnableKVList(isEnabled bool) Option {
	return func(logger *ByteDLogger) {
	}
}

// SetSecMark updates enableSecMark. Call it in program initialization.
func SetSecMark(isEnabled bool) {
	enableSecMark = isEnabled
}

func IsSecMarkEnabled() bool {
	return enableSecMark
}

func SetFatalOSExit(isEnabled bool) Option {
	return func(logger *ByteDLogger) {
		logger.exitWhenFatal = isEnabled
	}
}

func SetKVPosition(position KVPosition) Option {
	return func(logger *ByteDLogger) {
		logger.kvPosition = position
	}
}
