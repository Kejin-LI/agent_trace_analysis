package metrics_codec

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrUnknownMagicNumber      = fmt.Errorf("unknown_magic_number")
	ErrInvalidDest             = fmt.Errorf("dest bytes is not clean")
	ErrUnsupportedCodecVersion = fmt.Errorf("unsupported_codec_version")
	ErrDecodeBodyHeader        = fmt.Errorf("failed_to_decode_body_header")
	ErrNoEnoughBytesForString  = fmt.Errorf("no_enough_bytes_for_a_string")
	ErrTooShortForStringArray  = fmt.Errorf("no_enough_bytes_for_a_string_array")
	ErrTooManyStringsInArray   = fmt.Errorf("too_many_strings_in_the_array")
	ErrTooLargeMessage         = fmt.Errorf("too_large_message")
	ErrInvalidMessageHeader    = fmt.Errorf("message header is empty or too short")
	ErrSocketNotInit           = errors.New("socket_does_not_init")
	ErrNilConn                 = errors.New("nil_conn_in_agentWriter")
	ErrInvalidString           = errors.New("invalid string")
	ErrTooManyFieldNames       = errors.New("too_many_fields")
	ErrTooManyValues           = errors.New("too_many_values")
	ErrFieldCountNotMatch      = errors.New("field_count_not_match")
	ErrFieldIdxCountNotMatch   = errors.New("field_idx_and_value_not_match")
	ErrMissMetricHeader        = errors.New("metric_header_missing")
)

const (
	MagicCode  = 0x6d65747269637332
	TenantKey  = "tenant"
	SdkKey     = "sdk"
	DcKey      = "dc"
	HostKey    = "host"
	ProjectKey = "project_id"
	AccountKey = "account_id"
)

const (
	INT8_LEN   = 1
	INT16_LEN  = 2
	INT32_LEN  = 4
	INT64_LEN  = 8
	DOUBLE_LEN = 8
)

const (
	TIMER_FIELD_COUNT         = 10
	DELTA_COUNTER_FIELD_COUNT = 3
)

const (
	DefaultHeaderCapacity = 64
	DefaultBodyCapacity   = 256
	defaultPacketCapacity = 256
	maxIdleWindow         = 5
)

type FlatStructure []byte

type Field struct {
	Name  string
	VType int8 // 0 is float64, 1 is int64
}

type Tag struct {
	Key   string
	Value string
}

type Tags []Tag
type T = Tag
type Ts = []Tag

func (ts Tags) Len() int           { return len(ts) }
func (ts Tags) Swap(i, j int)      { ts[i], ts[j] = ts[j], ts[i] }
func (ts Tags) Less(i, j int) bool { return ts[i].Key < ts[j].Key }

func (ts Tags) flat() []string {
	kvs := make([]string, 0, len(ts)*2)
	for _, tag := range ts {
		kvs = append(kvs, tag.Key, tag.Value)
	}
	return kvs
}

func (ts Tags) Encode(buf []byte) []byte {
	buf = EncodeInt32(buf, int32(len(ts)*2))
	for _, v := range ts {
		buf = EncodeStr(buf, v.Key)
		buf = EncodeStr(buf, v.Value)
	}
	return buf
}

type Packet *[]byte

// NewPacket gets a usable byte slice.
func NewPacket(length int) Packet {
	p, ok := packetPool.Get().(Packet)
	if !ok {
		return nil
	}

	*p = (*p)[:0]
	*p = append(*p, make([]byte, length)...)
	return p
}

func PutPacket(p Packet) {
	*p = (*p)[:0]
	packetPool.Put(p)
}

var packetPool = &sync.Pool{
	New: func() interface{} {
		data := make([]byte, 0, defaultPacketCapacity)
		return Packet(&data)
	},
}

type MType int8

const (
	UnknownType MType = iota
	StoreType
	RateCounterType
	CounterType
	MeterType
	TimerType
	HistogramType
	MultiFieldsType
	DeltaCounterType
)

// flatMetrics represents a collection of metrics.
// It is designed to handle the storage and retrieval of metrics in a time-efficient manner.
// Note that flatMetrics are not thread-safe.
type flatMetrics struct {
	// useCodecV4 indicates whether to use codec-v4, it uses codec-v3 by default.
	// codec-v4 allow different timestamps in one message.
	useCodecV4 bool

	// lastUsedSeries points to the most recently accessed or created metric series.
	// This is used to optimize access patterns when the same series is updated multiple times consecutively.
	lastUsedSeries *flatSeries

	// baseTime is the timestamp of the first metric appended within the current window.
	// It serves as a reference point for calculating the time difference (delta) for subsequent metrics.
	baseTime time.Time

	// metrics is a map that uses the time difference (delta) from the baseTime as the key.
	// This is done because Go maps do not shrink in size even when items are deleted.
	// Each entry in the map is a pointer to a MergedMetrics object, which represents a collection of metrics
	// that share the same timestamp.
	// for codec-v3, there should be only one item in the map since one message of codec-v3 can only have one timestamp.
	// for codec-v4, there can be two items in the map.
	metrics map[time.Duration]*mergedMetrics

	pool             *bufferPool // pool is a buffer pool used for efficient key generation
	size             int         // speedup Size(). It is a high-frequency op which can reduce 50% encoding cost
	totalMetricCount int         // speedup GetMetricCount by 95%
	totalSeriesCount int         // speedup GetMetricCount by 95%
}

// mergedMetrics represents a collection of metrics that share the same timestamp.
type mergedMetrics struct {
	initialized  bool
	mergedSeries map[string]*flatSeries
}

// flatSeries represents a collection of series that share the same metric name.
type flatSeries struct {
	seriesCount int
	idle        int
	FlatStructure
}

func NewFlatMetrics(useCodecV4 bool) FlatMetrics {
	return &flatMetrics{
		baseTime:       time.Time{},
		lastUsedSeries: nil,
		metrics:        make(map[time.Duration]*mergedMetrics),
		useCodecV4:     useCodecV4,
		pool:           newBufferPool(),
	}
}

func (fm *flatMetrics) Reset() {
	fm.Clear()
	fm.metrics = make(map[time.Duration]*mergedMetrics)
}

func (fm *flatMetrics) Clear() {
	fm.baseTime = time.Time{}
	fm.lastUsedSeries = nil
	fm.size = 0
	fm.totalSeriesCount = 0
	fm.totalMetricCount = 0

	// Loop through all the metrics
	for _, v := range fm.metrics {
		// Loop through all the series in the current metric
		for k, s := range v.mergedSeries {
			s.idle++
			if s.idle >= maxIdleWindow {
				delete(v.mergedSeries, k)
			}

			// If the metric is initialized, reset the series count and flat structure
			if v.initialized {
				s.seriesCount = 0
				s.FlatStructure = s.FlatStructure[:0]
			}
		}
		v.initialized = false
	}
}

func (fm *flatMetrics) Encode(buffer []byte) []byte {
	for _, vs := range fm.metrics {
		if !vs.initialized {
			continue
		}
		for _, v := range vs.mergedSeries {
			if v.seriesCount > 0 {
				FillBackInt32(v.FlatStructure, 0, int32(len(v.FlatStructure)))
				FillBackInt32(v.FlatStructure, INT32_LEN, int32(v.seriesCount))
				buffer = append(buffer, v.FlatStructure...)
			}
		}
	}
	return buffer
}

// AppendMetric appends the metric header info
// If the metric already exited, directly return nil
func (fm *flatMetrics) AppendMetric(metricName string, mtype MType, fields []Field, ts time.Time) error {
	if len(fields) > MaxFieldCount {
		return ErrTooManyFieldNames
	}

	seriesSet, newMetric := fm.getOrCreateMetric(metricName, mtype, fields, ts)
	defer func() { fm.lastUsedSeries = seriesSet }()
	if !newMetric {
		return nil
	}

	// Append Metric Header if this metric is new
	seriesSet.idle = 0
	seriesSet.FlatStructure = seriesSet.FlatStructure[:0]
	seriesSet.FlatStructure = EncodeInt32(seriesSet.FlatStructure, 0)                         // occupy 4 bytes for length
	seriesSet.FlatStructure = EncodeInt32(seriesSet.FlatStructure, 0)                         // occupy 4 bytes for series count
	seriesSet.FlatStructure = EncodeInt8(seriesSet.FlatStructure, int8(mtype))                // metric type
	seriesSet.FlatStructure = EncodeInt32(seriesSet.FlatStructure, int32(Fnv32a(metricName))) // hashcode1

	if fm.useCodecV4 {
		seriesSet.FlatStructure = EncodeInt64(seriesSet.FlatStructure, ts.Unix())
	}
	seriesSet.FlatStructure = EncodeStr(seriesSet.FlatStructure, metricName) // metric name
	seriesSet.FlatStructure = EncodeInt32(seriesSet.FlatStructure, int32(len(fields)))
	for _, v := range fields {
		seriesSet.FlatStructure = EncodeStr(seriesSet.FlatStructure, v.Name)
		seriesSet.FlatStructure = EncodeInt8(seriesSet.FlatStructure, v.VType)
	}
	return nil
}

// AppendMeasurement appends the series. It uses the lastUsedSeries to append.
// This is for compatible API purpose.
func (fm *flatMetrics) AppendMeasurement(hashcode int32, tags FlatStructure, fieldIndices []int32, values ...float64) error {
	if fm.lastUsedSeries == nil {
		return ErrMissMetricHeader
	}

	oldLen := len(fm.lastUsedSeries.FlatStructure)
	if fm.lastUsedSeries.seriesCount == 0 {
		fm.size += oldLen
		fm.totalMetricCount++
	}

	fm.totalSeriesCount++
	fm.lastUsedSeries.seriesCount++
	fm.lastUsedSeries.FlatStructure = fm.appendMeasurement(
		fm.lastUsedSeries.FlatStructure, hashcode, tags, fieldIndices, values...)
	newLen := len(fm.lastUsedSeries.FlatStructure)
	fm.size += newLen - oldLen
	return nil
}

// IsSeriesAppendable checks whether a series (without metric header) can be inserted
func (fm *flatMetrics) IsSeriesAppendable() bool {
	return fm.lastUsedSeries != nil && len(fm.lastUsedSeries.FlatStructure) > 0
}

// SetNeedAppendMetricNameFirst marks a metric header must be inserted first
func (fm *flatMetrics) SetNeedAppendMetricNameFirst(need bool) {
	if need {
		fm.lastUsedSeries = nil
		fm.baseTime = time.Time{}
	}
}

func (fm *flatMetrics) Size() int {
	return fm.size
}

func (fm *flatMetrics) GetMetricCount() int {
	return fm.totalMetricCount
}

func (fm *flatMetrics) GetSeriesCount() int {
	return fm.totalSeriesCount
}

// getOrCreateMetric either fetches an existing metric or create a new one if it doesn't exist.
// It uses the metric name, type, fields, and timestamp to identify the metric.
// It returns a pointer to the metric (or newly created metric) and a boolean indicating whether a new metric was created.
func (fm *flatMetrics) getOrCreateMetric(metricName string, metricType MType, fields []Field, ts time.Time) (*flatSeries, bool) {
	var created bool

	if fm.metrics == nil {
		fm.metrics = make(map[time.Duration]*mergedMetrics)
	}

	// Initialize the baseTime if it's not already done
	if fm.baseTime.IsZero() {
		fm.baseTime = ts
	}

	// Calculate the time difference between the baseTime and the given timestamp and generate the key for the metricsWithTimestamp
	delta := ts.Sub(fm.baseTime)

	// Metrics are already in the map in most cases. so we use sync pool to avoid mem allocation.
	buffer := fm.pool.get()
	defer fm.pool.put(buffer)
	key := generateKey(*buffer, metricName, metricType, fields)

	// Try to get the mergedMetrics for the given delta. If it doesn't exist, create a new one and add it to the map.
	metricsWithTimestamp, timestampExisted := fm.metrics[delta]
	if !timestampExisted {
		metricsWithTimestamp = &mergedMetrics{
			mergedSeries: make(map[string]*flatSeries),
		}
		fm.metrics[delta] = metricsWithTimestamp
	}

	// If the mergedMetrics was not initialized before, mark it as a new metricsWithTimestamp
	created = !metricsWithTimestamp.initialized
	metricsWithTimestamp.initialized = true

	// Try to get the flatSeries for the given key generated by metric name
	// If it doesn't exist, create a new one and add it to the map
	series, metricExisted := metricsWithTimestamp.mergedSeries[key]
	if !metricExisted {
		series = &flatSeries{
			seriesCount:   0,
			FlatStructure: make([]byte, 0, 1024),
		}
		metricsWithTimestamp.mergedSeries[cloneStr(key)] = series
	}

	// If the series was not initialized before, mark it as a new metricsWithTimestamp
	created = created || len(series.FlatStructure) == 0

	return series, created
}

// tags bytes include the hashcode
func (fm *flatMetrics) appendMeasurement(buf FlatStructure, hashcode int32,
	tags FlatStructure, fieldIndices []int32, values ...float64) FlatStructure {
	buf = EncodeInt32(buf, hashcode) // hashcode for tags

	// directly append a sorted tag byte slice which is computed in metrics sdk.
	buf = append(buf, tags...)
	if fm.useCodecV4 {
		buf = EncodeInt32Array(buf, fieldIndices)
	}
	buf = EncodeFloatArrayV3(buf, values)
	return buf
}
