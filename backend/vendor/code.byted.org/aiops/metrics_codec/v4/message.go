package v4

import (
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"code.byted.org/aiops/metrics_codec"
)

const (
	magicCode int64 = 0x6d65747269637332
	version   int8  = 4
)

var messagePool = sync.Pool{
	New: func() interface{} {
		return &Message{
			Header: &MessageHeader{
				magicCode: magicCode,
				version:   version,
				Tenant:    "default",
				flags:     0x03,
			},
			Body: []*Metric{},
			dc:   "",
			host: "",
		}
	},
}

type Message struct {
	Header *MessageHeader
	Body   Body

	// internal fields or properties
	dc   string
	host string
}

func NewMessage() *Message {
	m, ok := messagePool.Get().(*Message)
	if !ok {
		return &Message{
			Header: NewMessageHeader(),
		}
	}
	m.Header.Reset()
	m.Body = m.Body[:0]
	m.dc = ""
	m.host = ""

	return m
}

func PutMessage(m *Message) {
	m.Header.flags = 0x03
	for _, v := range m.Body {
		for _, m := range v.Measurements {
			m.Recycle()
		}
	}
	messagePool.Put(m)
}

func (m *Message) Encode(buf []byte) []byte {
	if len(m.Body) == 0 {
		return buf
	}
	buf = m.Header.Encode(buf)
	startAt := len(buf)
	seriesCount := 0
	for _, metric := range m.Body {
		buf = metric.Encode(buf, m.dc, m.host)
		seriesCount += metric.MeasurementCount()
	}

	m.Header.body.FillBack(buf, int32(startAt), int32(len(buf)-startAt), int32(len(m.Body)), int32(seriesCount))
	return buf
}

func (m *Message) SetDcHost(dc string, host string) error {
	var err error
	if metrics_codec.IsValidTagValue(dc) {
		m.dc = dc
	} else {
		err = metrics_codec.ErrInvalidString
	}

	if metrics_codec.IsValidTagValue(host) {
		m.host = host
	} else {
		err = metrics_codec.ErrInvalidString
	}
	if m.dc != "" && m.host != "" {
		m.Header.flags |= 0x4
	}

	return err
}

func (m *Message) GetProject() string {
	if len(m.Header.Tags) < 4 {
		return ""
	}
	return m.Header.Tags[3]
}

// MessageHeader
// +----------+-------+-----------+--------+----------------+-------------------+---------+----------+-----+------------+------------+------+---------------+-----------+
// |   8B     |  1B   |    4B     |   4B   |      1B        |         4B        |    8B   |   8B     | 4B  |    4B      |     4B     | vStr |     []vStr    |    vStr   |
// +----------+-------+-----------+--------+----------------+-------------------+---------+----------+-----+------------+------------+------+---------------+-----------+
// |magic code|version|body offset|body len|body compression|body compressed len|timestamp|gateway_ts|flags|metric count|series count|tenant|tenant tags    |sdk_version|
// +----------+-------+-----------+--------+----------------+-------------------+---------+----------+-----+------------+------------+------+---------------+-----------+
type MessageHeader struct {
	magicCode        int64
	version          int8
	body             BodyHeader
	ClientTimestamp  int64 // ms
	GatewayTimestamp int64 // ms
	flags            int32
	metricNum        int32
	seriesNum        int32
	Tenant           string
	Tags             []string
	SdkVersion       string
}

func NewMessageHeader() *MessageHeader {
	return &MessageHeader{
		magicCode: magicCode,
		version:   version,
		Tenant:    "default",
		flags:     0x03,
	}
}

// Size returns the size of the message header.
func (h *MessageHeader) Size() int {
	res := 0
	res += metrics_codec.INT64_LEN
	res += metrics_codec.INT8_LEN
	bodyHeaderSize := bodyHeaderSize
	res += bodyHeaderSize
	res += metrics_codec.INT64_LEN + metrics_codec.INT64_LEN + metrics_codec.INT32_LEN + metrics_codec.INT32_LEN + metrics_codec.INT32_LEN
	res += metrics_codec.SizeOfVStr(h.Tenant)
	res += metrics_codec.SizeOfVStrs(h.Tags)
	res += metrics_codec.SizeOfVStr(h.SdkVersion)
	return res
}

func (h *MessageHeader) Encode(buf []byte) []byte {
	buf, _ = h.encode(buf)
	return buf
}

func (h *MessageHeader) encode(buf []byte) ([]byte, int) {
	if h == nil {
		h = NewMessageHeader()
	}
	buf = metrics_codec.EncodeInt64(buf, h.magicCode)
	buf = metrics_codec.EncodeInt8(buf, h.version)
	offset32 := len(buf)
	buf = h.body.Encode(buf)

	buf = metrics_codec.EncodeInt64(buf, h.ClientTimestamp) // Timestamp
	buf = metrics_codec.EncodeInt64(buf, h.GatewayTimestamp)
	buf = metrics_codec.EncodeInt32(buf, h.flags)

	buf = metrics_codec.EncodeInt32(buf, h.metricNum) // Just occupy the 4 bytes
	buf = metrics_codec.EncodeInt32(buf, h.seriesNum) // Series Count
	buf = metrics_codec.EncodeStr(buf, h.Tenant)
	buf = metrics_codec.EncodeStrArray(buf, h.Tags)
	buf = metrics_codec.EncodeStr(buf, h.SdkVersion)
	return buf, offset32
}

func (h *MessageHeader) Reset() {
	h.metricNum = 0
	h.seriesNum = 0
	h.ClientTimestamp = 0
	h.GatewayTimestamp = 0
	h.Tags = h.Tags[:0]
	h.flags = 0x03
	h.Tenant = "default"
	h.SdkVersion = ""
	h.body.offset = 0
}

func (h *MessageHeader) GetFlags() int32 {
	return h.flags
}

func (h *MessageHeader) SetFlags(flags int32) {
	h.flags = flags
}

type BodyHeader struct {
	Offset       int32
	Len          int32
	CompressMode int8
	CompressLen  int32
	offset       int
}

func (bh *BodyHeader) Encode(buf []byte) []byte {
	bh.offset = len(buf)
	buf = metrics_codec.EncodeInt32(buf, bh.Offset)
	buf = metrics_codec.EncodeInt32(buf, bh.Len)
	buf = metrics_codec.EncodeInt8(buf, bh.CompressMode)
	buf = metrics_codec.EncodeInt32(buf, bh.CompressLen)
	return buf
}

func (bh *BodyHeader) FillBack(buf []byte, bodyOffset int32, bodyLen int32, metricCount int32, seriesCount int32) {
	metrics_codec.FillBackInt32(buf, bh.offset, bodyOffset)
	metrics_codec.FillBackInt32(buf, bh.offset+metrics_codec.INT32_LEN, bodyLen)
	metrics_codec.FillBackInt8(buf, bh.offset+comperssAlgOffset, 0)           // compress alg
	metrics_codec.FillBackInt32(buf, bh.offset+compressAlgLenOffset, bodyLen) // compress len
	// Fill back the metric count

	// There are body offset, body len, body compress alg, body compressed len,
	// client_timestamp, gateway_ts, and flags before the metric account.
	// 4 + 4 + 1 + 4 +
	// 8 + 8 + 4
	metrics_codec.FillBackInt32(buf, bh.offset+metricCountOffset, metricCount)
	metrics_codec.FillBackInt32(buf, bh.offset+seriesCountOffset, seriesCount)
}

type Body []*Metric

type Metric struct {
	Header       MetricHeader
	Measurements []*Measurement
}

func NewMetric(metricName string, mType metrics_codec.MType, fields []metrics_codec.Field, ts time.Time) *Metric {
	return &Metric{
		Header: MetricHeader{
			MType:     mType,
			Name:      metricName,
			Fields:    fields,
			Timestamp: ts.Unix(),
			len:       0,
			offset:    0,
		},
		Measurements: nil,
	}
}

func (m *Metric) MeasurementCount() int {
	return len(m.Measurements)
}

func (m *Metric) Encode(buf []byte, dc, host string) []byte {
	startAt := len(buf)
	buf = m.Header.Encode(buf, m.Measurements)
	for _, m := range m.Measurements {
		buf = m.Encode(buf, dc, host)
	}
	m.Header.FillBack(buf, int32(len(buf)-startAt))
	return buf
}

type MetricHeader struct {
	MType     metrics_codec.MType
	Timestamp int64 // Unix time, Unit is second.
	Name      string
	Fields    []metrics_codec.Field

	// internal fields
	len    int32
	offset int
}

// metricHeaderSize
func metricHeaderSize(metricName string, _ metrics_codec.MType, fields []metrics_codec.Field) int {
	length := metrics_codec.INT32_LEN // metric len
	length += metrics_codec.INT32_LEN // series count
	length += metrics_codec.INT8_LEN  // mType
	length += metrics_codec.INT32_LEN // hashcode
	length += metrics_codec.INT64_LEN // timestamp
	length += metrics_codec.SizeOfVStr(metricName)
	length += metrics_codec.INT32_LEN

	for _, v := range fields {
		length += metrics_codec.INT32_LEN + len(v.Name)
		length += metrics_codec.INT8_LEN
	}
	return length
}

func (h *MetricHeader) Encode(buf []byte, measurer []*Measurement) []byte {
	h.offset = len(buf)

	buf = metrics_codec.EncodeInt32(buf, h.len)
	buf = metrics_codec.EncodeInt32(buf, int32(len(measurer)))
	buf = metrics_codec.EncodeInt8(buf, int8(h.MType))
	buf = metrics_codec.EncodeInt32(buf, int32(metrics_codec.Fnv32a(h.Name)))
	buf = metrics_codec.EncodeInt64(buf, h.Timestamp)
	buf = metrics_codec.EncodeStr(buf, h.Name)

	fields := h.Fields
	if len(fields) > metrics_codec.MaxFieldCount {
		fmt.Fprintf(os.Stderr, "%s has too many fields: %d, truncated to %d\n", h.Name, len(fields), metrics_codec.MaxFieldCount)
		fields = fields[:metrics_codec.MaxFieldCount]
	}

	buf = metrics_codec.EncodeInt32(buf, int32(len(fields)))
	for _, v := range fields {
		buf = metrics_codec.EncodeStr(buf, v.Name)
		buf = metrics_codec.EncodeInt8(buf, v.VType)
	}

	return buf
}

func (h *MetricHeader) FillBack(buf []byte, mLen int32) {
	metrics_codec.FillBackInt32(buf, h.offset, mLen)
}

type Measurement struct {
	Tags    []metrics_codec.Tag
	Indices []int32
	Values  []float64

	Fnv32a uint32
	Sorted bool
}

func NewMeasurement() *Measurement {
	m, ok := measurementPool.Get().(*Measurement)
	if !ok {
		return nil
	}
	m.Tags = m.Tags[:0]
	m.Indices = m.Indices[:0]
	m.Values = m.Values[:0]
	m.Sorted = false
	m.Fnv32a = 0
	return m
}

func (m *Measurement) SetFnv32a(fnv32a uint32) { // hashcode is computed in sdk
	m.Fnv32a = fnv32a
}

func (m *Measurement) SetSorted(sorted bool) {
	m.Sorted = sorted
}

func (m *Measurement) Encode(buf []byte, dc, host string) []byte {
	tags := m.Tags
	if dc != "" {
		tags = append(tags, metrics_codec.Tag{Key: "dc", Value: dc})
	}

	if host != "" {
		tags = append(tags, metrics_codec.Tag{Key: "host", Value: host})
	}

	if !m.Sorted {
		sort.Sort(metrics_codec.Tags(tags))
	}

	if m.Fnv32a == 0 {
		m.Fnv32a = metrics_codec.Fnv32aTags(tags...)
	}

	buf = metrics_codec.EncodeInt32(buf, int32(m.Fnv32a))

	buf = metrics_codec.EncodeInt32(buf, int32(len(tags)*2))
	for _, v := range tags {
		buf = metrics_codec.EncodeStr(buf, v.Key)
		buf = metrics_codec.EncodeStr(buf, v.Value)
	}

	fieldIndices := m.Indices
	if len(fieldIndices) > metrics_codec.MaxFieldCount {
		fieldIndices = fieldIndices[:metrics_codec.MaxFieldCount]
	}

	buf = metrics_codec.EncodeInt32Array(buf, fieldIndices)

	values := m.Values
	if len(values) > metrics_codec.MaxFieldCount {
		values = values[:metrics_codec.MaxFieldCount]
	}
	buf = metrics_codec.EncodeFloatArrayV3(buf, values)
	return buf
}

func (m *Measurement) Recycle() {
	measurementPool.Put(m)
}

var measurementPool = sync.Pool{
	New: func() interface{} {
		return &Measurement{
			Tags:    make([]metrics_codec.Tag, 0, 10),
			Indices: make([]int32, 0, 1),
			Values:  make([]float64, 0, 1),
			Fnv32a:  0,
			Sorted:  false,
		}
	},
}
