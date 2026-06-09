package v3

import (
	"fmt"
	"os"
	"sort"
	"sync"

	"code.byted.org/aiops/metrics_codec"
)

type Metric struct {
	Header       MetricHeader
	Measurements []*Measurement
}

func NewMetric(metricName string, mType metrics_codec.MType, fields []metrics_codec.Field) *Metric {
	return &Metric{
		Header: MetricHeader{
			MType:  mType,
			Name:   metricName,
			Fields: fields,
			len:    0,
			offset: 0,
		},
		Measurements: nil,
	}
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
	MType  metrics_codec.MType
	Name   string
	Fields []metrics_codec.Field

	len    int32
	offset int
}

// metricHeaderSize
func metricHeaderSize(metricName string, _ metrics_codec.MType, fields []metrics_codec.Field) int {
	length := metrics_codec.INT32_LEN + metrics_codec.INT32_LEN + metrics_codec.INT8_LEN +
		metrics_codec.INT32_LEN + metrics_codec.SizeOfVStr(metricName) + metrics_codec.INT32_LEN
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
	Tags   []metrics_codec.Tag
	Values []float64
	Fnv32a uint32
	Sorted bool
}

func NewMeasurement() *Measurement {
	m, ok := measurementPool.Get().(*Measurement)
	if !ok {
		return nil
	}
	m.Tags = m.Tags[:0]
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
			Tags:   make([]metrics_codec.Tag, 0, 10),
			Values: make([]float64, 0, 1),
			Fnv32a: 0,
			Sorted: false,
		}
	},
}
