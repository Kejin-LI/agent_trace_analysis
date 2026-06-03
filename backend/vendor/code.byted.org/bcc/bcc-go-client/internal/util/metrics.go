package util

import (
	"time"

	"code.byted.org/bcc/bcc-go-client"
	metrics "code.byted.org/bcc/tools/bmetrics/v3"
	"code.byted.org/gopkg/env"
	m "code.byted.org/gopkg/metrics/v3"
)

var metricClient *metrics.MetricsClient

var (
	errorEvent           = "error"
	streamAction         = getPSMMetricsName("stream_action")
	registerEvent        = "register"
	closeEvent           = "close"
	abnormalClose        = "abnormal_close"
	callbackTime         = getPSMMetricsName("callback.latency")
	abnormalCallbackTime = "abnormal.callback.latency"
	watchEvent           = getPSMMetricsName("watch")
	errorWatch           = "error.watch"
	downloadTime         = getPSMMetricsName("download.latency")
	pathInitTime         = "path.latency"
	flowStat             = "stat.flow"
	keyStat              = "stat.key"
	slaTime              = "sla.confupdate.latency"
	abnormalSLATime      = "sla.abnormal.confupdate.latency"
	slaTimeWithConfSize  = "sla.confupdate.latency.confsize"
	pingSLA              = "sla.ping"
	registerSLA          = "sla.register"
	connectSLA           = "sla.connect"
	findBestPushSvr      = "best_push_svr"
	fetchError           = "fetch.error"            //拉渠道失败
	downloadBigFileError = "download.bigfile.error" //下载大文件失败
	cacheDirError        = "cache.dir.error"        //cache目录无法使用
)

func InitMetricsClient() {
	metricClient = metrics.NewMetricsClient(
		metrics.WithPrefix("bytedance.bcc.sdk"),
		metrics.WithMetrics(map[string][]string{
			errorEvent:           {"key", "msg"},
			streamAction:         {"action", "result"},
			registerEvent:        {"name", "sdk_version", "sdk_lang", "sdk_path", "connect", "first"},
			closeEvent:           {"connect", "reason"},
			abnormalClose:        {"connect", "reason"},
			callbackTime:         {"key"},
			abnormalCallbackTime: {"key"},
			watchEvent:           {"key", "version", "source", "result"},
			errorWatch:           {"key", "version", "source", "result"},
			downloadTime:         {"key", "version", "source", "result"},
			pathInitTime:         {"path", "count"},
			flowStat:             {"source"},
			keyStat:              {"source"},
			slaTime:              {"key_name"},
			abnormalSLATime:      {"key_name", "channel", "updateType", "configSize"},
			slaTimeWithConfSize:  {"key_name"},
			pingSLA:              {"result"},
			registerSLA:          {"result"},
			connectSLA:           {"result"},
			findBestPushSvr:      {"result"},
			fetchError:           {},
			downloadBigFileError: {"keyname", "has_agent"},
			cacheDirError:        {"dir"},
		},
		),
	)

}

func EmitFindBestPushSvrCounter(result string) {
	tags := []m.T{
		{Name: "result", Value: result},
	}

	metricClient.EmitCounter(findBestPushSvr, 1, tags...)
}

// 处理key的error，包括回调函数返回的
func EmitError(key, msg string) {
	tags := []m.T{
		{Name: "key", Value: key},
		{Name: "msg", Value: msg},
	}
	metricClient.EmitCounter(errorEvent, 1, tags...)
}
func EmitStreamAction(action, result string, count int) {
	tags := []m.T{
		{Name: "action", Value: action},
		{Name: "result", Value: result},
	}
	metricClient.EmitCounter(streamAction, count, tags...)
}

// 注册事件
func EmitRegister(name string, connect int, sdkLang string, sdkPath string) {
	tags := []m.T{
		{Name: "name", Value: name},
		{Name: "sdk_version", Value: bcc.SDKVersion()},
		{Name: "sdk_lang", Value: sdkLang},
		metrics.Tag("sdk_path", sdkPath),
		metrics.Tag("connect", connect),    //>=0
		metrics.Tag("first", connect == 1), // 废弃, 直接通过connect == 0判断是否首次链接
	}
	metricClient.EmitCounter(registerEvent, 1, tags...)
}
func EmitClose(connect int, normal bool, reason string) {
	if normal {
		EmitNormalClose(connect, reason)
	} else {
		EmitAbnormalClose(connect, reason)
	}
}

// 关闭事件
func EmitNormalClose(connect int, reason string) {
	tags := []m.T{
		metrics.Tag("connect", connect), //>=1
		{Name: "reason", Value: reason},
	}
	metricClient.EmitCounter(closeEvent, 1, tags...)
}
func EmitAbnormalClose(connect int, reason string) {
	tags := []m.T{
		metrics.Tag("connect", connect), //>=1
		{Name: "reason", Value: reason},
	}
	metricClient.EmitCounter(abnormalClose, 1, tags...)
}

// 回调时间（微妙）
func EmitCallbackLatency(key string, ti time.Time) {
	cost := time.Since(ti)
	tags := []m.T{
		{Name: "key", Value: key},
	}
	metricClient.EmitTimer(callbackTime, ti, tags...)
	if cost.Milliseconds() > 200*time.Millisecond.Milliseconds() {
		EmitAbnormalCallback(key, cost)
	}
}
func EmitAbnormalCallback(key string, cost time.Duration) {
	tags := []m.T{

		{Name: "key", Value: key},
	}
	metricClient.EmitTimer(abnormalCallbackTime, int(cost.Milliseconds()), tags...)
}

// 监听事件（watchKey或watchPath）
func EmitWatch(key string, version int, source string) {
	tags := []m.T{
		{Name: "key", Value: key},
		metrics.Tag("version", version),
		{Name: "source", Value: source},
	}

	metricClient.EmitCounter(watchEvent, 1, tags...)

}
func EmitErrorWatch(key string, version int, source string, result string) {
	tags := []m.T{
		{Name: "key", Value: key},
		metrics.Tag("version", version),
		{Name: "source", Value: source},
		{Name: "result", Value: result},
	}
	name := errorWatch
	metricClient.EmitCounter(name, 1, tags...)
}

// 总下载耗时（tos或p2p或file）
func EmitDownloadLatency(key string, version int, source string, result string, ti time.Time) {
	tags := []m.T{
		{Name: "key", Value: key},
		metrics.Tag("version", version),
		{Name: "source", Value: source},
		{Name: "result", Value: result},
	}

	metricClient.EmitTimer(downloadTime, ti, tags...)
}

// path初始化耗时
func EmitPathLatency(path string, count int, ti time.Time) {
	tags := []m.T{
		{Name: "path", Value: path},
		metrics.Tag("count", count),
	}
	metricClient.EmitTimer(pathInitTime, ti, tags...)
}

// 每30秒，统计带宽（成功才统计）
func EmitStatFlow(source string, value int) {
	tags := []m.T{

		{Name: "source", Value: source},
	}
	metricClient.EmitCounter(flowStat, value, tags...)
}

// 每30秒，统计成功的key数（成功才统计）
func EmitStatKey(source string, value int) {
	tags := []m.T{

		{Name: "source", Value: source},
	}
	metricClient.EmitCounter(keyStat, value, tags...)
}

func EmitConfUpdateLatency(keyName string, channel string, updateType string, confSize int, cost int) {

	formatFileSize := func(size int) string {
		var _1KB, _1MB, _1GB = 1024, 1024 * 1024, 1024 * 1024 * 1024
		if size <= 500 {
			return "500B"
		} else if size <= _1KB {
			return "1KB"
		} else if size <= 8*_1KB {
			return "8KB"
		} else if size <= 30*_1KB {
			return "30KB"
		} else if size <= 100*_1KB {
			return "100KB"
		} else if size <= 500*_1KB {
			return "500KB"
		} else if size <= _1MB {
			return "1MB"
		} else if size <= 10*_1MB {
			return "10MB"
		} else if size <= 100*_1MB {
			return "100MB"
		} else if size <= 500*_1MB {
			return "500MB"
		} else if size <= _1GB {
			return "1GB"
		}
		return "greater1GB"
	}
	isAbnormalCostTime := func(cost int, size int) bool {
		var _1KB, _1MB = 1024, 1024 * 1024
		if size <= 500*_1KB {
			return cost >= 6*1000
		} else if size <= 10*_1MB {
			return cost >= 30*1000
		} else if size <= 500*_1MB {
			return cost >= 60*1000
		} else {
			return cost >= 10*60*1000
		}
	}

	if cost < 1000*3600*24 { //大于1天的下发时间，可以认为waiter或者时间戳啥的有问题。这种极端case先不统计，避免打点失效
		emitNormalLatencySLA(keyName, cost)
		emitConfSizeUpdateLatency(keyName, formatFileSize(confSize), cost)
	}

	if isAbnormalCostTime(cost, confSize) {
		emitAbnormalUpdateLatency(keyName, channel, updateType, formatFileSize(confSize), cost)
	}

}
func emitNormalLatencySLA(keyName string, cost int) {
	tags := []m.T{
		{Name: "key_name", Value: keyName},
	}
	metricClient.EmitStore(slaTime, cost, tags...)
}
func emitConfSizeUpdateLatency(keyName string, confSize string, cost int) {
	tags := []m.T{
		{Name: "key_name", Value: keyName},
	}
	metricClient.EmitStoreWithSuffix(slaTimeWithConfSize, confSize, cost, tags...)
}
func emitAbnormalUpdateLatency(keyName string, channel string, updateType string, confSize string, cost int) {
	tags := []m.T{
		{Name: "key_name", Value: keyName},
		{Name: "channel", Value: channel},
		{Name: "updateType", Value: updateType},
		{Name: "configSize", Value: confSize},
	}
	metricClient.EmitStore(abnormalSLATime, cost, tags...)
}

// 收发ping包的可用性
func EmitPingSLA(result string) {
	tags := []m.T{
		{Name: "result", Value: result}, //success, fail
	}
	metricClient.EmitCounter(pingSLA, 1, tags...)
}

// 连接并发送pipe包的可用性
func EmitConnectSLA(result string) {
	tags := []m.T{
		{Name: "result", Value: result}, //success, fail
	}
	metricClient.EmitCounter(connectSLA, 1, tags...)
}

// register包的发送可用性
func EmitRegisterSLA(result string) {
	tags := []m.T{
		{Name: "result", Value: result}, //success, fail
	}
	metricClient.EmitCounter(registerSLA, 1, tags...)
}

func getPSMMetricsName(source string) string {
	return env.PSM() + "." + source
}

func EmitFetchError(count int) {
	tags := []m.T{}

	metricClient.EmitCounter(fetchError, count, tags...)
}

func EmitDownloadBigFileError(keyname string, hasAgent bool, count int) {
	hasAgentStr := "false"
	if hasAgent {
		hasAgentStr = "true"
	}
	tags := []m.T{
		{Name: "keyname", Value: keyname},
		{Name: "has_agent", Value: hasAgentStr},
	}

	metricClient.EmitCounter(downloadBigFileError, count, tags...)
}

func emitCacheDirError(dir string, count int) {
	tags := []m.T{
		{Name: "dir", Value: dir},
	}

	metricClient.EmitCounter(cacheDirError, count, tags...)
}
