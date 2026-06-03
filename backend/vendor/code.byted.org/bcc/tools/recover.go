package tools

import (
	"context"
	"fmt"
	"reflect"
	"runtime"

	"code.byted.org/bcc/tools/bmetrics"
	"code.byted.org/gopkg/env"
	"code.byted.org/gopkg/logs"
	"code.byted.org/gopkg/metrics"
)

//Recover功能：recover、metrics、logs、jobs（&error、func(interface{})）
func Recover(ctx context.Context, msg string, jobs ...interface{}) {
	e := recover()
	if e != nil {
		buf := make([]byte, 64<<10)
		buf = buf[:runtime.Stack(buf, false)]

		if recoverMode == 1 {
			logs.Warn("Recover panic msg='%v' err=%v", msg, e)
		} else {
			if ctx != nil {
				logs.CtxError(ctx, "Recover panic msg='%v' err=%v stack=%v", msg, e, string(buf))
			} else {
				logs.Error("Recover panic msg='%v' err=%v stack=%v", msg, e, string(buf))
			}
		}

		bmetrics.EmitCounter("go.panic", 1, metrics.T{"cluster", env.Cluster()}, metrics.T{"msg", msg})
		bmetrics.EmitError("panic")

		for _, job := range jobs {
			switch v := job.(type) {
			case func(interface{}):
				v(e)
			case func():
				v()
			case *error:
				*v = fmt.Errorf("panic %v", e)
			default:
				logs.Error("Recover invalid job=%v", reflect.TypeOf(job))
			}
		}
	}
}

var recoverMode = 0 //recover打印模式：0为打印堆栈，1为打印一行警告日志（方便测试）
