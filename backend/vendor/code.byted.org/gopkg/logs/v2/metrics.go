package logs

import (
	"log"
	"sync"
	osTime "time"

	"code.byted.org/aiops/apm_vendor_byted/vendor_tags"
	"code.byted.org/gopkg/env"
	"code.byted.org/gopkg/metrics/v3"
)

var (
	emitterMap            = sync.Map{}
	defaultMetricEmitters *metricMiddlewareEmitters
)

func init() {
	defaultMetricEmitters = newMetricMiddlewareEmitters(env.PSM())
	go defaultMetricEmitters.reportLiveness()
}

// metricsMiddleware is the default metrics middleware.
// It reports metrics with name "toutiao.service.log.{env.PSM()}.throughput".
func metricsMiddleware(log RewritableLog) RewritableLog {
	defaultMetricEmitters.emitErrorLog(log.GetLevel())
	return log
}

// getErrorLogMiddleware gets the error log metric middleware based on the logger's psm.
func getErrorLogMiddleware(psm string, ops ...metrics.ClientOption) Middleware {
	switch psm {
	case env.PSM():
		return metricsMiddleware
	default:
		emittersV, loaded := emitterMap.Load(psm)
		if !loaded {
			emittersV = newMetricMiddlewareEmitters(psm, ops...)
			emitterMap.Store(psm, emittersV)
		}

		emitters, ok := emittersV.(*metricMiddlewareEmitters)
		if !ok {
			log.Printf("failed to get the error log metrics emitters for psm %s", psm)
			return func(log RewritableLog) RewritableLog { return log }
		}

		return func(log RewritableLog) RewritableLog {
			emitters.emitErrorLog(log.GetLevel())
			return log
		}
	}
}

type metricMiddlewareEmitters struct {
	client metrics.Client
	metric metrics.Metric

	warnEmitter  metrics.Emitter
	errEmitter   metrics.Emitter
	fatalEmitter metrics.Emitter
}

func newMetricMiddlewareEmitters(psm string, ops ...metrics.ClientOption) *metricMiddlewareEmitters {
	tags := make([]metrics.T, 0)
	tags = append(tags, metrics.T{Name: "_psm", Value: psm})
	if env.InTCE() {
		tags = append(tags, metrics.T{Name: "env_type", Value: "tce"})
	}

	if env.HasIPV6() {
		tags = append(tags, metrics.T{Name: "host_v6", Value: env.HostIPV6()})
	}

	if env.Cluster() != "" {
		tags = append(tags, metrics.T{Name: "cluster", Value: env.Cluster()})
	}

	tags = append(tags,
		metrics.T{Name: "pod_name", Value: env.PodName()},
		metrics.T{Name: "deploy_stage", Value: env.Stage()},
		metrics.T{Name: "_pod_ip", Value: vendor_tags.GetPodIP()},
		metrics.T{Name: "env", Value: env.Env()},
		metrics.T{Name: "version", Value: "v2.0.0"},
		metrics.T{Name: "dc", Value: vendor_tags.GetDC()},
		metrics.T{Name: "host", Value: vendor_tags.GetHost()},
	)

	// add extra _primary_psm tags
	tags = append(tags, metrics.T{Name: "_primary_psm", Value: vendor_tags.GetPrimaryPSM()})

	ops = append(ops,
		metrics.SetGlobalTags(tags...),
		metrics.SetMetricsExpireDuration(0), // Emitters will not expire.
	)

	metricsClient := metrics.NewClient(
		"toutiao.service.log",
		ops...,
	)

	metric := metricsClient.NewMetric(psm+".throughput", "level")
	warnEmitter := metric.WithTagValues("WARNING")
	errEmitter := metric.WithTagValues("ERROR")
	fatalEmitter := metric.WithTagValues("CRITICAL")

	return &metricMiddlewareEmitters{
		client:       metricsClient,
		metric:       metric,
		warnEmitter:  warnEmitter,
		errEmitter:   errEmitter,
		fatalEmitter: fatalEmitter,
	}
}

func (emiters *metricMiddlewareEmitters) emitErrorLog(Level string) {
	switch Level {
	case "Warn":
		_ = emiters.warnEmitter.Emit1(metrics.WithSuffix("").IncrCounter(1))
	case "Error":
		_ = emiters.errEmitter.Emit1(metrics.WithSuffix("").IncrCounter(1))
	case "Fatal":
		_ = emiters.fatalEmitter.Emit1(metrics.WithSuffix("").IncrCounter(1))
	default:
		return
	}
}

func (emitter *metricMiddlewareEmitters) reportLiveness() {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("failed to do metrics gc, err: %s", err)
		}
	}()

	clientReport := emitter.client.NewMetric("client.liveness").WithTags()

	for {
		_ = clientReport.Emit1(metrics.Store(1))
		osTime.Sleep(15 * osTime.Second)
	}
}
