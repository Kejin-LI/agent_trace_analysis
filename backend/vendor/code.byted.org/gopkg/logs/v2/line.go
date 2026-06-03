package logs

import (
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
)

// Line creates a cached source code file line information by sync.Once
// to avoid reflect call of each log printing.
type Line struct {
	sync.Once
	literal []byte
}

func (l *Line) load(callDepth int, fullPath bool) []byte {
	l.Do(func() {
		if len(l.literal) != 0 {
			return
		}
		_, file, line, ok := runtime.Caller(callDepth + preparedCallDepthOffset)
		if !fullPath {
			file = filepath.Base(file)
		}
		if ok {
			l.literal = append(l.literal, file+":"+strconv.Itoa(line)...)
		}
	})
	return l.literal
}
