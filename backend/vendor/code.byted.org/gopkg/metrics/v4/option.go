package metrics

import (
	"io"
	"time"

	"code.byted.org/aiops/apm_vendor_byted/vendor_tags"
	"code.byted.org/gopkg/apm_vendor_interface"
	"code.byted.org/gopkg/env"
	core "code.byted.org/gopkg/metrics_core"
)

type CodecVersion = core.CodecVersion

const (
	CodecV3 CodecVersion = core.CodecV3
	CodecV4 CodecVersion = core.CodecV4
)

var DefaultBuckets = []float64{
	.005, .01, .025, .05, .075, .1, .25, .5, .75, 1, 2.5, 5, 7.5, 10,
}
var SyncMapCollectorFactory Collector = core.SyncMapCollectorFactory
var ShardingKdTreeCollectorFactory Collector = core.ShardingKdTreeCollectorFactory
var ShardingSyncMapCollectorFactory Collector = core.ShardingSyncMapCollectorFactory

// ClientOption provides a set of client options,
// options can be declared in client initialization.
type ClientOption = core.ClientOption

// SetGlobalTags allows users to set their global static tags,
// it would panic if tag key or value is invalid.
func SetGlobalTags(tags ...T) ClientOption {
	return core.SetGlobalTags(tags...)
}

// SetTceTags sets default tce tags be read from env: env_type, pod_name, _psm, deploy_stage, host_v6 and cluster.
func SetTceTags() ClientOption {
	tags := make([]T, 0)
	if env.InTCE() {
		tags = append(tags, T{Name: "env_type", Value: "tce"})
	}
	tags = append(tags, T{Name: "pod_name", Value: env.PodName()})
	tags = append(tags, T{Name: "_psm", Value: env.PSM()})
	tags = append(tags, T{Name: "deploy_stage", Value: env.Stage()})
	if env.HasIPV6() {
		tags = append(tags, T{Name: "host_v6", Value: env.HostIPV6()})
	}
	if env.Cluster() != "" {
		tags = append(tags, T{Name: "cluster", Value: env.Cluster()})
	}
	return SetGlobalTags(tags...)
}

// SetWriter allows users to set their custom writer,
// writer should be synchronous,
// the default writer is metrics agent writer.
func SetWriter(w io.WriteCloser) ClientOption {
	return core.SetWriter(w)
}

// Collector is a collector factory to get a collector instance.
// Metrics client store cached metrics into collector for a while.
type Collector = core.Collector

// SetCollector allows users to set which collector be used,
// default is k-d tree collector.
func SetCollector(c Collector) ClientOption {
	return core.SetCollector(c)
}

// Deprecated: SetHistogramTimer sets timer use histogram to pre-aggregate timer instead.
// It doesn't work in metrics 2.0
func SetHistogramTimer() ClientOption {
	return core.SetHistogramTimer()
}

// SetHighestWaterMark sets the highest watermark of each Metric instance,
// emitting would fail if the series count of the Metric reaches the highest watermark
// and returns error.
func SetHighestWaterMark(w int) ClientOption {
	return core.SetHighestWaterMark(w)
}

// Deprecated: it is not worked in metrics 2.0.
// SetTimerBufSize sets the timer buffer if the timer does not use histogram mode,
// set it to zero may reduce the overhead of timer send lock.
func SetTimerBufSize(n int) ClientOption {
	return core.SetTimerBufSize(n)
}

// SetTenant sets the tenant in metrics 2.0, use "default" tenant as default.
func SetTenant(tenant string) ClientOption {
	return core.SetTenant(tenant)
}

// SetTimeInterval sets the client sending interval time by specified second,
// the default interval is 30 seconds.
func SetTimeInterval(sec int) ClientOption {
	return core.SetTimeInterval(sec)
}

// SetXFLTags sets the necessary global tags for XFL environment.
func SetXFLTags() ClientOption {
	tags := make([]T, 0, 3)
	tags = append(tags, T{"_pod_ip", vendor_tags.GetPodIP()})
	tags = append(tags, T{"dc", env.IDC()})
	tags = append(tags, T{"host", vendor_tags.GetHost()})
	return SetGlobalTags(tags...)
}

// SetOverwriteVendorTags .
// Deprecated: create custom VendorTagsProvider to overwrite the default vendor tags
func SetOverwriteVendorTags() ClientOption {
	return core.SetOverwriteVendorTags()
}

// MetricOption is the option type to specify the Metric behavior.
type MetricOption = core.MetricOption

// SetCompactTimer uses multi-field timer rather than 1.0 as before,
// it would break the query compatibility,
// but increase the query performance.
func SetCompactTimer() MetricOption {
	return core.SetCompactTimer()
}

// SetMultiFieldTimer is the alias of SetCompactTimer.
var SetMultiFieldTimer = SetCompactTimer

// SetHistogramBucket set the bucket for a default histogram metric
func SetHistogramBucket(buckets []float64, suffix ...string) MetricOption {
	return core.SetHistogramBucket(buckets, suffix...)
}

// LinearBuckets creates 'count' buckets, each 'width' wide, where the lowest
// bucket has an upper bound of 'start'. The final +Inf bucket is not counted
// and not included in the returned slice. The returned slice is meant to be
// used for the Buckets field of HistogramOpts.
//
// The function panics if 'count' is zero or negative.
func LinearBuckets(start, width float64, count int) []float64 {
	return core.LinearBuckets(start, width, count)
}

// ExponentialBuckets creates 'count' buckets, where the lowest bucket has an
// upper bound of 'start' and each following bucket's upper bound is 'factor'
// times the previous bucket's upper bound. The final +Inf bucket is not counted
// and not included in the returned slice. The returned slice is meant to be
// used for the Buckets field of HistogramOpts.
//
// The function panics if 'count' is 0 or negative, if 'start' is 0 or negative,
// or if 'factor' is less than or equal 1.
func ExponentialBuckets(start, factor float64, count int) []float64 {
	return core.ExponentialBuckets(start, factor, count)
}

// SetMultiFields sets the customized multi fields.
// Users must provide the fields if they want to use customized multi-field metrics.
// Note that when multi-fields is enabled, the multi-field timer is also enabled
func SetMultiFields(fields []Field) MetricOption {
	return core.SetMultiFields(fields)
}

// SetCKMSTimer uses CKMS approximate algorithm to aggregate timer data points.
func SetCKMSTimer() MetricOption {
	return core.SetCKMSTimer()
}

// SetCKMSTimerConfig creates a customized configuration for timer metrics with CKMS algorithm.
// Users can set whether to use multi-field feature, the objectives, the max age, age bucket and bufCap.
func SetCKMSTimerConfig(useMultiFieldTimer bool, objectives map[float64]float64,
	maxAge time.Duration, ageBuckets int, bufCap int,
) MetricOption {
	return core.SetCKMSTimerConfig(useMultiFieldTimer, objectives, maxAge, ageBuckets, bufCap)
}

// SetTimerCapacity sets the cache capacity of each series of timer metrics.
// To use unlimited cache, use 0 as the capacity.
// SDK will discard data points if cache is full.
func SetTimerCapacity(n int) MetricOption {
	return core.SetTimerCapacity(n)
}

// SetMetricsExpireDuration set the time duration before the metrics got garbage-collected,
// memory used by metrics will be recycled if the metric not emit data-point for this period of time.
func SetMetricsExpireDuration(d time.Duration) ClientOption {
	return core.SetMetricsExpireDuration(d)
}

// SetMetricsGCStrategy set the metrics gc strategy,
// gcInterval set the gc check interval in percentage of the MetricsExpireDuration, range [0.1~1], default to 0.25,
// gcThreshold set the min percentage of expired metric series before the gc really happen, range [0.1~1], default to 0.3
func SetMetricsGCStrategy(gcInterval float64, gcThreshold float64) ClientOption {
	return core.SetMetricsGCStrategy(gcInterval, gcThreshold)
}

// SetAllowDuplicateMetricNames sets if allow duplicate metric names exist in the same Client,
// default value is false.
func SetAllowDuplicateMetricNames() ClientOption {
	return core.SetAllowDuplicateMetricNames()
}

// SetEnableTagCache enables the client to cache the tag bytes.
// It can speed up packing message but occupy more memory in the meantime.
func SetEnableTagCache() ClientOption {
	return core.SetEnableTagCache()
}

// SetCustomVendorTagsProvider set custom VendorTagsProvider which will inject tags as globalTags
// into Client
func SetCustomVendorTagsProvider(provider apm_vendor_interface.VendorTagsProvider) ClientOption {
	return core.SetCustomVendorTagsProvider(provider)
}

// SetDiscardInvalidTag changes SDK's behavior when it receives invalid tag values.
// It will replace invalid tag values with a default string "-".
func SetDiscardInvalidTag() ClientOption {
	return core.SetDiscardInvalidTag()
}

// SetReportInitialCounter enables the client to send the initial counter value for each counter series.
func SetReportInitialCounter() ClientOption {
	return core.SetReportInitialCounter()
}

// Deprecated:
// use SetReportUnchangedCounter instead
// SetIgnoreUnchangedCounter changes client's behavior when counter series keeps unchanged.
// It stops reporting the unchanged counter after 3 intervals.
func SetIgnoreUnchangedCounter(isEnabled ...bool) ClientOption {
	return core.SetIgnoreUnchangedCounter()
}

// SetReportUnchangedCounter changes client's behavior when counter series keeps unchanged.
// If it is enabled, it will continuously report counter series until it is expired.
// Otherwise, it stops reporting the unchanged counter after 3 intervals.
func SetReportUnchangedCounter(enable bool) ClientOption {
	return core.SetReportUnchangedCounter(enable)
}

// SetDisableDeepCopyTags changes client's behavior on how to store tag values.
// The client by default will deep-copy the tag values to avoid race or long-hold problems when the tag-value is backed with reusable or huge bytes.
// Call this method to disable the deep-copy behavior, if you confirm that you didn't meet any of the scenes above.
func SetDisableDeepCopyTags() ClientOption {
	return core.SetDisableDeepCopyTags()
}

// SetCodec sets the codec for encoding and decoding.
func SetCodec(codec CodecVersion) ClientOption {
	return core.SetCodec(codec)
}

// SetExporterFactory sets the factory of the metrics exporter
func SetExporterFactory(factory core.ExporterFactory) ClientOption {
	return core.SetExporterFactory(factory)
}

// SetPackerParallelism sets the parallelism level for packers in the client, which is 1 by default.
func SetPackerParallelism(parallelism int) ClientOption {
	return core.SetPackerParallelism(parallelism)
}
