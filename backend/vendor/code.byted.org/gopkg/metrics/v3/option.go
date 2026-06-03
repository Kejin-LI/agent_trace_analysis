package metrics

import (
	"fmt"
	"io"
	"time"

	"code.byted.org/aiops/apm_vendor_byted/vendor_tags"

	"code.byted.org/gopkg/env"
)

// ClientOption provides a set of client options,
// options can be declared in client initialization.
type ClientOption func(*client)

// SetGlobalTags allows users to set their global static tags,
// it would panic if tag key or value is invalid.
func SetGlobalTags(tags ...T) ClientOption {
	return func(c *client) {
		for _, t := range tags {
			if !isValidString(t.Value) || !isValidString(t.Name) {
				panic(fmt.Sprintf("global tag %s:%s is not valid", t.Name, t.Value))
			}
		}
		c.globalTags = appendTags(c.globalTags, tags)
	}
}

// SetTceTags sets default tce tags be read from env: env_type, pod_name, _psm, deploy_stage, host_v6 and cluster.
func SetTceTags() ClientOption {
	tags := make([]T, 0)
	if env.InTCE() {
		tags = append(tags, T{"env_type", "tce"})
	}
	tags = append(tags, T{"pod_name", env.PodName()})
	tags = append(tags, T{"_psm", env.PSM()})
	tags = append(tags, T{"deploy_stage", env.Stage()})

	tags = append(tags, T{"_pod_ip", vendor_tags.GetPodIP()})
	tags = append(tags, T{"dc", vendor_tags.GetDC()})
	tags = append(tags, T{"host", vendor_tags.GetHost()})

	if env.HasIPV6() {
		tags = append(tags, T{"host_v6", env.HostIPV6()})
	}
	if env.Cluster() != "" {
		tags = append(tags, T{"cluster", env.Cluster()})
	}
	return SetGlobalTags(tags...)
}

// SetWriter allows users to set their custom writer,
// writer should be synchronous,
// the default writer is metrics agent writer.
func SetWriter(w io.WriteCloser) ClientOption {
	return func(c *client) {
		c.sender = newSender(w)
	}
}

// Collector is a collector factory to get a collector instance.
// Metrics client store cached metrics into collector for a while.
type Collector func(int, *metric, int, func([]string) error) collector

var (
	// Map is a collector which use sync.Map to store metrics,
	// it is an experimental feature, please do not use it in production.
	Map Collector = newMap
	// KDTree a collector which use k-d tree to store metrics.
	KDTree Collector = newShardingCollector
)

// SetCollector allows users to set which collector be used,
// default is k-d tree collector.
func SetCollector(c Collector) ClientOption {
	return func(client *client) {
		client.collectorFactory = c
	}
}

// SetHistogramTimer sets timer use histogram to pre-aggregate timer instead.
func SetHistogramTimer() ClientOption {
	return func(c *client) {
		c.histogramTimer = true
	}
}

// SetHighestWaterMark sets the highest watermark of each Metric instance,
// emitting would fail if the series count of the Metric reaches the highest watermark
// and returns error.
func SetHighestWaterMark(w int) ClientOption {
	return func(c *client) {
		c.forceDrop = w
	}
}

// SetTimerBufSize sets the timer buffer if the timer does not use histogram mode,
// set it to zero may reduce the overhead of timer send lock.
func SetTimerBufSize(n int) ClientOption {
	return func(c *client) {
		if n > 0 {
			c.timerBuf = newRingBuffer(uint64(n))
		}
	}
}

// SetMetricsExpireDuration set the time duration before the metrics got garbage-collected,
// memory used by metrics will be recycled if the metric not emit data-point for this period of time.
func SetMetricsExpireDuration(d time.Duration) ClientOption {
	return func(c *client) {
		c.metricExpireDuration = d
	}
}

// SetMetricsGCStrategy set the metrics gc strategy,
// gcInterval set the gc check interval in percentage of the MetricsExpireDuration, range [0.1~1], default to 0.25,
// gcThreshold set the min percentage of expired metric series before the gc really happen, range [0.1~1], default to 0.3
func SetMetricsGCStrategy(gcInterval float64, gcThreshold float64) ClientOption {
	if gcInterval < 0.1 {
		gcInterval = 0.1
	} else if gcInterval > 1 {
		gcInterval = 1
	}
	if gcThreshold < 0.1 {
		gcThreshold = 0.1
	} else if gcThreshold > 1 {
		gcThreshold = 1
	}
	return func(c *client) {
		c.metricGCInterval = gcInterval
		c.metricGCThreshold = gcThreshold
	}
}
