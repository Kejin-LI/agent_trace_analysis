package util

import (
	"fmt"
	"time"

	metrics "code.byted.org/bcc/tools/bmetrics/v3"
	m "code.byted.org/gopkg/metrics/v3"
)

var sidecarMetrics *metrics.MetricsClient

var (
	sidecarError        = "error"
	downloadError       = "download.error"
	panicError          = "panic"
	sidecarSuccess      = getPSMMetricsName("success") // 用于用户查看psm的状况
	sidecarDownloadCost = getPSMMetricsName("download.cost")
	sidecarConnection   = getPSMMetricsName("connection")
	sidecarCount        = "count" // 用于大盘
	sidecarCost         = "cost"  // 大盘，整体初始化sidecar耗时
	sidecarServerPanic  = "sidecar.panic"
	sidecarDownload     = "sidecar.download"
	sidecarUsing        = "sidecar.using"
)

func init() {
	sidecarMetrics = metrics.NewMetricsClient(
		metrics.WithPrefix("bytedance.bcc.sidecar"),
		metrics.WithMetrics(map[string][]string{
			sidecarSuccess:     {},
			sidecarCost:        {},
			sidecarCount:       {},
			sidecarServerPanic: {"exit_code"},
			sidecarDownload:    {"url", "version", "msg"},
			sidecarUsing:       {"version", "result"},
		}),
	)
}

func EmitSidecarError(msg string) {

}

func EmitSidecarUsing(version, result string) {
	tags := []m.T{
		{Name: "version", Value: version},
		{Name: "result", Value: result},
	}

	sidecarMetrics.EmitCounter(sidecarUsing, 1, tags...)
}

func EmitSidecarDownload(url, version, msg string) {
	if url == "" {
		url = "-"
	}
	if version == "" {
		version = "-"
	}
	tags := []m.T{
		{Name: "url", Value: url},
		{Name: "version", Value: version},
		{Name: "msg", Value: msg},
	}

	sidecarMetrics.EmitCounter(sidecarDownload, 1, tags...)
}

func EmitSidecarPanic(exitCode int, count int) {
	exitCodeStr := fmt.Sprintf("%d", exitCode)
	tags := []m.T{
		{Name: "exit_code", Value: exitCodeStr},
	}
	sidecarMetrics.EmitCounter(sidecarServerPanic, 1, tags...)
}

func EmitSidecarSuccess(cost time.Duration) {
	sidecarMetrics.EmitCounter(sidecarSuccess, 1)
	sidecarMetrics.EmitTimer(sidecarCost, cost)
	sidecarMetrics.EmitCounter(sidecarCount, 1)
}

func EmitSidecarConnectionInfo() {

}
