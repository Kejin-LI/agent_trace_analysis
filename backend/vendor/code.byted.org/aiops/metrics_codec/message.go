package metrics_codec

import "time"

// Message is a batch of metrics and series
type Message interface {
	Size() int
	GetFlags() int32
	GetMetricCount() int
	GetSeriesCount() int
	GetProject() string
	GetTimestamp() time.Time
	IsSeriesAppendable() bool
	GetGatewayTimestamp() time.Time
	GetClientTimestamp() time.Time

	SetClientTimestamp(ts time.Time) error
	SetGatewayTimestamp(ts time.Time) error
	SetTimestamp(timestamp time.Time) error
	SetFlags(flag int32) error
	SetTenant(tenant string) error
	SetDcHost(dc string, host string) error
	SetProperties(properties ...string) error
	SetNeedAppendMetricNameFirst(need bool)

	Encode(buf []byte) ([]byte, error)
	AppendMetric(metricName string, metricType MType, fields []Field, ts time.Time) error
	AppendMeasurement(hashcode int32, tags FlatStructure, fieldIndices []int32, values ...float64) error
	AppendRawMeasurement(tags []Tag, tagsAreSorted bool, fieldIndices []int32, values ...float64) error

	Clear()
	Reset()
	Recycle()
}

type FlatMetrics interface {
	AppendMetric(metricName string, metricType MType, fields []Field, ts time.Time) error
	AppendMeasurement(hashcode int32, tags FlatStructure, fieldIndices []int32, values ...float64) error

	Size() int
	GetMetricCount() int
	GetSeriesCount() int
	SetNeedAppendMetricNameFirst(need bool)
	IsSeriesAppendable() bool

	Encode(buffer []byte) []byte
	Clear()
	Reset()
}
