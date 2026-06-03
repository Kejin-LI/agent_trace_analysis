package tools

import (
	"runtime"
	"strings"
)

//调用栈的名字 //skip=0~N (效率底，用于测试)
//short=0为调用者，-1为CallerName
//short=false  ret=code.byted.org/toutiao/easygo/util/uruntime.CallerName
//short=true   ret=uruntime.CallerName
func CallerName(skip int, short ...bool) (name string) {
	pc, _, _, _ := runtime.Caller(skip + 1)
	f := runtime.FuncForPC(pc)
	if f == nil {
		return
	}
	name = f.Name()
	if len(short) == 1 && short[0] {
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
	}
	return
}

//调用栈的文件名和行数 //skip=0~N (效率底，用于测试)
func CallerFileline(skip int, short ...bool) (file string, line int) {
	pc, _, _, _ := runtime.Caller(skip + 1)
	f := runtime.FuncForPC(pc)
	if f == nil {
		return
	}
	file, line = f.FileLine(pc)
	if len(short) == 1 && short[0] {
		if idx := strings.LastIndex(file, "/"); idx >= 0 {
			file = file[idx+1:]
		}
	}
	return
}

//调用栈 //short=true时合成一行方便logs打印
func CallerStack(short ...bool) string {
	const size = 64 << 10
	buf := make([]byte, size)
	buf = buf[:runtime.Stack(buf, false)]

	if len(short) > 0 && short[0] {
		for k, v := range buf {
			if v == '\n' {
				buf[k] = '~'
			}
		}
	}
	return string(buf)
}
