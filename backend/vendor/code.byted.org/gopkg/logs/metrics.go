package logs

import (
	"time"

	"context"

	"code.byted.org/gopkg/env"
	"code.byted.org/gopkg/metrics"
)

var (
	metricsClient *metrics.MetricsClientV2

	metricsTagWarn = []metrics.T{
		{Name: "level", Value: "WARNING"},
		{Name: "cluster", Value: env.Cluster()},
		{Name: "env", Value: env.Env()},
		{Name: "version", Value: "v1.0.0"},
	}
	metricsTagError = []metrics.T{
		{Name: "level", Value: "ERROR"},
		{Name: "cluster", Value: env.Cluster()},
		{Name: "env", Value: env.Env()},
		{Name: "version", Value: "v1.0.0"},
	}
	metricsTagFatal = []metrics.T{
		{Name: "level", Value: "CRITICAL"},
		{Name: "cluster", Value: env.Cluster()},
		{Name: "env", Value: env.Env()},
		{Name: "version", Value: "v1.0.0"},
	} // 和py统一, 将fatal打成critical
	clientReport = []metrics.T{
		{Name: "env", Value: env.Env()},
		{Name: "version", Value: "v1.0.0"},
	}
	metricsLim = 4 //  只打Warn及以上的日志,
)

func init() {
	metricsClient = metrics.NewDefaultMetricsClientV2("toutiao.service.log", true)
	go func() {
		for {
			_ = metricsClient.EmitStore("client.liveness", 1, clientReport...)
			time.Sleep(15 * time.Second)
		}
	}()
}

// FIXME: it's strange to do metrics in logs SDK
// TODO: Use LogHook for metrics, refer: https://github.com/sirupsen/logrus/blob/master/hooks.go#L8
func doMetrics(ctx context.Context, logLevel int, psm string) error {
	if logLevel < metricsLim {
		return nil
	}

	if len(psm) == 0 {
		return nil
	}

	var err error
	if logLevel == 4 { // warning
		if ctx != nil {
			metricsTags := getMetricTags(ctx)
			if len(metricsTags) != 0 {
				tags := make([]metrics.T, 0, len(metricsTagWarn)+len(metricsTags))
				tags = append(tags, metricsTagWarn...)
				tags = append(tags, metricsTags...)
				return metricsClient.EmitCounter(psm+".throughput", 1, tags...)
			}
		}
		err = metricsClient.EmitCounter(psm+".throughput", 1, metricsTagWarn...)
	} else if logLevel == 5 { // error
		if ctx != nil {
			metricsTags := getMetricTags(ctx)
			if len(metricsTags) != 0 {
				tags := make([]metrics.T, 0, len(metricsTagError)+len(metricsTags))
				tags = append(tags, metricsTagError...)
				tags = append(tags, metricsTags...)
				return metricsClient.EmitCounter(psm+".throughput", 1, tags...)
			}
		}
		err = metricsClient.EmitCounter(psm+".throughput", 1, metricsTagError...)
	} else if logLevel == 6 { // fatal
		if ctx != nil {
			metricsTags := getMetricTags(ctx)
			if len(metricsTags) != 0 {
				tags := make([]metrics.T, 0, len(metricsTagFatal)+len(metricsTags))
				tags = append(tags, metricsTagFatal...)
				tags = append(tags, metricsTags...)
				return metricsClient.EmitCounter(psm+".throughput", 1, tags...)
			}
		}
		err = metricsClient.EmitCounter(psm+".throughput", 1, metricsTagFatal...)
	}
	return err
}
