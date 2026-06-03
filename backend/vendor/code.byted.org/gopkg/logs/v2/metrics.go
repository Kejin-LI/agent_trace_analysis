package logs

import (
	osTime "time"

	"code.byted.org/gopkg/env"
	"code.byted.org/gopkg/metrics/v3"
)

var (
	metric metrics.Metric
	warnEmitter metrics.Emitter
	errEmitter   metrics.Emitter
	fatalEmitter metrics.Emitter
	clientReport metrics.Emitter
)

func init() {
	metricsClient := metrics.NewClient(
		"toutiao.service.log",
		metrics.SetTceTags(),
		metrics.SetGlobalTags(
			metrics.T{Name: "cluster", Value: env.Cluster()},
			metrics.T{Name: "env", Value: env.Env()},
			metrics.T{Name: "version", Value: "v2.0.0"},
			),
	)
	metric = metricsClient.NewMetric(env.PSM() + ".throughput", "level")
	warnEmitter = metric.WithTagValues("WARNING")
	errEmitter = metric.WithTagValues("ERROR")
	fatalEmitter = metric.WithTagValues("CRITICAL")

	clientReport = metricsClient.NewMetric("client.liveness").WithTags()

	go func() {
		for {
			_ = clientReport.Emit1(metrics.Store(1))
			osTime.Sleep(15 * osTime.Second)
		}
	}()
}

func metricsMiddleware(log RewritableLog) RewritableLog {
	switch log.GetLevel() {
	case "Warn":
		_ = warnEmitter.Emit1(metrics.WithSuffix("").IncrCounter(1))
	case "Error":
		_ = errEmitter.Emit1(metrics.WithSuffix("").IncrCounter(1))
	case "Fatal":
		_ = fatalEmitter.Emit1(metrics.WithSuffix("").IncrCounter(1))
	default:
		return log
	}
	return log
}
