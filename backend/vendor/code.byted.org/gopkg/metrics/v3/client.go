package metrics

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

const (
	errorPrefix = "[Metrics(v3)]"
	// limit non aggregated timer sending frequency, prevent aggregated metrics
	maxTimerPerSent       = 128
	tick                  = 5 * time.Second
	minGCInterval         = 5 * time.Minute
	defaultExpireDuration = 24 * time.Hour
	defaultGCInterval     = 0.25
	defaultGCThreshold    = 0.3

	// LogLevelEnvKey 可以设置这个环境变量来影响日志等级
	LogLevelEnvKey = "METRICS_LOG_LEVEL"
)

const (
	collectorFactoryKeyKDTree = "kdtree"
	collectorFactoryKeyMap    = "map"
)

var (
	// enableDebug is a trigger to toggle debug message or not.
	enableDebug = os.Getenv("DEBUG_GOPKG_METRICS") != ""

	// polishLog indicates whether to print the log in Prometheus style.
	polishLog = os.Getenv("METRICS_LOG_DETAIL") != ""

	// default collector factory: kdtree or map
	defaultCollectorFactory = os.Getenv("METRICS_DEFAULT_COLLECTOR")
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
// it boost the performance of metrics in a variety of ways,
// and keep some flexibilities.
type Client interface {
	// NewMetric creates a metric set and define its tag names,
	// it would panic if tag name is not valid.
	NewMetric(metricName string, tagNames ...string) Metric

	// Close closes a client,
	// all workers would exit and all data would be purged (send and recycle).
	Close()

	// Flush flushes all metrics and emits them at once.
	Flush()
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
}

type client struct {
	prefix               string
	metrics              []*metric
	sender               *sender
	globalTags           []byte
	ticker               *time.Ticker
	close                chan bool
	workers              sync.WaitGroup
	collectorFactory     Collector
	forceDrop            int
	metricExpireDuration time.Duration
	metricGCInterval     float64
	metricGCThreshold    float64
	sync.Mutex

	histogramTimer bool
	timerClose     chan bool
	timerFlush     chan bool
	timerBuf       *ringBuffer
}

type cursor struct {
	values           *queue
	index            []string
	globalTagLiteral []byte
	prefix           string
	metric           *metric

	// gc stats
	gcCount int32
}

type metric struct {
	name      string
	matrix    collector
	tags      []string
	d         int
	client    *client
	indexPool *sync.Pool
}

// NewClient creates a default client with agent writer.
func NewClient(prefix string, ops ...ClientOption) Client {
	client := &client{
		prefix:               prefix,
		metrics:              make([]*metric, 0, 8),
		ticker:               time.NewTicker(tick),
		metricExpireDuration: defaultExpireDuration,
		metricGCInterval:     defaultGCInterval,
		metricGCThreshold:    defaultGCThreshold,
		close:                make(chan bool),

		timerClose: make(chan bool),
		timerFlush: make(chan bool),
		forceDrop:  (1 << 13) - 1,
		timerBuf:   newRingBuffer(1024),
	}

	// load options
	for _, option := range ops {
		option(client)
	}

	// it sucks, we should make NewClient be more common,
	// and use option to control the default behavior
	if client.sender == nil {
		var w io.WriteCloser
		w, err := NewAgentWriter()
		if err != nil {
			warnLog(
				"metrics new writer error: %s, all metrics would be dropped",
				err,
			)
		}
		client.sender = newSender(w)
	}
	if client.collectorFactory == nil {
		switch defaultCollectorFactory {
		case collectorFactoryKeyMap:
			client.collectorFactory = Map
		case collectorFactoryKeyKDTree:
			client.collectorFactory = KDTree
		default:
			client.collectorFactory = KDTree
		}
	}

	client.workers.Add(1)
	go client.send()
	go client.gc()

	if !client.histogramTimer {
		client.workers.Add(1)
		go client.timerSend()
	}
	return client
}

func (c *client) gc() {
	if c.metricExpireDuration <= 0 {
		return
	}

	gcInterval := time.Duration(math.Max(float64(c.metricExpireDuration)*c.metricGCInterval,
		math.Max(float64(tick*10), float64(minGCInterval))))
	for {
		select {
		case <-c.close:
			return
		case <-time.After(gcInterval):
			c.Lock()
			cms := make([]*metric, len(c.metrics))
			copy(cms, c.metrics)
			c.Unlock()
			startTime := time.Now()
			if enableDebug {
				debugLog("start gc at %s", startTime.Format(time.RFC3339Nano))
			}
			for _, m := range cms {
				m.matrix.GC()
			}
			if enableDebug {
				debugLog("end gc, using %s", time.Now().Sub(startTime))
			}
		}
	}
}

func (c *client) send() {
	for {
		select {
		case <-c.ticker.C:
			var startTime time.Time
			if enableDebug {
				startTime = time.Now()
				debugLog("start sending at %s", startTime.Format(time.RFC3339Nano))
			}
			c.Lock()
			for _, m := range c.metrics {
				m.matrix.Send()
			}
			c.Unlock()
			if enableDebug {
				debugLog("end sending, using %s", time.Now().Sub(startTime))
			}
		case <-c.close:
			c.ticker.Stop()
			c.workers.Done()
			return
		}
	}
}

func (c *client) timerSend() {
	sleep := time.Millisecond
	for {
		select {
		case <-c.timerClose:
			c.workers.Done()
			return
		case <-c.timerFlush:
			batchPacket := make([]timerPacket, 0, maxTimerPerSent)
			for {
				v, _ := c.timerBuf.poll()
				if v == empty {
					if len(batchPacket) > 0 {
						c.sideSend(batchPacket)
					}
					break
				}
				batchPacket = append(batchPacket, v)
			}
		default:
			batchPacket := make([]timerPacket, 0, maxTimerPerSent)
			for {
				v, _ := c.timerBuf.poll()
				if v == empty {
					if sleep > 100*time.Millisecond && len(batchPacket) > 0 {
						c.sideSend(batchPacket)
						break
					}
					time.Sleep(sleep)
					if sleep < 200*time.Millisecond {
						sleep *= 2
						continue
					} else {
						break
					}
				}
				sleep = time.Millisecond
				batchPacket = append(batchPacket, v)
				if len(batchPacket) >= maxTimerPerSent {
					c.sideSend(batchPacket)
					break
				}
			}
		}
	}
}

func (c *client) removeMetric(m *metric) {
	c.Lock()
	defer c.Unlock()
	for i, ptr := range c.metrics {
		if ptr == m {
			ptr.matrix.Send()

			nextLen := len(c.metrics) - 1
			c.metrics[i] = c.metrics[nextLen]
			// set the last element nil to avoid memory leak.
			c.metrics[nextLen] = nil
			c.metrics = c.metrics[:nextLen]
			break
		}
	}
}

func (c *client) NewMetric(metricName string, tagNames ...string) Metric {
	m, err := c.newMetric(metricName, c.prefix, tagNames...)
	if err != nil {
		panic(err)
	}
	c.Lock()
	defer c.Unlock()
	if len(c.metrics) >= 1024 {
		log(
			"metrics client keeps over 1024 metric instances, ignore new creation: %s.",
			metricName,
		)
		return &noopMetric{}
	}
	c.metrics = append(c.metrics, m)
	return m
}

func (c *client) Close() {
	close(c.close)
	c.Flush()
	close(c.timerClose)
	c.workers.Wait()
	c.sender.close()
}

func (c *client) Flush() {
	if !c.histogramTimer {
		c.timerFlush <- true
	}
	for _, m := range c.metrics {
		m.Flush()
	}
}

func (c *client) newMetric(metricName string, prefix string, tagNames ...string) (*metric, error) {
	if metricName != "" && !isValidString(metricName) {
		return nil, errors.New(fmt.Sprintf("invalid metrics name: %v", metricName))
	}
	for _, name := range tagNames {
		if !isValidString(name) {
			return nil, errors.New(fmt.Sprintf("invalid tag name: %v", name))
		}
	}
	var name string
	if metricName != "" {
		if prefix != "" {
			name = prefix + "." + metricName
		} else {
			name = metricName
		}
	} else {
		if prefix == "" {
			panic(fmt.Sprintf("%s can not set both client and metric name empty", errorPrefix))
		}
		name = prefix
	}
	m := &metric{
		name:   name,
		tags:   tagNames,
		d:      len(tagNames),
		client: c,
		indexPool: &sync.Pool{
			New: func() interface{} {
				is := make([]string, len(tagNames))
				return TagIndex(&is)
			},
		},
	}
	validator := func(index []string) error {
		for _, value := range index {
			if !isValidString(value) {
				return fmt.Errorf("invalid tag value: %v", value)
			}
		}
		return nil
	}
	m.matrix = (c.collectorFactory)(len(tagNames), m, c.forceDrop, validator)
	return m, nil
}

func (m *metric) WithTags(tags ...T) Emitter {
	if len(tags) > m.d {
		return &errorCursor{fmt.Errorf("tag: %v does not match length: %d", tags, m.d)}
	}
	index, err := m.constructIndex(tags)
	if err != nil {
		return &errorCursor{err}
	}
	cursor, err := m.matrix.Get(*index)
	if err != nil {
		return &errorCursor{err}
	}
	m.indexPool.Put(index)
	return cursor
}

func (m *metric) WithTagValues(tagValues ...string) Emitter {
	if len(tagValues) != m.d {
		return &errorCursor{fmt.Errorf("tag values: %v does not match length: %d", tagValues, m.d)}
	}
	cursor, err := m.matrix.Get(tagValues)
	if err != nil {
		return &errorCursor{err}
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
			(*index)[0] = errorPrefix
		} else {
			*index = append(*index, errorPrefix)
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
	if (*index)[0] == errorPrefix {
		return &errorCursor{fmt.Errorf((*index)[1])}
	}
	if len(*index) != m.d {
		return &errorCursor{fmt.Errorf("the number of tags does not match defined")}
	}
	cursor, err := m.matrix.Get(*index)
	if err != nil {
		m.indexPool.Put(index)
		return &errorCursor{err}
	}
	m.indexPool.Put(index)
	return cursor
}

func (m *metric) Close() {
	m.client.removeMetric(m)
}

func (m *metric) Flush() {
	m.matrix.Send()
	if !m.client.histogramTimer {
		m.client.timerFlush <- true
	}
}

func (m *metric) storeCursor(cursor *cursor) {
	cursor.prefix = m.name
	cursor.globalTagLiteral = m.client.globalTags
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

func (c *cursor) Emit(values ...*Value) error {
	for _, value := range values {
		if value == nil {
			continue
		}
		c.walkValues(value)
	}
	return nil
}

func (c *cursor) Emit1(v *Value, values ...*Value) error {
	c.walkValues(v)
	return c.Emit(values...)
}

func (c *cursor) Emit2(v1 *Value, v2 *Value, vs ...*Value) error {
	c.walkValues(v1)
	return c.Emit1(v2, vs...)
}

type timerPacket struct {
	c     *cursor
	timer *Value
}

func (c *cursor) walkValues(newValue *Value) {
	// to avoid the searching overhead,
	// hack here check if need to send instead of creat another reservoir
	if !c.metric.client.histogramTimer && newValue.mType == timerType {
		ok, _ := c.metric.client.timerBuf.offer(timerPacket{c, newValue})
		if !ok {
			measurePool.Put((*measure)(unsafe.Pointer(newValue)))
		}
	} else {
		c.values.mergeOrInsert(newValue)
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
	error
}

func (c *errorCursor) Emit(ms ...*Value) error {
	for _, v := range ms {
		measurePool.Put((*measure)(unsafe.Pointer(v)))
	}
	return c.error
}

func (c *errorCursor) Emit1(m1 *Value, ms ...*Value) error {
	measurePool.Put((*measure)(unsafe.Pointer(m1)))
	for _, v := range ms {
		measurePool.Put((*measure)(unsafe.Pointer(v)))
	}
	return c.error
}

func (c *errorCursor) Emit2(m1 *Value, m2 *Value, ms ...*Value) error {
	measurePool.Put((*measure)(unsafe.Pointer(m1)))
	measurePool.Put((*measure)(unsafe.Pointer(m2)))
	for _, v := range ms {
		measurePool.Put((*measure)(unsafe.Pointer(v)))
	}
	return c.error
}
