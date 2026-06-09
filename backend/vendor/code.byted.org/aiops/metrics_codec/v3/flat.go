package v3

import (
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"code.byted.org/aiops/metrics_codec"
)

const (
	timestampOffset = 22
	flagOffset      = 30

	bodyHeaderSize       = 13 // Size of BodyHeader is 13
	comperssAlgOffset    = 8
	compressAlgLenOffset = 9
	metricCountOffset    = 25
)

var flatMessagePool = &sync.Pool{
	New: func() interface{} {
		fm := &FlatMessage{
			flatHeader:                 make([]byte, 0, metrics_codec.DefaultHeaderCapacity),
			flatBody:                   make([]byte, 0, metrics_codec.DefaultBodyCapacity),
			header:                     NewMessageHeader(),
			timestamp:                  0,
			flags:                      0x03,
			totalMetricCount:           0,
			totalMeasurementCount:      0,
			currPosOfMetricHeader:      0,
			currMetricMeasurementCount: 0,
			headerOffset:               0,

			noCheck: false,
			dc:      "",
			host:    "",
		}
		return fm
	},
}

// FlatMessage is just two byte slices and some help info.
type FlatMessage struct {
	flatHeader  metrics_codec.FlatStructure // Message header
	flatBody    metrics_codec.FlatStructure // Message body
	flatMetrics metrics_codec.FlatMetrics

	// meta info
	timestamp                  int64
	headerOffset               int // Indicates where to fill back the information in the message header
	flags                      int32
	totalMetricCount           int
	totalMeasurementCount      int
	currPosOfMetricHeader      int  // Record the start of a metric in the buffer
	currMetricMeasurementCount int  // Record the series count in the metric
	currentFieldCount          int  // Record the current field count for the multi-field metric
	isMultiFieldMetric         bool // Indicates if the current metric is a multi-field metric
	isMetricHeaderAppended     bool // Indicates if the metric header is already appended to the buffer
	isSeriesMergeable          bool // Indicates whether FlatMessage will merge series bound to the same metric together

	// properties can set with options
	dc      string
	host    string
	noCheck bool
	header  *MessageHeader
}

// NewFlatMessage creates a empty flat message.
// For historic reason, user must provide a message header.
func NewFlatMessage(ops ...MessageOption) *FlatMessage {
	fm, ok := flatMessagePool.Get().(*FlatMessage)
	if !ok {
		fm = &FlatMessage{
			header: NewMessageHeader(),
		}
	}
	fm.Reset()

	for _, op := range ops {
		err := op(fm)
		if err != nil {
			log.Printf("failed to execute option: %v\n", err)
		}
	}

	if len(fm.dc) > 0 && len(fm.host) > 0 {
		fm.flags |= 0x04
	}

	if fm.isSeriesMergeable {
		fm.flatMetrics = metrics_codec.NewFlatMetrics(false)
	}
	return fm
}

func NewDefaultFlatMessage(ops ...MessageOption) *FlatMessage {
	fm := NewFlatMessage(ops...)
	return fm
}

func PutFlatMessage(fm *FlatMessage) {
	flatMessagePool.Put(fm)
}

func (fm *FlatMessage) Recycle() {
	PutFlatMessage(fm)
}

func (fm *FlatMessage) Clear() {
	fm.flatHeader = fm.flatHeader[:0]
	fm.flatBody = fm.flatBody[:0]
	fm.timestamp = 0
	fm.totalMetricCount = 0
	fm.totalMeasurementCount = 0
	fm.currPosOfMetricHeader = 0
	fm.currMetricMeasurementCount = 0
	fm.headerOffset = 0
	fm.isMultiFieldMetric = false
	fm.currentFieldCount = 0
	fm.isMetricHeaderAppended = false

	if fm.isSeriesMergeable {
		fm.flatMetrics.Clear()
	}
}

func (fm *FlatMessage) Reset() {
	fm.Clear()
	fm.header.Reset()
	fm.dc = ""
	fm.host = ""
	fm.noCheck = false
	fm.flags = 0x03
	fm.isSeriesMergeable = false
	fm.flatMetrics = nil
}

func (fm *FlatMessage) SetMessageHeader(mh *MessageHeader) {
	fm.header = mh
	if mh != nil {
		fm.flags |= mh.flags
	}
}

func (fm *FlatMessage) GetMessageHeader() *MessageHeader {
	if fm.header == nil {
		fm.header = NewMessageHeader()
	}
	return fm.header
}

func (fm *FlatMessage) SetTenant(tenant string) error {
	if fm.header != nil {
		fm.header.Tenant = tenant
	}
	return nil
}

func (fm *FlatMessage) SetClientTimestamp(ts time.Time) error {
	return nil
}

func (fm *FlatMessage) GetClientTimestamp() time.Time {
	return time.Time{}
}

func (fm *FlatMessage) SetGatewayTimestamp(ts time.Time) error {
	return nil
}

func (fm *FlatMessage) GetGatewayTimestamp() time.Time {
	return time.Time{}
}

func (fm *FlatMessage) SetTimestamp(timestamp time.Time) error {
	fm.timestamp = timestamp.Unix()
	return nil
}

func (fm *FlatMessage) GetTimestamp() time.Time {
	return time.Unix(fm.timestamp, 0)
}

func (fm *FlatMessage) SetProperties(properties ...string) error {
	if len(properties) == 0 || (len(properties)&1 == 1) { // ignore odd kvlist
		return fmt.Errorf("the count of properties must be even: %d", len(properties))
	}
	for i := 0; i+1 < len(properties); i += 2 {
		k, v := properties[i], properties[i+1]
		switch k {
		case metrics_codec.TenantKey:
			fm.header.Tenant = v
		case metrics_codec.SdkKey:
			fm.header.SdkVersion = v
		case metrics_codec.DcKey:
			fm.dc = v
		case metrics_codec.HostKey:
			fm.host = v
		case metrics_codec.ProjectKey:
			fm.header.Project = v
		case metrics_codec.AccountKey:
			fm.header.Account = v
		default:
		}
	}
	return nil
}

func (fm *FlatMessage) SetNeedAppendMetricNameFirst(need bool) {
	if fm.isSeriesMergeable {
		fm.flatMetrics.SetNeedAppendMetricNameFirst(need)
	}
	fm.isMetricHeaderAppended = !need
}

func (fm *FlatMessage) IsSeriesAppendable() bool {
	if fm.isSeriesMergeable {
		return fm.flatMetrics.IsSeriesAppendable()
	}
	return fm.isMetricHeaderAppended
}

func (fm *FlatMessage) GetFlags() int32 {
	return fm.flags
}

func (fm *FlatMessage) SetFlags(flag int32) error {
	fm.flags = flag
	return nil
}

func (fm *FlatMessage) GetProject() string {
	return fm.header.Project
}

func (fm *FlatMessage) SetDcHost(dc string, host string) error {
	var err error
	if metrics_codec.IsValidTagValue(dc) {
		fm.dc = dc
	} else {
		err = metrics_codec.ErrInvalidString
	}

	if metrics_codec.IsValidTagValue(host) {
		fm.host = host
	} else {
		err = metrics_codec.ErrInvalidString
	}
	if fm.dc != "" && fm.host != "" {
		fm.flags |= 0x4
	}

	return err
}

func (fm *FlatMessage) Encode(buf []byte) ([]byte, error) {
	if fm.GetSeriesCount() == 0 {
		return buf, nil
	}

	err := fm.finalize()
	if err != nil {
		return buf, err
	}
	buf = append(buf, fm.flatHeader...)
	buf = append(buf, fm.flatBody...)
	return buf, nil
}

func (fm *FlatMessage) Size() int {
	if fm.GetSeriesCount() == 0 {
		return 0
	}

	if fm.isSeriesMergeable {
		return fm.header.Size() + fm.flatMetrics.Size()
	}
	return fm.header.Size() + len(fm.flatBody)
}

func (fm *FlatMessage) GetMetricCount() int {
	if fm.isSeriesMergeable {
		return fm.flatMetrics.GetMetricCount()
	}
	return fm.totalMetricCount
}

func (fm *FlatMessage) GetSeriesCount() int {
	if fm.isSeriesMergeable {
		return fm.flatMetrics.GetSeriesCount()
	}
	return fm.totalMeasurementCount
}

// AppendMetric adds a metric header to the message.
func (fm *FlatMessage) AppendMetric(metricName string, mtype metrics_codec.MType, fields []metrics_codec.Field, ts time.Time) error {
	if !fm.noCheck {
		if !metrics_codec.IsValidString(metricName) {
			return fmt.Errorf("invalid metric name: %v", metricName)
		}

		if fm.Size()+metricHeaderSize(metricName, mtype, fields) >= metrics_codec.MessageSizeLimit {
			return metrics_codec.ErrTooLargeMessage
		}
	}

	if len(fields) > metrics_codec.MaxFieldCount {
		return metrics_codec.ErrTooManyFieldNames
	}

	switch mtype {
	case metrics_codec.TimerType:
		fm.isMultiFieldMetric = true
		fm.currentFieldCount = metrics_codec.TIMER_FIELD_COUNT
	case metrics_codec.HistogramType, metrics_codec.MultiFieldsType:
		fm.isMultiFieldMetric = true
		fm.currentFieldCount = len(fields)
	case metrics_codec.DeltaCounterType:
		fm.isMultiFieldMetric = true
		fm.currentFieldCount = metrics_codec.DELTA_COUNTER_FIELD_COUNT
	default:
		fm.isMultiFieldMetric = false
		fm.currentFieldCount = 0
	}

	if fm.isSeriesMergeable {
		return fm.flatMetrics.AppendMetric(metricName, mtype, fields, ts)
	}

	// Update the last metric header
	if fm.currPosOfMetricHeader < len(fm.flatBody) {
		metrics_codec.FillBackInt32(fm.flatBody, fm.currPosOfMetricHeader, int32(len(fm.flatBody)-fm.currPosOfMetricHeader))
		metrics_codec.FillBackInt32(fm.flatBody, fm.currPosOfMetricHeader+metrics_codec.INT32_LEN, int32(fm.currMetricMeasurementCount))
		fm.currPosOfMetricHeader = len(fm.flatBody)
		fm.currMetricMeasurementCount = 0
	}

	fm.currPosOfMetricHeader = len(fm.flatBody)
	fm.flatBody = metrics_codec.EncodeInt32(fm.flatBody, 0)                                       // occupy 4 bytes for length
	fm.flatBody = metrics_codec.EncodeInt32(fm.flatBody, 0)                                       // occupy 4 bytes for series count
	fm.flatBody = metrics_codec.EncodeInt8(fm.flatBody, int8(mtype))                              // metric type
	fm.flatBody = metrics_codec.EncodeInt32(fm.flatBody, int32(metrics_codec.Fnv32a(metricName))) // hashcode1
	fm.flatBody = metrics_codec.EncodeStr(fm.flatBody, metricName)                                // metric name

	fm.flatBody = metrics_codec.EncodeInt32(fm.flatBody, int32(len(fields)))
	for _, v := range fields {
		fm.flatBody = metrics_codec.EncodeStr(fm.flatBody, v.Name)
		fm.flatBody = metrics_codec.EncodeInt8(fm.flatBody, v.VType)
	}

	fm.totalMetricCount++ // increase the metric count in this information
	fm.isMetricHeaderAppended = true
	return nil
}

// AppendMeasurement adds a measurement/series to the message
// The hashcode is computed and tags are already deduplicted, sorted and encoded.
func (fm *FlatMessage) AppendMeasurement(hashcode int32, tags metrics_codec.FlatStructure, _ []int32, values ...float64) error {
	if len(values) > metrics_codec.MaxFieldCount {
		return metrics_codec.ErrTooManyValues
	}

	if !fm.IsSeriesAppendable() {
		return metrics_codec.ErrMissMetricHeader
	}

	if fm.isMultiFieldMetric && len(values) != fm.currentFieldCount {
		return fmt.Errorf("%w, %d, %d", metrics_codec.ErrFieldCountNotMatch, len(values), fm.currentFieldCount)
	}

	tagBytes := tags
	if !fm.noCheck {
		if fm.Size() >= metrics_codec.MessageSizeLimit {
			return metrics_codec.ErrTooLargeMessage
		}

		if fm.dc != "" && fm.host != "" {
			p := metrics_codec.NewPacket(0)
			defer metrics_codec.PutPacket(p)
			*p = append(*p, tags...)
			var err error
			*p, hashcode, err = addDCAndHostToTagBytes(*p, hashcode, fm.dc, fm.host)
			if err != nil {
				return err
			}
			tagBytes = *p
		}
	}
	if fm.isSeriesMergeable {
		return fm.flatMetrics.AppendMeasurement(hashcode, tagBytes, nil, values...)
	}

	fm.flatBody = appendMeasurement(fm.flatBody, hashcode, tagBytes, values...)
	fm.totalMeasurementCount++      // increase the total series count
	fm.currMetricMeasurementCount++ // increase the series count in the current metric
	return nil
}

// AppendRawMeasurement adds a measurement/series to the message,
// tagsAreSorted indicates whether the tags are sorted
func (fm *FlatMessage) AppendRawMeasurement(tags []metrics_codec.Tag, tagsAreSorted bool, _ []int32, values ...float64) error {
	if len(values) > metrics_codec.MaxFieldCount {
		return metrics_codec.ErrTooManyValues
	}

	if !fm.IsSeriesAppendable() {
		return metrics_codec.ErrMissMetricHeader
	}

	// fmt.Println(fm.isMultiFieldMetric, len(values), fm.currentFieldCount)
	if fm.isMultiFieldMetric && len(values) != fm.currentFieldCount {
		return metrics_codec.ErrFieldCountNotMatch
	}

	if !fm.noCheck {
		if fm.Size() >= metrics_codec.MessageSizeLimit {
			return metrics_codec.ErrTooLargeMessage
		}
	}

	tags, hashcode := fm.checkTags(tags, tagsAreSorted)
	tagBytes := metrics_codec.NewPacket(0)
	defer metrics_codec.PutPacket(tagBytes)
	*tagBytes = metrics_codec.Tags(tags).Encode(*tagBytes)

	if fm.isSeriesMergeable {
		return fm.flatMetrics.AppendMeasurement(hashcode, *tagBytes, nil, values...)
	}

	fm.flatBody = appendMeasurement(fm.flatBody, hashcode, *tagBytes, values...)
	fm.totalMeasurementCount++      // increase the total series count
	fm.currMetricMeasurementCount++ // increase the series count in the current metric
	return nil
}

func (fm *FlatMessage) finalize() error {
	if fm.isSeriesMergeable {
		fm.flatBody, fm.totalMetricCount, fm.totalMeasurementCount =
			fm.flatMetrics.Encode(fm.flatBody), fm.flatMetrics.GetMetricCount(), fm.flatMetrics.GetSeriesCount()
	} else {
		// Update the last metric header
		if fm.currPosOfMetricHeader < len(fm.flatBody) {
			// Fill the last metric header
			metrics_codec.FillBackInt32(fm.flatBody, fm.currPosOfMetricHeader, int32(len(fm.flatBody)-fm.currPosOfMetricHeader))
			metrics_codec.FillBackInt32(fm.flatBody, fm.currPosOfMetricHeader+metrics_codec.INT32_LEN, int32(fm.currMetricMeasurementCount))
			fm.currPosOfMetricHeader = len(fm.flatBody)
			fm.currMetricMeasurementCount = 0
		}
	}

	fm.flatHeader = fm.header.Encode(fm.flatHeader)
	if len(fm.flatHeader) < timestampOffset {
		return metrics_codec.ErrInvalidMessageHeader
	}
	metrics_codec.FillBackInt64(fm.flatHeader, timestampOffset, fm.timestamp)
	if len(fm.flatHeader) < flagOffset {
		return metrics_codec.ErrInvalidMessageHeader
	}
	metrics_codec.FillBackInt32(fm.flatHeader, flagOffset, fm.flags)

	// Set the body offset and length in the message header
	if len(fm.flatHeader) < fm.headerOffset {
		return metrics_codec.ErrInvalidMessageHeader
	}

	// TODO: fill the compressed mode and length
	fm.header.body.FillBack(fm.flatHeader, int32(len(fm.flatHeader)), int32(len(fm.flatBody)), int32(fm.totalMetricCount))
	return nil
}

func (fm *FlatMessage) checkTags(tags []metrics_codec.Tag, sorted bool) ([]metrics_codec.Tag, int32) {
	foundDC, foundHost := false, false

	for _, t := range tags {
		if !foundDC {
			if t.Key == "dc" {
				foundDC = true
			}
		}
		if !foundHost {
			if t.Key == "host" {
				foundHost = true
			}
		}
		if foundDC && foundHost {
			break
		}
	}

	if !foundDC {
		if fm.dc != "" {
			tags = append(tags, metrics_codec.Tag{Key: "dc", Value: fm.dc})
		}
		sorted = false
	}

	if !foundHost {
		if fm.host != "" {
			tags = append(tags, metrics_codec.Tag{Key: "host", Value: fm.host})
		}
		sorted = false
	}

	if !sorted {
		sort.Sort(metrics_codec.Tags(tags))
	}

	hashcode := int32(metrics_codec.Fnv32aTags(tags...))
	return tags, hashcode
}

// tags bytes include the hashcode
func appendMeasurement(buf metrics_codec.FlatStructure, hashcode int32, tags metrics_codec.FlatStructure, values ...float64) metrics_codec.FlatStructure {
	buf = metrics_codec.EncodeInt32(buf, hashcode) // hashcode for tags
	buf = append(buf, tags...)                     // directly append a sorted tag byte slice which is computed in metrics sdk.
	buf = metrics_codec.EncodeFloatArrayV3(buf, values)
	return buf
}

func addDCAndHostToTagBytes(tagBytes []byte, hashcode int32, dc string, host string) ([]byte, int32, error) {
	if len(tagBytes) < metrics_codec.INT32_LEN {
		return tagBytes, hashcode, metrics_codec.ErrTooShortForStringArray
	}
	count := metrics_codec.DecodeUint32(tagBytes)
	tagBytes = metrics_codec.EncodeStr(tagBytes, "dc")
	tagBytes = metrics_codec.EncodeStr(tagBytes, dc)
	tagBytes = metrics_codec.EncodeStr(tagBytes, "host")
	tagBytes = metrics_codec.EncodeStr(tagBytes, host)
	metrics_codec.FillBackInt32(tagBytes, 0, int32(count)+metrics_codec.INT32_LEN)
	hashcode = int32(metrics_codec.ComputeHashcode(uint32(hashcode), metrics_codec.Tag{Key: "dc", Value: dc}, metrics_codec.Tag{Key: "host", Value: host}))
	return tagBytes, hashcode, nil
}
