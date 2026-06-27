package v3

import (
	"fmt"

	"code.byted.org/aiops/metrics_codec"
)

const (
	bodyHeaderMinSize   = 13 // At least 3 int32 and 1 int8
	metricHeaderMinSize = 21 // 3 int32 + 1 int8 + 2 string with length >= 3 * 4 + 1 + 2 * (4 + 0) = 21
)

func DecodeMessage(data []byte, offset int) (*Message, int, error) {
	pos := offset
	messageHeader, readLength, err := DecodeMessageHeader(data, pos)
	if err != nil {
		return nil, 0, err
	}
	pos += readLength
	if messageHeader.body.Offset != int32(pos) {
		return nil, 0, fmt.Errorf("body offset not match %v, %v", messageHeader.body.Offset, pos)
	}
	bodyLen := messageHeader.body.Len

	body, readLength, err := DecodeBody(data, pos, int(bodyLen))
	if err != nil {
		return nil, 0, err
	}
	pos += readLength

	if int(bodyLen) != readLength {
		return nil, 0, fmt.Errorf("body length not match")
	}

	// create a new message
	message := &Message{
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
	message.Header = messageHeader
	message.Body = body
	return message, pos - offset, nil
}

func DecodeMessageHeader(data []byte, offset int) (*MessageHeader, int, error) {
	pos := offset
	magicNumber := int64(metrics_codec.DecodeUint64(data[pos:]))
	if magicNumber != magicCode {
		return nil, 0, metrics_codec.ErrUnknownMagicNumber
	}

	pos += metrics_codec.INT64_LEN
	decodedVersion := int8(metrics_codec.DecodeUint8(data[pos:]))
	if decodedVersion != version {
		return nil, 0, metrics_codec.ErrUnsupportedCodecVersion
	}
	pos += metrics_codec.INT8_LEN

	bodyHeader, readLength, err := DecodeBodyHeader(data, pos)
	if err != nil {
		return nil, 0, metrics_codec.ErrDecodeBodyHeader
	}
	pos += readLength

	ts := int64(metrics_codec.DecodeUint64(data[pos:]))
	pos += metrics_codec.INT64_LEN
	flags := int32(metrics_codec.DecodeUint32(data[pos:]))
	pos += metrics_codec.INT32_LEN
	metricNum := int32(metrics_codec.DecodeUint32(data[pos:]))
	pos += metrics_codec.INT32_LEN

	tenant, readLength, err := metrics_codec.DecodeStr(data, pos)
	if err != nil {
		return nil, 0, err
	}
	pos += readLength
	account, readLength, err := metrics_codec.DecodeStr(data, pos)
	if err != nil {
		return nil, 0, err
	}
	pos += readLength

	project, readLength, err := metrics_codec.DecodeStr(data, pos)
	if err != nil {
		return nil, 0, err
	}
	pos += readLength

	sdkVersion, readLength, err := metrics_codec.DecodeStr(data, pos)
	if err != nil {
		return nil, 0, err
	}
	pos += readLength
	var messageHeader = &MessageHeader{magicCode, version, *bodyHeader, ts, flags, metricNum, tenant, account, project, sdkVersion}

	return messageHeader, pos - offset, nil
}

func DecodeBodyHeader(data []byte, offset int) (*BodyHeader, int, error) {
	if len(data)-offset < bodyHeaderMinSize {
		return nil, 0, fmt.Errorf("no enough bytes for body header")
	}
	pos := offset
	bodyHeader := &BodyHeader{}
	bodyHeader.Offset = int32(metrics_codec.DecodeUint32(data[pos:]))
	pos += metrics_codec.INT32_LEN
	bodyHeader.Len = int32(metrics_codec.DecodeUint32(data[pos:]))
	pos += metrics_codec.INT32_LEN
	bodyHeader.CompressMode = int8(metrics_codec.DecodeUint8(data[pos:]))
	pos += metrics_codec.INT8_LEN
	bodyHeader.CompressLen = int32(metrics_codec.DecodeUint32(data[pos:]))
	pos += metrics_codec.INT32_LEN
	return bodyHeader, pos - offset, nil
}

func DecodeBody(data []byte, offset int, length int) (Body, int, error) {
	pos := offset
	body := Body{}

	for pos < offset+length {
		metric, readLength, err := DecodeMetric(data, pos)
		if err != nil {
			return nil, 0, err
		}
		pos += readLength
		body = append(body, metric)
	}

	if pos != offset+length {
		return nil, 0, fmt.Errorf("body_length_not_match")
	}
	return body, length, nil
}

func DecodeMetric(data []byte, offset int) (*Metric, int, error) {
	pos := offset
	metric := Metric{}
	metricHeader, readLength, numberOfMeasurements, err := DecodeMetricHeader(data, pos)
	if err != nil {
		return nil, 0, err
	}
	pos += readLength
	metric.Header = *metricHeader

	measureMents, readLength, err := DecodeMetricMeasurements(data, pos, numberOfMeasurements, metric.Header.Fields)
	if err != nil {
		return nil, 0, err
	}
	pos += readLength
	metric.Measurements = measureMents
	return &metric, pos - offset, nil
}

// DecodeMetricHeader returns the decoded MetricHeader, readLength, numberOfMeasurers, error.
func DecodeMetricHeader(data []byte, offset int) (*MetricHeader, int, int, error) {
	if len(data)-offset < metricHeaderMinSize {
		return nil, 0, 0, fmt.Errorf("no enough bytes for body header")
	}

	pos := offset
	metricHeader := &MetricHeader{}
	length := int32(metrics_codec.DecodeUint32(data[pos:]))
	pos += metrics_codec.INT32_LEN
	metricHeader.len = length

	numberOfMeasurer := int(metrics_codec.DecodeUint32(data[pos:]))
	pos += metrics_codec.INT32_LEN

	mType := metrics_codec.MType(metrics_codec.DecodeUint8(data[pos:]))
	pos += metrics_codec.INT8_LEN
	metricHeader.MType = mType

	fnv32 := int32(metrics_codec.DecodeUint32(data[pos:]))
	pos += metrics_codec.INT32_LEN
	name, readLength, err := metrics_codec.DecodeStr(data, pos)
	if err != nil {
		return nil, 0, 0, err
	}
	pos += readLength

	if int32(metrics_codec.Fnv32a(name)) != fnv32 {
		return nil, 0, 0, fmt.Errorf("hashcode not match. metric name: %s, type: %v, hashcode: %d, should be: %d",
			name, mType, fnv32, int32(metrics_codec.Fnv32a(name)))
	}

	metricHeader.Name = name
	fieldCount := int(metrics_codec.DecodeUint32(data[pos:]))
	pos += metrics_codec.INT32_LEN

	for i := 0; i < fieldCount; i++ {
		fieldName, readLength, err := metrics_codec.DecodeStr(data, pos)
		if err != nil {
			return nil, 0, 0, err
		}
		pos += readLength
		vType := int8(metrics_codec.DecodeUint8(data[pos:]))
		pos++
		metricHeader.Fields = append(metricHeader.Fields, metrics_codec.Field{Name: fieldName, VType: vType})
	}

	return metricHeader, pos - offset, numberOfMeasurer, nil
}

func DecodeMetricMeasurements(data []byte, offset int, numberOfMeasurements int, fields []metrics_codec.Field) ([]*Measurement, int, error) {
	pos := offset
	measurements := make([]*Measurement, 0, numberOfMeasurements)
	for i := 0; i < numberOfMeasurements; i++ {
		measurement, readLength, err := DecodeMetricMeasurement(data, pos, fields)
		if err != nil {
			return nil, 0, err
		}
		pos += readLength
		measurements = append(measurements, measurement)
	}
	return measurements, pos - offset, nil
}

func DecodeMetricMeasurement(data []byte, offset int, fields []metrics_codec.Field) (*Measurement, int, error) {
	pos := offset
	hashcode2 := metrics_codec.DecodeUint32(data[pos:])
	pos += metrics_codec.INT32_LEN
	mesurement := &Measurement{
		Fnv32a: hashcode2,
	}
	Ts, readLength, err := DecodeMetricMeasurementTs(data, pos)
	if err != nil {
		return nil, 0, err
	}
	pos += readLength
	mesurement.Tags = Ts

	values, readLength, err := DecodeMetricsMeasurementValues(data, pos, fields)
	if err != nil {
		return nil, 0, err
	}
	pos += readLength
	mesurement.Values = values

	return mesurement, pos - offset, nil
}

func DecodeMetricMeasurementTs(data []byte, offset int) ([]metrics_codec.Tag, int, error) {
	pos := offset
	strArray, readLength, err := metrics_codec.DecodeStrArray(data, pos)
	if err != nil {
		return nil, 0, err
	}
	pos += readLength
	Ts, err := metrics_codec.BuildTsFromStrArray(strArray)
	if err != nil {
		return nil, 0, err
	}
	return Ts, pos - offset, nil
}

func DecodeMetricsMeasurementValues(data []byte, offset int, fields []metrics_codec.Field) ([]float64, int, error) {
	pos := offset
	values, readLength, err := metrics_codec.DecodeFloatArrayV3(data, pos, fields)
	if err != nil {
		return nil, 0, err
	}
	pos += readLength
	return values, pos - offset, nil
}
