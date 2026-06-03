package logs

import (
	"path/filepath"
	"runtime"
	"strconv"
	"unsafe"

	"code.byted.org/gopkg/env"
)

type prefixedLog struct {
	Log
}

func (l *prefixedLog) Time(includeZone bool) *prefixedLog {
	if l == nil {
		return nil
	}
	l.executors = append(l.executors, func(l *Log) {
		current := current()
		l.time = current.Time
		if includeZone {
			l.buf = append(l.buf, current.serialBytes...)
		} else {
			l.buf = append(l.buf, current.serialBytes[:len(current.serialBytes)-5]...)
		}

		l.buf = append(l.buf, ' ')
	})
	return l
}

func (l *prefixedLog) Version() *prefixedLog {
	if l == nil {
		return nil
	}
	l.executors = append(l.executors, func(l *Log) {
		l.appendStrings(version, " ")
	})
	return l
}

func (l *prefixedLog) Location() *prefixedLog {
	if l == nil {
		return nil
	}
	l.executors = append(l.executors, func(l *Log) {
		var fileLine string
		if len(l.loc) > 0 {
			fileLine = *(*string)(unsafe.Pointer(&l.loc))
		} else if l.line != nil {
			f := l.line.load(l.logger.callDepth+l.callDepthOffset, l.logger.fullPath || enableSecMark)
			fileLine = *(*string)(unsafe.Pointer(&f))
		} else {
			_, file, line, ok := runtime.Caller(l.logger.callDepth + l.callDepthOffset)
			if ok {
				if !(l.logger.fullPath || enableSecMark) {
					file = filepath.Base(file)
				}
				fileLine = file + ":" + strconv.Itoa(line)
			} else {
				fileLine = "?:?"
			}
		}
		l.appendStrings(fileLine, " ")
		l.loc = l.buf[len(l.buf)-len(fileLine)-1 : len(l.buf)-1]
	})
	return l
}

func (l *prefixedLog) Level() *prefixedLog {
	if l == nil {
		return nil
	}
	l.executors = append(l.executors, func(l *Log) {
		l.appendStrings(l.level.String(), " ")
	})
	return l
}

func (l *prefixedLog) Host() *prefixedLog {
	if l == nil {
		return nil
	}
	l.executors = append(l.executors, func(l *Log) {
		l.appendStrings(env.HostIP(), " ")
	})
	return l
}

func (l *prefixedLog) PSM(psm string) *prefixedLog {
	if l == nil {
		return nil
	}
	l.psm = append(l.psm, psm...)
	l.executors = append(l.executors, func(l *Log) {
		l.buf = append(l.buf, l.psm...)
		l.buf = append(l.buf, ' ')
	})
	return l
}

func (l *prefixedLog) LogID() *prefixedLog {
	if l == nil {
		return nil
	}
	l.executors = append(l.executors, func(l *Log) {
		l.appendStrings(logIDFromContext(l.ctx), " ")
	})
	return l
}

func (l *prefixedLog) SpanID() *prefixedLog {
	if l == nil {
		return nil
	}
	l.executors = append(l.executors, func(l *Log) {
		l.buf = strconv.AppendUint(l.buf, spanIDFromContext(l.ctx), 10)
		l.buf = append(l.buf, ' ')
	})
	return l
}

func (l *prefixedLog) Cluster() *prefixedLog {
	if l == nil {
		return nil
	}
	l.executors = append(l.executors, func(l *Log) {
		l.appendStrings(env.Cluster(), " ")
	})
	return l
}

func (l *prefixedLog) Stage() *prefixedLog {
	if l == nil {
		return nil
	}
	l.executors = append(l.executors, func(l *Log) {
		l.appendStrings(env.Stage(), " ")
	})
	return l
}

func (l *prefixedLog) End() *Log {
	if l == nil {
		return nil
	}
	return (*Log)(unsafe.Pointer(l))
}
