package metrics

import (
	"sync/atomic"

	"code.byted.org/gopkg/metrics_core/logs"
)

const (
	versionMetric       = "inf.apm.ingress.version"
	senderBytesMetric   = "inf.apm.ingress.sender.bytes"
	senderItemsMetric   = "inf.apm.ingress.sender.items"
	senderFailMetric    = "inf.apm.ingress.sender.fail"
	senderLatencyMetric = "inf.apm.ingress.sender.latency"
	emitMetric          = "inf.apm.ingress.emit.metric"
	cacheSeriesMetric   = "inf.apm.ingress.cache.series"
	cacheMemMetric      = "inf.apm.ingress.cache.mem"
)

var (
	internalTenant           = "inf.metrics"
	selfMonitorFlushInterval = 30
)

type metricStatus struct {
	client *client
	metric *metric

	cacheSeries *addition
	cacheMem    *addition
}

func (m *metric) newMetricStatus() *metricStatus {
	s := &metricStatus{
		client: m.client,
		metric: m,

		cacheSeries: newAddition(cacheSeriesMetric, CounterType, false),
		cacheMem:    newAddition(cacheMemMetric, CounterType, false),
	}

	return s
}

func (s *metricStatus) addCacheSeries(n int64) {
	atomic.AddInt64(&s.cacheSeries.value, n)
}

func (s *metricStatus) addCacheMem(n int64) {
	atomic.AddInt64(&s.cacheMem.value, n)
}

func emitAndLog(emitter Emitter, values ...*Value) {
	if emitter == nil {
		logs.Warn("nil emitter")
		return
	}
	err := emitter.Emit(values...)
	if err != nil {
		logs.Warn("failed to emit monitor metric: %v", err)
	}
}
