package metrics

import (
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"code.byted.org/aiops/metrics_codec"
	"code.byted.org/gopkg/apm_vendor_interface"
	"code.byted.org/gopkg/metrics_core/logs"
	"code.byted.org/gopkg/metrics_core/utils"
	"code.byted.org/gopkg/metrics_core/writers"
)

const (
	keyPrimaryPSM                    = "_primary_psm"
	minGCInterval                    = 5 * time.Minute
	defaultExpireDuration            = 24 * time.Hour
	defaultGCInterval                = 0.25
	defaultGCThreshold               = 0.3
	bucketMaxCount                   = 100
	activeWindowsForUnchangedCounter = 3
	defaultBufCap                    = 10
)

const (
	StatusStop = iota
	StatusRunning
)

const (
	// tenantInitializing indicates that the client is created, but the SDK doesn't know whether it is valid.
	// The SDK allows users to emit series in this stage.
	tenantInitializing = iota

	// tenantInactive indicates that this tenant is invalid, and the SDK won't send data for this client.
	// The SDK doesn't allow users to emit series.
	tenantInactive

	// tenantActive indicates that this tenant is valid, and the SDK starts sending data for this client.
	// The SDK allows users to emit series.
	tenantActive
)

var (
	candidateTimeIntervals = []int{1, 5, 15, 30, 60, 300, 600, 900}
)

// for debug purpose
var gcCallback func(source interface{}, arg interface{})

// T is a tag struct.
type T struct {
	Name  string
	Value string
}

// TagIndex is a cached string slice,
// it was generated internally, user should use NewTagIndex to construct a TagIndex
type TagIndex *[]string

// Client is the next generation metrics client,
// it boosts the performance of metrics in a variety of ways,
// and keep some flexibilities.
type Client interface {
	// NewMetric creates a metric set and defines its tag names,
	// it would return error if tag name is not valid or metric name is invalid.
	NewMetric(metricName string, tagNames ...string) (Metric, error)

	// NewMetricWithOps creates a metric set, defines its tag names, and specifies the metric options.
	// it would return error if tag name is not valid, metric name is invalid, or some option is invalid.
	NewMetricWithOps(name string, tagNames []string, options ...MetricOption) (Metric, error)

	// Close closes a client,
	// all workers would exit and all data would be purged (send and recycle).
	Close()

	// Flush flushes all metrics and emits them at once.
	// Deprecated: Please use Close() instead
	Flush()

	// GetConfigService returns the config service of this client.
	GetConfigService() apm_vendor_interface.MetricsConfigService

	// GetTenant returns the tenant of the client bound to.
	GetTenant() string

	// IsTenantActive returns whether the tenant if active
	IsTenantActive() bool
}

// Metric is the metric instances, it contains a pre-declared tag names cache tree.
type Metric interface {
	// WithTags declares a tag key value series and return an emittable instance.
	WithTags(tag ...T) Emitter

	// WithTagValues declares emitter like WithTags but only needs tag values.
	WithTagValues(tagValues ...string) Emitter

	// NewTagIndex gets a tag index from the tag index pool,
	// and you are able to append tag name/value pairs into it instead of construct a tag slice,
	// it is useful to reduce the gc pressures.
	NewTagIndex() TagIndex

	// AppendTag appends a tag name/value into a tag index,
	// the order of appending tags should be totally equal to the tag keys declaration in NewMetric
	AppendTag(index TagIndex, name, value string)

	// WithTagIndex is a high-performance version of WithTags,
	// it should be used with NewTagIndex together.
	WithTagIndex(index TagIndex) Emitter

	// Close closes metric and emits it.
	Close()

	// Flush flushes the metric and emits it at once.
	Flush()
}

// Emitter provide once emitting with variable methods.
type Emitter interface {
	// Emit emits with methods.
	Emit(values ...*Value) error

	// Emit1 emits at least 1 value.
	Emit1(value *Value, values ...*Value) error

	// Emit2 emits at least 2 values.
	Emit2(v1 *Value, v2 *Value, values ...*Value) error
}

// Measurer provides supported metric methods.
type Measurer interface {
	// Incr increments a value as the rate counter type,
	// It would be simply added together and waiting for sending once.
	Incr(n int) *Value

	// Observe observes a value as the timer type,
	// it would be inserted into a histogram,
	// and compress to max 20 bins before sending to the metric server,
	// in the metric server it would get further more pretreatments (calculating avg, pct99, pct90, etc.).
	Observe(n int) *Value

	// Store stores a raw value as the store type,
	// always send the last stored value in every time window to metric server.
	Store(n int) *Value

	// IncrMeter sends both counter and rate counter type metric.
	IncrMeter(n int) *Value

	// IncrCounter increments a value as a counter type.
	IncrCounter(n int) *Value

	// Stat observes a value as the histogram type.
	// It would be inserted into a histogram storage object.
	// The results contain the counts of values in each bucket and the total count and sum.
	Stat(n int) *Value

	// Add increments an int value as the delta_counter type.
	// It has three fields: delta, rate, and counter.
	Add(n int) *Value

	// Incrf increments a value as the rate counter type,
	// It would be simply added together and waiting for sending once.
	Incrf(n float64) *Value

	// Observef observes a value as the timer type,
	// it would be inserted into a histogram,
	// and compress to max 20 bins before sending to the metric server,
	// in the metric server it would get further more pretreatments (calculating avg, pct99, pct90, etc.).
	Observef(n float64) *Value

	// Storef stores a raw value as the store type,
	// always send the last stored value in every time window to metric server.
	Storef(n float64) *Value

	// IncrMeterf sends both counter and rate counter type metric.
	IncrMeterf(n float64) *Value

	// IncrCounterf increments a value as a counter type.
	IncrCounterf(n float64) *Value

	// Statf observes a value as the histogram type.
	// It would be inserted into a histogram storage object.
	// The results contain the counts of values in each bucket and the total count and sum.
	Statf(n float64) *Value

	// Addf increments a float value as the delta_counter type.
	// It has three fields: delta, rate, and counter.
	Addf(n float64) *Value
}

type client struct {
	sdk                  *sdk
	senderStatus         int32
	prefix               string
	metrics              map[string][]*metric
	sender               *sender
	globalTags           []metrics_codec.Tag
	close                chan bool
	workers              sync.WaitGroup
	collectorFactory     Collector
	forceDrop            int
	metricExpireDuration time.Duration
	metricGCInterval     float64
	metricGCThreshold    float64
	messageSizeLimit     int
	exporterFactory      ExporterFactory

	sync.Mutex
	// message properties, e.g., tenant, project, vesrion, etc.
	messageProperties []string
	packers           []*packer
	packerIdx         int64

	foundDCInGlobalTags   bool
	foundHostInGlobalTags bool
	isMonitorClient       bool

	// properties users can set with ClientOptions
	withCodecV4            bool
	tenant                 string
	timeIntervalSec        int
	tenantStatus           int32
	vendorTagsProvider     apm_vendor_interface.VendorTagsProvider
	allowDuplicateMetrics  bool
	cacheTags              bool
	discardInvalidTag      bool
	reportInitialValue     bool
	ignoreUnchangedCounter bool
	deepCopyTagIndex       bool
	packerParallelism      int
}

type cursor struct {
	values *queue
	index  []string
	ops    int64
	prefix string
	metric *metric

	tagBytes []byte
	hashcode int32

	// gc stats
	gcCount int32
}

type metric struct {
	name               string
	matrix             collector
	tags               []string
	sortedUserTagPos   []int
	sortedGlobalTagPos []int
	fnv32aIndices      []int
	d                  int
	client             *client
	indexPool          *sync.Pool
	packetPool         *sync.Pool
	tagPool            *sync.Pool
	fieldPool          *sync.Pool
	status             *metricStatus
	packerID           int

	// for customized multi fields
	fields           fields
	forceMultiFields bool
	flags            int32
	timerOps         timerOption
	closed           bool
}

func NewClient(prefix string, ops ...ClientOption) (Client, error) {
	return GetDefaultSDK().NewClient(prefix, ops...)
}

func (c *client) gc() {
	if c.metricExpireDuration <= 0 {
		return
	}

	gcInterval := time.Duration(math.Max(
		float64(c.metricExpireDuration)*c.metricGCInterval,
		float64(minGCInterval)))
	for {
		select {
		case <-c.close:
			return
		case <-time.After(gcInterval):
			c.Lock()
			cms := make([]*metric, 0, len(c.metrics))
			for _, m := range c.metrics {
				cms = append(cms, m...)
			}
			c.Unlock()
			startTime := time.Now()
			if c.sdk.sdkConfig.DebugMode {
				logs.Debug("start gc at %s", startTime.Format(time.RFC3339Nano))
			}
			for _, m := range cms {
				m.matrix.GC()
			}
			if c.sdk.sdkConfig.DebugMode {
				logs.Debug("end gc, using %s", time.Now().Sub(startTime))
			}
		}
	}
}

func (c *client) GetConfigService() apm_vendor_interface.MetricsConfigService {
	return c.sdk.GetConfigService()
}

// setExporter sets the writer for the client.
// This method is safe since sendMetrics acquires the lock.
func (c *client) setExporter(e apm_vendor_interface.MetricsExporter) error {
	if e == nil {
		return fmt.Errorf("nil_exporter")
	}

	c.Lock()
	defer c.Unlock()
	if c.sender == nil {
		c.sender = c.newSender(nil)
	}

	old := c.sender.w
	if e == old {
		return nil
	}

	if e.GetTenant() != c.GetTenant() {
		return fmt.Errorf("tenant mismatched. client is bound to %s but the exporter is bound to %s",
			c.GetTenant(), e.GetTenant())
	}

	c.sender.w = e
	c.messageSizeLimit = e.GetBatchSize()
	if c.messageSizeLimit <= 0 {
		if c.sdk.sdkConfig.ResourcesLimit.MaxBatchSize > 0 {
			c.messageSizeLimit = c.sdk.sdkConfig.ResourcesLimit.MaxBatchSize
		} else {
			c.messageSizeLimit = defaultMessageSizeLimit
		}
	}

	return nil
}

// send start the send goroutines
func (c *client) send() {
	defer func() {
		atomic.StoreInt32(&c.senderStatus, StatusStop)
		c.workers.Done()
		if c.sdk.sdkConfig.DebugMode {
			logs.Debug("sender of Client: %s of Tenant: %s exited at %v", c.prefix, c.tenant, time.Now())
		}
	}()

	if c.sdk.sdkConfig.DebugMode {
		logs.Debug("sender of Client: %s of Tenant: %s is started at %v", c.prefix, c.tenant, time.Now())
	}

	sendTime, _ := c.getSendTime(time.Now())
	ticker := time.NewTicker(time.Duration(c.timeIntervalSec) * time.Second)
	defer ticker.Stop()
	firstTick := false

	// create a wrapper of the ticker that ticks the first time immediately
	tickerChan := func() <-chan time.Time {
		if !firstTick {
			firstTick = true
			c := make(chan time.Time, 1)
			c <- time.Now()
			return c
		}
		return ticker.C
	}

	for {
		select {
		case <-tickerChan():
			actualTime := time.Now()
			var lastSend bool
			// if send time and actual time is not in the same send window, the sender skipped some intervals
			if !inSameWindow(sendTime, actualTime, c.timeIntervalSec) {
				if actualTime.After(sendTime) {
					logs.Warn("Client: %s of Tenant: %s skipped %d intervals. send time: %v, actual time: %v",
						c.prefix, c.tenant, int(actualTime.Sub(sendTime).Seconds())/c.timeIntervalSec,
						sendTime, actualTime)
				}
				// wait until next send time
				sendTime, lastSend = c.waitForSending()
			}

			c.sendMetrics(sendTime)

			if c.isTenantInvalid() { // This tenant is deleted in config service.
				return
			}
			sendTime = sendTime.Add(time.Duration(c.timeIntervalSec) * time.Second)

			// exit here, otherwise it will send the data again with timestamp = sendTime + 30.
			if lastSend {
				return
			}
		case <-c.close:
			c.sendMetrics(sendTime)
			return
		}
	}

}

func inSameWindow(time1, time2 time.Time, interval int) bool {
	return (time1.Unix() / int64(interval)) == (time2.Unix() / int64(interval))
}

func (c *client) waitForSending() (time.Time, bool) {
	sendTime, shift := c.getSendTime(time.Now())
	shiftTimer := time.NewTimer(shift)
	lastSend := false
	defer shiftTimer.Stop()
	select {
	case <-c.close: // If the client is closed immediately, no need to wait here.
		lastSend = true
	case <-shiftTimer.C:
	}
	return sendTime, lastSend
}

func (c *client) sendMetrics(sendTime time.Time) {
	var start, end time.Time
	start = time.Now()
	c.Lock()

	// If there is only one packer, call packMetrics directly without a goroutine.
	if len(c.packers) == 1 {
		c.packers[0].packMetrics(sendTime)
	} else {
		var wg sync.WaitGroup
		for i := range c.packers {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				c.packers[i].packMetrics(sendTime)
			}(i)
		}
		wg.Wait()
	}

	c.Unlock()
	end = time.Now()
	// send signal to notify connections not enough
	if end.Sub(start) >= time.Duration(float64(c.timeIntervalSec)*float64(time.Second)*0.8) {
		sr, ok := c.sender.w.(writers.WriterSignalReceiver)

		if ok {
			sr.Signal(writers.WriteSigSlow)
		}
	}

	if c.sdk.sdkConfig.SelfMonitor.Enabled && !c.isMonitorClient {
		emitAndLog(c.sdk.senderLatencyMetric.WithTagValues(c.tenant, "tenant"),
			WithSuffix("").Observe(int(end.Sub(start).Microseconds())))
	}

	if c.sdk.sdkConfig.DebugMode {
		logs.Info("Client: %s of Tenant: %s finished sending metrics at %v, duration: %v",
			c.prefix, c.tenant, end, end.Sub(start))
	}
}

func (c *client) removeMetric(m *metric) {
	c.Lock()
	defer c.Unlock()
	sendTime, _ := c.getSendTime(time.Now())
	m.matrix.Send(sendTime)
	ms := c.metrics[m.name]
	for i := 0; i < len(ms); i++ {
		if ms[i] == m {
			ms = append(ms[:i], ms[i+1:]...)
			if len(ms) == 0 {
				delete(c.metrics, m.name)
				return
			}
			c.metrics[m.name] = ms
			return
		}
	}
}

func (c *client) newSender(writer io.WriteCloser) *sender {
	senderP := &sender{
		w:   writer,
		cli: c,
	}
	return senderP
}

func (c *client) getSendTime(now time.Time) (time.Time, time.Duration) {
	mod := now.UnixNano() % (int64(c.timeIntervalSec) * int64(time.Second))
	if mod == 0 {
		return time.Unix(now.Unix(), 0), time.Duration(0)
	} else {
		nextTime := time.Unix(((now.Unix()/int64(c.timeIntervalSec))+1)*int64(c.timeIntervalSec), 0)
		return nextTime, nextTime.Sub(now)
	}
}

func (c *client) NewMetric(metricName string, tagNames ...string) (Metric, error) {
	return c.NewMetricWithOps(metricName, tagNames)
}

func (c *client) NewMetricWithOps(name string, tagNames []string, options ...MetricOption) (Metric, error) {
	m, err := c.newMetric(name, c.prefix, tagNames, options)
	if err != nil {
		return &noopMetric{}, err
	}
	c.Lock()
	defer c.Unlock()
	if ms := c.metrics[m.name]; len(ms) > 0 && !c.allowDuplicateMetrics {
		err = fmt.Errorf("duplicate metric name: %s", m.name)
		return &noopMetric{}, err
	}

	if len(c.metrics) >= c.sdk.sdkConfig.ResourcesLimit.MaxMetricsPerClient {
		err = fmt.Errorf("reached max limit num[%d] of metrics", c.sdk.sdkConfig.ResourcesLimit.MaxMetricsPerClient)
		logs.Info(err.Error())
		return &noopMetric{}, err
	}
	c.metrics[m.name] = append(c.metrics[m.name], m)
	return m, nil
}

func (c *client) Close() {
	if c.IsTenantActive() {
		close(c.close)
		c.workers.Wait() // wait until the sender exited.
		c.sender.close()
	}
	c.sdk.RemoveClient(c)
}

func (c *client) IsTenantActive() bool {
	return atomic.LoadInt32(&c.tenantStatus) == tenantActive
}

// isTenantInvalid means that this tenant is checked and marked as invalid.
func (c *client) isTenantInvalid() bool {
	return atomic.LoadInt32(&c.tenantStatus) == tenantInactive
}

func (c *client) updateTenantStatus(active bool) {
	if active {
		atomic.StoreInt32(&c.tenantStatus, tenantActive)
		return
	}
	atomic.StoreInt32(&c.tenantStatus, tenantInactive)
}

// checkConfig will do the following checks.
// 1. deduplicate global tags
// 2. sort the global tags
func (c *client) checkConfig() {
	c.deduplicateTags()

	c.sortGlobalTags()
}

func (c *client) deduplicateTags() {
	var immutableTags, rewritableTags map[string]string
	if c.vendorTagsProvider != nil {
		immutableTags = c.vendorTagsProvider.GetImmutableTags()
		rewritableTags = c.vendorTagsProvider.GetRewritableTags()
	}
	var seenTags = make(map[string]string, len(c.globalTags)+len(immutableTags)+len(rewritableTags))

	for k, v := range immutableTags {
		seenTags[k] = v
	}
	for _, tag := range c.globalTags {
		if _, ok := seenTags[tag.Key]; !ok {
			seenTags[tag.Key] = tag.Value
			continue
		}
		logs.Warn("Client: %s of Tenant: %s found a user-defined global tag in conflict with the vendor tags."+
			" It will be overwritten by key: %s, value: %s", c.prefix, c.tenant, tag.Key, seenTags[tag.Key])
		continue
	}
	for k, v := range rewritableTags {
		if _, ok := seenTags[k]; ok {
			continue
		}
		seenTags[k] = v
	}

	uniqueTags := make([]metrics_codec.Tag, 0, len(seenTags))
	for k, v := range seenTags {
		uniqueTags = append(uniqueTags, metrics_codec.Tag{Key: k, Value: v})
	}

	c.globalTags = uniqueTags
}

func (c *client) sortGlobalTags() {
	sort.Sort(metrics_codec.Tags(c.globalTags))
}

// Deprecated: Please use Close() instead
func (c *client) Flush() {
}

func (c *client) GetTenant() string {
	return c.tenant
}

// Load should understand the conf.
func (c *client) Load(conf *apm_vendor_interface.MetricsConfig) {
	var start, end time.Time
	if c.sdk.sdkConfig.DebugMode {
		start = time.Now()
	}
	// If the client is not ready, we directly update its config.
	if !c.IsTenantActive() {
		if conf.IsTenantActive != nil && *conf.IsTenantActive {
			if conf.MinIntervalInSecond != nil && c.timeIntervalSec < *conf.MinIntervalInSecond {
				logs.Warn("The minimal flush interval of tenant %s is %d second(s). "+
					" You set an invalid interval: %d second(s). "+
					"It will reset to the default interval: %d seconds",
					c.tenant, *conf.MinIntervalInSecond, c.timeIntervalSec, c.sdk.sdkConfig.DefaultInterval)
				c.timeIntervalSec = c.sdk.sdkConfig.DefaultInterval
			}

			c.startSender()
			// Finally activate the client.
			c.updateTenantStatus(*conf.IsTenantActive)
		}
	} else {
		// If the client is already started
		logs.Info("Client: %s of Tenant: %s is already started, loading new config: %v",
			c.prefix, c.tenant, utils.Str(conf))

		// Even if SDK found the interval is not valid, (user may modify it in config center after the client is started).
		// SDK cannot update the interval anymore. Just send error logs to users.
		if conf.MinIntervalInSecond != nil && c.timeIntervalSec < *conf.MinIntervalInSecond {
			logs.Error("The minimal flush interval of tenant %s is %d second(s). "+
				"You set an invalid interval: %d second(s).",
				c.tenant, *conf.MinIntervalInSecond, c.timeIntervalSec)
		}

		// The client is active but users forbid the metrics of this tenant.
		if conf.IsTenantActive != nil && !*conf.IsTenantActive {
			// Here we cannot remove all metrics, since users may reactivate this tenant later
			c.updateTenantStatus(*conf.IsTenantActive)
			c.workers.Wait() // wait until the sender exited.
			logs.Warn("all metrics of Client: %s of Tenant: %s will be all discarded.", c.prefix, c.tenant)
		}
	}

	if c.sdk.sdkConfig.DebugMode {
		end = time.Now()
		logs.Info("it takes %v for Client: %s of Tenant: %s to load the conf: %v",
			end.Sub(start), c.prefix, c.tenant, utils.Str(conf))
	}
}

func (c *client) newMetric(metricName string, prefix string, tagNames []string, options []MetricOption) (
	*metric, error) {
	if !utils.IsValidString(metricName) {
		return nil, errors.New(fmt.Sprintf("invalid metrics name: %v", metricName))
	}

	if unique, err := utils.UniqueStrings(tagNames); !unique {
		return nil, errors.New(fmt.Sprintf("duplicate tags: %v", err))
	}

	for _, name := range tagNames {
		if !utils.IsValidString(name) {
			return nil, errors.New(fmt.Sprintf("invalid tag name: %v", name))
		}

		if utils.ContainsTagKey(c.globalTags, name) {
			return nil, errors.New(fmt.Sprintf("tag already existed in global tags: %s", name))
		}
	}

	var name string
	if prefix != "" {
		name = prefix + "." + metricName
	} else {
		name = metricName
	}
	m := &metric{
		name:               name,
		tags:               tagNames,
		sortedGlobalTagPos: make([]int, len(c.globalTags), len(c.globalTags)),
		sortedUserTagPos:   make([]int, len(tagNames), len(tagNames)),
		fnv32aIndices:      make([]int, 0, len(tagNames)),
		d:                  len(tagNames),
		client:             c,
		indexPool: &sync.Pool{
			New: func() interface{} {
				is := make([]string, len(tagNames))
				return TagIndex(&is)
			},
		},

		packetPool: &sync.Pool{
			New: func() interface{} {
				p := packet(make([]byte, 0, 256))
				return &p
			},
		},

		tagPool: &sync.Pool{
			New: func() interface{} {
				ts := mTags(make([]metrics_codec.Tag, 0, 20))
				return &ts
			},
		},

		fieldPool: &sync.Pool{
			New: func() interface{} {
				fs := mFields{
					fieldNames: make([]metrics_codec.Field, 0, defaultBufCap),
					indices:    make([]int32, 0, defaultBufCap),
					values:     make([]float64, 0, defaultBufCap),
				}
				return &fs
			},
		},

		forceMultiFields: false,
		fields:           fields{},
		flags:            0x3 | (0x1 << 3), // 1. the strings are validated, 2. tags are sorted, 3. messages are generated by sdk.
		timerOps:         defaultTimerOps,
		packerID:         int(atomic.AddInt64(&c.packerIdx, 1)-1) % c.packerParallelism,
	}

	m.matrix = (c.collectorFactory)(len(tagNames), m, c.forceDrop, m.client.sdk.tagValidator.Validate)

	for _, op := range options {
		err := op(m)
		if err != nil {
			return nil, err
		}
	}

	m.client.sdk.metricPreparer.Prepare(m)
	err := m.checkConfig()
	if err != nil {
		return nil, err
	}

	m.status = m.newMetricStatus()
	return m, nil
}

func (m *metric) checkConfig() error {
	var err error
	err = m.checkMultiFieldsSetting()
	if err != nil {
		return err
	}

	err = m.setMultiFields()

	return err
}

func (m *metric) checkMultiFieldsSetting() error {
	if m.forceMultiFields && len(m.fields.fields) == 0 {
		return fmt.Errorf("did not specify the fields")
	}

	if m.forceMultiFields && m.timerOps.compatTimer {
		return fmt.Errorf("try to use single-field timer in a multi-field metric")
	}

	return nil
}

func (m *metric) setMultiFields() error {
	err := m.fields.expandFields(m.forceMultiFields, m.client.sdk.sdkConfig.ResourcesLimit.MaxFieldsPerMetric)
	if err != nil {
		return err
	}

	return nil
}

func (m *metric) WithTags(tags ...T) Emitter {
	if m == nil {
		return &errorCursor{nil, fmt.Errorf("nil metric")}
	}
	if len(tags) > m.d {
		return &errorCursor{m, fmt.Errorf("tag: %v does not match length: %d", tags, m.d)}
	}
	index, err := m.constructIndex(tags)
	if err != nil {
		return &errorCursor{m, err}
	}
	cursor, err := m.matrix.Get(*index)
	if err != nil {
		return &errorCursor{m, err}
	}
	m.indexPool.Put(index)
	return cursor
}

func (m *metric) WithTagValues(tagValues ...string) Emitter {
	if m == nil {
		return &errorCursor{nil, fmt.Errorf("nil metric")}
	}
	if len(tagValues) != m.d {
		return &errorCursor{m, fmt.Errorf("tag values: %v does not match length: %d", tagValues, m.d)}
	}
	cursor, err := m.matrix.Get(tagValues)
	if err != nil {
		return &errorCursor{m, err}
	}
	return cursor
}

func (m *metric) NewTagIndex() TagIndex {
	ti := m.indexPool.Get().(TagIndex)
	*ti = (*ti)[:0]
	return ti
}

func (m *metric) AppendTag(index TagIndex, name, value string) {
	var err error
	if value == "" {
		// TODO: remove auto filling
		value = "-"
	}
	if len(*index) >= len(m.tags) {
		err = fmt.Errorf("appended tags are more than defined")
	} else {
		expect := m.tags[len(*index)]
		if expect != name {
			err = fmt.Errorf("unsorted tags, expect %s, find %s", expect, name)
		}
	}
	// use the first and the second elements to pass the error
	// and it would be handled in WithTagIndex
	// it is useful to avoid returning error in each appending
	if err != nil {
		if len(*index) > 0 {
			(*index)[0] = logs.ErrorPrefix
		} else {
			*index = append(*index, logs.ErrorPrefix)
		}
		if len(*index) > 1 {
			(*index)[1] = err.Error()
		} else {
			*index = append(*index, err.Error())
		}
		return
	}
	*index = append(*index, value)
}

func (m *metric) WithTagIndex(index TagIndex) Emitter {
	if m == nil {
		return &errorCursor{nil, fmt.Errorf("nil metric")}
	}

	if len(*index) > 0 && (*index)[0] == logs.ErrorPrefix {
		return &errorCursor{m, fmt.Errorf((*index)[1])}
	}
	if len(*index) != m.d {
		return &errorCursor{m, fmt.Errorf("the number of tags does not match defined")}
	}
	cursor, err := m.matrix.Get(*index)
	if err != nil {
		m.indexPool.Put(index)
		return &errorCursor{m, err}
	}
	m.indexPool.Put(index)
	return cursor
}

func (m *metric) Close() {
	m.closed = true
	m.client.removeMetric(m)
}

func (m *metric) Flush() {}

func (m *metric) storeCursor(cursor *cursor) {
	cursor.prefix = m.name
}

func (m *metric) constructIndex(tags []T) (TagIndex, error) {
	index := m.indexPool.Get().(TagIndex)
	for i := range *index {
		(*index)[i] = "-"
	}
	for d, tag := range tags {
		if m.tags[d] != tag.Name {
			err := m.slowConstructIndex(index, tag)
			if err != nil {
				m.indexPool.Put(index)
				return nil, err
			}
		} else {
			(*index)[d] = tag.Value
		}
	}
	return index, nil
}

func (m *metric) slowConstructIndex(index TagIndex, tag T) error {
	for i, tagName := range m.tags {
		if tagName == tag.Name {
			(*index)[i] = tag.Value
			return nil
		}
	}
	return fmt.Errorf("undefined tag name: %s", tag.Name)
}

// newPacket gets a usable byte slice.
func (m *metric) newPacket() *packet {
	p, ok := m.packetPool.Get().(*packet)
	if !ok {
		pkt := packet(make([]byte, 0, 256))
		return &pkt
	}

	*p = (*p)[:0]
	return p
}

// putPacket recycles the used byte slice.
func (m *metric) putPacket(p *packet) {
	m.packetPool.Put(p)
}

// newTags gets a usable tag slice of the provided length.
func (m *metric) newTags(length int) *mTags {
	ts, ok := m.tagPool.Get().(*mTags)
	if !ok {
		tags := make(mTags, length)
		return &tags
	}

	if cap(*ts) < length {
		tags := make(mTags, length)
		ts = &tags
	}
	*ts = (*ts)[:length]
	return ts
}

// putTags recycles the tag slice.
func (m *metric) putTags(t *mTags) {
	m.tagPool.Put(t)
}

// newTags gets a usable field slice of the provided length.
func (m *metric) newFields() *mFields {
	fs, ok := m.fieldPool.Get().(*mFields)
	if !ok {
		fs := mFields{
			fieldNames: make([]metrics_codec.Field, 0, defaultBufCap),
			indices:    make([]int32, 0, defaultBufCap),
			values:     make([]float64, 0, defaultBufCap),
			pairs:      make([]Pair, 0, defaultBufCap),
		}
		return &fs
	}

	fs.fieldNames = fs.fieldNames[:0]
	fs.indices = fs.indices[:0]
	fs.values = fs.values[:0]
	fs.pairs = fs.pairs[:0]
	return fs
}

// putTags recycles the field slice.
func (m *metric) putFields(t *mFields) {
	m.fieldPool.Put(t)
}

func (c *cursor) Emit(values ...*Value) error {
	var err error
	if c.metric.client.sdk.sdkConfig.SelfMonitor.Enabled && !c.metric.client.isMonitorClient {
		defer c.reportEmitMetricStatus(err)
	}

	for _, value := range values {
		if value == nil {
			continue
		}
		err = c.walkValues(value)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *cursor) Emit1(v *Value, values ...*Value) error {
	var err error
	err = c.walkValues(v)
	if err != nil {
		if c.metric.client.sdk.sdkConfig.SelfMonitor.Enabled && !c.metric.client.isMonitorClient {
			c.reportEmitMetricStatus(err)
		}
		return err
	}
	return c.Emit(values...)
}

func (c *cursor) Emit2(v1 *Value, v2 *Value, vs ...*Value) error {
	var err error
	err = c.walkValues(v1)
	if err != nil {
		if c.metric.client.sdk.sdkConfig.SelfMonitor.Enabled && !c.metric.client.isMonitorClient {
			c.reportEmitMetricStatus(err)
		}
		return err
	}

	return c.Emit1(v2, vs...)
}

func (c *cursor) walkValues(newValue *Value) error {
	// hack meter type
	// TODO: it is stupid to allow user use dynamic multi typed value
	// use declaration instead
	if c.metric.client.isTenantInvalid() {
		return fmt.Errorf("Client: %s of Tenant: %s is invalid, will drop the data", c.metric.client.prefix, c.metric.client.tenant)
	}

	if c.metric.closed {
		return fmt.Errorf("metric %s already closed", c.metric.name)
	}

	// The tenant is initializing
	if !c.metric.client.IsTenantActive() {
		logs.Debug("Client: %s of Tenant: %s is initializing", c.metric.client.prefix, c.metric.client.tenant)
	}

	// If the user declared this is a multi-field metric.
	// We need to check if the pre-declared fields contains the value's suffix (field name) and type.
	if c.metric.forceMultiFields {
		mType := newValue.mType
		if mType == MeterType {
			mType = CounterType
		}
		if !c.metric.fields.contains(newValue.suffix, mType) {
			logs.Error("NOT found this Field (%s, %s) in the fields: %s", newValue.suffix, newValue.mType, c.metric.fields.String())
			return fmt.Errorf("not found this Field (%s, %s) in the fields: %s", newValue.suffix, newValue.mType, c.metric.fields.String())
		}
	}

	switch newValue.mType {
	case MeterType:
		m := measurePool.Get().(*measure)
		m.v.mType = RateCounterType
		m.v.value = newValue.value
		m.v.valuef = newValue.valuef
		if len(newValue.suffix) > 0 {
			m.v.suffix = newValue.suffix + "." + "rate"
		} else {
			m.v.suffix = "rate"
		}
		m.v.interval = c.metric.client.timeIntervalSec
		err := c.values.mergeOrInsert(&m.v)
		if err != nil {
			return err
		}
		newValue.mType = CounterType
		newValue.reportInitialValue = c.metric.client.reportInitialValue
	case HistogramType:
		i := c.metric.fields.indexOf(newValue.suffix, newValue.mType)
		if i == -1 {
			logs.Error("didn't found the buckets for histogram metric %v. It will ignore this value", newValue.suffix)
			return fmt.Errorf("didn't found the buckets for histogram metric %v. It will ignore this value", newValue.suffix)
		}
		buckets := c.metric.fields.fields[i].getBucket()
		if len(buckets) == 0 {
			// logs.Error("empty bucket array of metric: %v. It will ignore this value", newValue.suffix)
			return fmt.Errorf("empty bucket array of metric: %v. It will ignore this value", newValue.suffix)
		}
		newValue.buckets = buckets
	case TimerType:
		newValue.timerOption = &c.metric.timerOps
	case RateCounterType:
		newValue.interval = c.metric.client.timeIntervalSec
	case CounterType:
		newValue.reportInitialValue = c.metric.client.reportInitialValue
	case DeltaCounterType:
		newValue.interval = c.metric.client.timeIntervalSec
		newValue.reportInitialValue = c.metric.client.reportInitialValue
	}

	return c.values.mergeOrInsert(newValue)
}

func (c *cursor) reportEmitMetricStatus(err error) {
	emitter := c.metric.client.sdk.emitMetric.WithTagValues(c.metric.client.tenant, c.metric.name)
	if err == nil {
		emitAndLog(emitter, WithField("emit.success").Incr(1))
	} else {
		emitAndLog(emitter, WithField("emit.fail").Incr(1))
	}
}

// GC not thread safe
func (c *cursor) GC() {
	expireTime := time.Now().Add(-c.metric.client.metricExpireDuration).UnixNano()

	// remove expired node
	prev := c.values.getHead()
	for {
		curr, ok := prev.getNext()
		if !ok {
			break
		}
		lastActiveTime := curr.getLastActiveTime()
		if lastActiveTime > 0 && lastActiveTime < expireTime {
			atomic.AddInt32(&c.gcCount, 1)
			c.values.remove(curr, prev)
			if gcCallback != nil {
				gcCallback(c, curr)
			}
		}
		prev = curr
	}
}

func (c *cursor) IsExpired() bool {
	gcCount := atomic.LoadInt32(&c.gcCount)
	if gcCount == 0 {
		return false
	}
	_, ok := c.values.getHead().getNext()
	return !ok
}

type errorCursor struct {
	metric *metric
	error
}

func (c *errorCursor) Emit(ms ...*Value) error {
	c.reportEmitMetricFailure()

	for _, v := range ms {
		measurePool.Put((*measure)(unsafe.Pointer(v)))
	}
	return c.error
}

func (c *errorCursor) Emit1(m1 *Value, ms ...*Value) error {
	c.reportEmitMetricFailure()

	measurePool.Put((*measure)(unsafe.Pointer(m1)))
	for _, v := range ms {
		measurePool.Put((*measure)(unsafe.Pointer(v)))
	}
	return c.error
}

func (c *errorCursor) Emit2(m1 *Value, m2 *Value, ms ...*Value) error {
	c.reportEmitMetricFailure()
	measurePool.Put((*measure)(unsafe.Pointer(m1)))
	measurePool.Put((*measure)(unsafe.Pointer(m2)))
	for _, v := range ms {
		measurePool.Put((*measure)(unsafe.Pointer(v)))
	}
	return c.error
}

func (c *errorCursor) reportEmitMetricFailure() {
	if c.metric != nil && c.metric.client.sdk.sdkConfig.SelfMonitor.Enabled && !c.metric.client.isMonitorClient {
		emitAndLog(
			c.metric.client.sdk.emitMetric.WithTagValues(c.metric.client.tenant, c.metric.name),
			WithField("emit.fail").Incr(1),
		)
	}
}
