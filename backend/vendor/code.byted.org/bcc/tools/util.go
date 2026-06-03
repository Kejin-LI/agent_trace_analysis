package tools

import (
	"fmt"
	"os"
	"reflect"
	"runtime"
	"sync"
	"time"

	"code.byted.org/gopkg/logs"
)

//------------------ 通用函数 -----------------------
func Exit(format string, v ...interface{}) {
	if format != "" {
		logs.SetCallDepth(4)
		logs.Fatal(format, v...)
		logs.Flush()
		time.Sleep(time.Millisecond * 50)
		logs.Stop()
		os.Exit(-1)
	} else {
		logs.Flush()
		time.Sleep(time.Millisecond * 50)
		logs.Stop()
		os.Exit(0)
	}
}

func Panic(format string, v ...interface{}) {
	s := fmt.Sprintf(format, v...)
	logs.SetCallDepth(4)
	logs.Fatal(s)
	logs.Flush()
	time.Sleep(time.Millisecond * 50)
	logs.Stop()
	panic(s)
}

func Must(err error) {
	if err != nil {
		Panic(err.Error())
	}
}

//暂停进程，用于测试
func Pause(msg string) {
	logs.Warn("Pause msg=%v caller=%v", msg, CallerName(1))
	select {}
}

//首次才返回true，否则返回false //业务方保证不重名？
func Once(key string) bool {
	_, loaded := g_onceMap.LoadOrStore(key, true)
	return loaded == false
}

var g_onceMap sync.Map

//------------------ goos -----------------------
func IsWindows() bool {
	return runtime.GOOS == "windows"
}

func IsLinux() bool {
	return runtime.GOOS == "linux"
}

func IsDarwin() bool {
	return runtime.GOOS == "darwin" //macOS和iOS
}

func GetStructName(obj interface{}) string {
	var stName string
	if t := reflect.TypeOf(obj); t.Kind() == reflect.Ptr {
		stName = t.Elem().Name()
	} else {
		stName = t.Name()
	}
	return stName
}
