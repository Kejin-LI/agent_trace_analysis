package bmetrics

import (
	"time"

	"code.byted.org/bcc/tools/uconv"
	"code.byted.org/gopkg/metrics"
)

type Client = ClientV2
type T = metrics.T

//init //可以不调用
func Init(psm string) {
	defClient = getClient(psm)
}

//new client
func NewClient(psm string) *Client {
	return getClient(psm)
}

//flush: 退出时主动调用防止丢数据
func Flush() {
	defClient.Flush()
}

//emit counter
func EmitCounter(name string, value interface{}, tags ...metrics.T) {
	defClient.EmitCounter(name, value, tags...)
}

//emit rate counter
func EmitRateCounter(name string, value interface{}, tags ...metrics.T) {
	defClient.EmitRateCounter(name, value, tags...)
}

//emit meter (counter + rate counter)
func EmitMeter(name string, value interface{}, tags ...metrics.T) {
	defClient.EmitMeter(name, value, tags...)
}

//emit timer
func EmitTimer(name string, value interface{}, tags ...metrics.T) {
	defClient.EmitTimer(name, value, tags...)
}

//emit store
func EmitStore(name string, value interface{}, tags ...metrics.T) {
	defClient.EmitStore(name, value, tags...)
}

//---------------- 业务封装 ----------------
// 通用错误
func EmitError(msg string) {
	defClient.EmitError(msg)
}

// 通用错误（废弃）
func EmitWarn(msg string) {
	defClient.EmitWarn(msg)
}

// 通用告警
func EmitAlarm(msg string) {
	defClient.EmitAlarm(msg)
}

// 通用函数统计：调用次数和错误标记、调用耗时和超时标记（module=mysql|redis|mq|app|...）(必须通过defer使用)  // counter类型+timer类型
func EmitFunc(module string, key string, err *error, timeout time.Duration, t0 time.Time) {
	defClient.EmitFunc(module, key, err, timeout, t0)
}

// 通用store：存放服务级的数据，例如每N秒更新的数据
func EmitGoStore(name string, value int) {
	defClient.EmitGoStore(name, value)
}

// 通用counter
func EmitGoCounter(name string, value int) {
	defClient.EmitGoCounter(name, value)
}

// 通用timer
func EmitGoTimer(name string, value interface{}) {
	defClient.EmitGoTimer(name, value)
}

//-------------------------------------------
// value不允许空格 //总体比Tag2慢5%，因为EmitCounter自身很大消耗
func Tag(name string, value interface{}) metrics.T {
	return metrics.T{Name: name, Value: uconv.ToString(value)}
}

func Tag2(name string, value string) metrics.T {
	return metrics.T{Name: name, Value: value}
}

// 空就设置为none，方便标签过滤
func FormatEmpty(s string) string {
	return formatEmpty(s)
}

// 把特殊字符都转换为'_' // 尽量少用
func FormatAny(s string) string {
	return formatAny(s)
}
