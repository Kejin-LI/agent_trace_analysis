package metrics

import (
	"fmt"
	"io"
	"time"

	"code.byted.org/aiops/metrics_codec"
	"code.byted.org/gopkg/apm_vendor_interface"
	"code.byted.org/gopkg/metrics_core/logs"
	"code.byted.org/gopkg/metrics_core/utils"
)

const (
	defaultHistogramFieldName   = "histogram"
	defaultSummaryCacheCapacity = 2048
)

type CodecVersion int8

const (
	CodecV3 CodecVersion = iota
	CodecV4
)

// ClientOption provides a set of client options,
// options can be declared in client initialization.
type ClientOption func(*client)

// SetGlobalTags allows users to set their global static tags,
// it would panic if tag key or value is invalid.
func SetGlobalTags(tags ...T) ClientOption {
	return func(c *client) {
		for _, tag := range tags {
			c.globalTags = append(c.globalTags, metrics_codec.Tag{Key: tag.Name, Value: tag.Value})
			if !c.foundDCInGlobalTags && tag.Name == "dc" {
				c.foundDCInGlobalTags = true
			}

			if !c.foundHostInGlobalTags && tag.Name == "host" {
				c.foundHostInGlobalTags = true
			}
		}
	}
}

// SetWriter allows users to set their custom writer,
// writer should be synchronous,
// the default writer is metrics agent writer.
func SetWriter(w io.WriteCloser) ClientOption {
	return func(c *client) {
		c.sender = c.newSender(w)
		if e, ok := w.(apm_vendor_interface.MetricsExporter); ok {
			if e.GetBatchSize() > 0 {
				c.messageSizeLimit = e.GetBatchSize()
			}
		}
	}
}

// Collector is a collector factory to get a collector instance.
// Metrics client store cached metrics into collector for a while.
type Collector func(int, *metric, int, func([]string) error) collector

var (
	// KDTree a collector which use k-d tree to store metrics.
	KDTree Collector = newShardingKDTree
)

// SetCollector allows users to set which collector be used,
// default is k-d tree collector.
func SetCollector(c Collector) ClientOption {
	return func(client *client) {
		client.collectorFactory = c
	}
}

// Deprecated: SetHistogramTimer sets timer use histogram to pre-aggregate timer instead.
// It doesn't work in metrics 2.0
func SetHistogramTimer() ClientOption {
	return func(c *client) {
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

// Deprecated: it is not worked in metrics 2.0.
// SetTimerBufSize sets the timer buffer if the timer does not use histogram mode,
// set it to zero may reduce the overhead of timer send lock.
func SetTimerBufSize(n int) ClientOption {
	return func(c *client) {
		return
	}
}

// SetTenant sets the tenant in metrics 2.0, use "default" tenant as default.
func SetTenant(tenant string) ClientOption {
	return func(c *client) {
		c.tenant = tenant
	}
}

// SetTimeInterval sets the client sending interval time by specified second,
// the default interval is 30 seconds.
func SetTimeInterval(sec int) ClientOption {
	return func(c *client) {
		if !utils.Contains(candidateTimeIntervals, sec) {
			logs.Error("invalid interval %ds, reset to default 30s, supported intervals are %v.", sec, candidateTimeIntervals)
			return
		}
		c.timeIntervalSec = sec
	}
}

// SetOverwriteVendorTags allows users to overwrite vendor tags with client's global tags.
// Deprecated: create custom VendorTagsProvider to overwrite the default vendor tags
func SetOverwriteVendorTags() ClientOption {
	return func(c *client) {
	}
}

// SetTenantActive force set the client's isTenantActive to true.
func SetTenantActive() ClientOption {
	return func(c *client) {
		c.updateTenantStatus(true)
	}
}

// SetEnableTagCache enables the client to cache the tag bytes.
// It can speed up packing message but occupy more memory in the meantime.
func SetEnableTagCache() ClientOption {
	return func(c *client) {
		c.cacheTags = true
	}
}

// SetBatchSize sets the batch size of each message.
// Once the message reaches the limit, the client will flush the data.
func SetBatchSize(batchSizeInBytes int) ClientOption {
	return func(c *client) {
		if batchSizeInBytes > 0 {
			c.messageSizeLimit = batchSizeInBytes
		}
	}
}

// MetricOption is the option type to specify the Metric behavior.
type MetricOption func(*metric) error

// SetCompactTimer uses multi-field timer rather than 1.0 as before,
// it would break the query compatibility,
// but increase the query performance.
func SetCompactTimer() MetricOption {
	return func(m *metric) error {
		m.timerOps.compatTimer = false
		return nil
	}
}

// SetMultiFieldTimer is the alias of SetCompactTimer.
var SetMultiFieldTimer = SetCompactTimer

// SetHistogramBucket set the bucket for a default histogram metric
func SetHistogramBucket(buckets []float64, suffix ...string) MetricOption {
	name := defaultHistogramFieldName
	if len(suffix) > 0 {
		name = suffix[0]
	}
	return appendFields([]Field{{Name: name, Type: HistogramType, Buckets: buckets}}, false)
}

// LinearBuckets creates 'count' buckets, each 'width' wide, where the lowest
// bucket has an upper bound of 'start'. The final +Inf bucket is not counted
// and not included in the returned slice. The returned slice is meant to be
// used for the Buckets field of HistogramOpts.
func LinearBuckets(start, width float64, count int) []float64 {
	if count < 1 {
		logs.Error("LinearBuckets needs a positive count, start: %v, width: %v, count: %v", start, width, count)
		return nil
	}
	buckets := make([]float64, count)
	for i := range buckets {
		buckets[i] = start
		start += width
	}
	return buckets
}

// ExponentialBuckets creates 'count' buckets, where the lowest bucket has an
// upper bound of 'start' and each following bucket's upper bound is 'factor'
// times the previous bucket's upper bound. The final +Inf bucket is not counted
// and not included in the returned slice. The returned slice is meant to be
// used for the Buckets field of HistogramOpts.
func ExponentialBuckets(start, factor float64, count int) []float64 {
	if count < 1 {
		logs.Error("ExponentialBuckets needs a positive count, start: %v, factor: %v, count: %v", start, factor, count)
		return nil
	}
	if start <= 0 {
		logs.Error("ExponentialBuckets needs a positive start value, start: %v, factor: %v, count: %v", start, factor, count)
		return nil
	}
	if factor <= 1 {
		logs.Error("ExponentialBuckets needs a factor greater than 1, start: %v, factor: %v, count: %v", start, factor, count)
		return nil
	}
	buckets := make([]float64, count)
	for i := range buckets {
		buckets[i] = start
		start *= factor
	}
	return buckets
}

// SetMultiFields sets the customized multi fields.
// Users must provide the fields if they want to use customized multi-field metrics.
// Note that when multi-fields is enabled, the multi-field timer is also enabled
func SetMultiFields(fields []Field) MetricOption {
	return setMultiFields(fields, true, false)
}

func setMultiFields(fields []Field, force, compatTimer bool) MetricOption {
	return func(m *metric) error {
		m.fields.fields = fields
		m.forceMultiFields = force
		m.timerOps.compatTimer = compatTimer
		return nil
	}
}

func appendFields(fields []Field, force bool) MetricOption {
	return func(m *metric) error {
		m.fields.fields = append(m.fields.fields, fields...)
		if !m.forceMultiFields {
			m.forceMultiFields = force
		}
		return nil
	}
}

// SetCKMSTimer uses CKMS approximate algorithm to aggregate timer data points.
func SetCKMSTimer() MetricOption {
	return func(m *metric) error {
		m.timerOps.maxAge = DefMaxAge
		m.timerOps.ageBuckets = DefAgeBuckets
		m.timerOps.bufCap = DefBufCap
		m.timerOps.objectives = defaultObjectives
		m.timerOps.useCKMS = true
		return nil
	}
}

// SetCKMSTimerConfig creates a customized configuration for timer metrics with CKMS algorithm.
// Users can set whether to use multi-field feature, the objectives, the max age, age bucket and bufCap.
func SetCKMSTimerConfig(useMultiFieldTimer bool, objectives map[float64]float64,
	maxAge time.Duration, ageBuckets int, bufCap int,
) MetricOption {

	return func(m *metric) error {
		if maxAge <= 0 {
			return fmt.Errorf("invalid maxAge in ckms summary option %v", maxAge)
		}

		if ageBuckets <= 0 {
			return fmt.Errorf("invalid ageBuckets in ckms summary option %v", ageBuckets)
		}

		if bufCap <= 0 {
			return fmt.Errorf("invalid bufCap in ckms summary option %v", bufCap)
		}

		if len(objectives) != 5 {
			return fmt.Errorf("invalid objectives in ckms summary option %v", objectives)
		}

		objectiveKeys := []float64{0.5, 0.9, 0.95, 0.99, 0.999}
		for k, v := range objectives {
			if !utils.FloatContains(objectiveKeys, k) || v <= 0 || v >= 1 {
				return fmt.Errorf("invalid objectives in ckms summary option %v", objectives)
			}
		}

		m.timerOps.compatTimer = !useMultiFieldTimer
		m.timerOps.objectives = objectives
		m.timerOps.maxAge = maxAge
		m.timerOps.ageBuckets = uint32(ageBuckets)
		m.timerOps.bufCap = uint32(bufCap)
		m.timerOps.useCKMS = true
		return nil
	}
}

type timerOption struct {
	compatTimer bool // whether to convert timer to 10 single-field metrics, metrics 2.0 supports multi-fields
	useCKMS     bool // whether to use CKMS approximate algorithm to compress the data like Prometheus

	// The following configurations works only if CKMS is enabled
	objectives    map[float64]float64
	maxAge        time.Duration
	ageBuckets    uint32
	bufCap        uint32
	cacheCapacity int
}

var (
	defaultTimerOps = timerOption{
		compatTimer:   true,
		useCKMS:       false,
		cacheCapacity: defaultSummaryCacheCapacity,
	}

	defaultObjectives = map[float64]float64{
		0.5:   DefEpsilon,
		0.9:   DefEpsilon,
		0.95:  DefEpsilon,
		0.99:  DefEpsilon,
		0.999: DefEpsilon,
	}
)

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

// SetAllowDuplicateMetricNames sets if allow duplicate metric names exist in the same Client,
// default value is false.
func SetAllowDuplicateMetricNames() ClientOption {
	return func(c *client) {
		c.allowDuplicateMetrics = true
	}
}

// SetCustomVendorTagsProvider set custom VendorTagsProvider which will inject tags as globalTags
// into Client
func SetCustomVendorTagsProvider(provider apm_vendor_interface.VendorTagsProvider) ClientOption {
	return func(c *client) {
		c.vendorTagsProvider = provider
	}
}

// SetDiscardInvalidTag changes client's behavior when it receives invalid tag values.
// It will replace invalid tag values with a default string "-".
func SetDiscardInvalidTag() ClientOption {
	return func(c *client) {
		c.discardInvalidTag = true
	}
}

// SetReportInitialCounter enables the client to send the initial counter value for each counter series.
func SetReportInitialCounter() ClientOption {
	return func(c *client) {
		c.reportInitialValue = true
	}
}

// Deprecated:
// use SetReportUnchangedCounter instead
// SetIgnoreUnchangedCounter changes client's behavior when counter series keeps unchanged.
// It stops reporting the unchanged counter after 3 intervals.
func SetIgnoreUnchangedCounter() ClientOption {
	return func(c *client) {
		c.ignoreUnchangedCounter = true
	}
}

// SetReportUnchangedCounter changes client's behavior when counter series keeps unchanged.
// If it is enabled, it will continuously report counter series until it is expired.
// Otherwise, it stops reporting the unchanged counter after 3 intervals.
func SetReportUnchangedCounter(enable bool) ClientOption {
	return func(c *client) {
		c.ignoreUnchangedCounter = !enable
	}
}

// SetDisableDeepCopyTags changes client's behavior on how to store tag values.
// The client by default will deep-copy the tag values to avoid race or long-hold problems when the tag-value is backed with reusable or huge bytes.
// Call this method to disable the deep-copy behavior, if you confirm that you didn't meet any of the scenes above.
func SetDisableDeepCopyTags() ClientOption {
	return func(c *client) {
		c.deepCopyTagIndex = false
	}
}

// SetCodec sets th codec for encoding and decoding.
func SetCodec(codec CodecVersion) ClientOption {
	return func(c *client) {
		c.withCodecV4 = codec == CodecV4
	}
}

type ExporterFactory func(Client) apm_vendor_interface.MetricsExporter

func SetExporterFactory(factory ExporterFactory) ClientOption {
	return func(c *client) {
		c.exporterFactory = factory
	}
}

// SetTimerCapacity sets the cache capacity of each series of timer metrics.
// To use unlimited cache, use 0 as the capacity.
// SDK will discard data points if cache is full.
func SetTimerCapacity(n int) MetricOption {
	return func(m *metric) error {
		m.timerOps.cacheCapacity = n
		return nil
	}
}

// SetPackerParallelism sets the parallelism level for packers in the client, which is 1 by default.
func SetPackerParallelism(parallelism int) ClientOption {
	return func(c *client) {
		if parallelism > 0 {
			c.packerParallelism = parallelism
		}
	}
}
