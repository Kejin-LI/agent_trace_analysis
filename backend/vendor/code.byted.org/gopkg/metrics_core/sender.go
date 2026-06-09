package metrics

import (
	"fmt"
	"io"
	"sync"
	"time"

	"code.byted.org/aiops/metrics_codec"
	v3 "code.byted.org/aiops/metrics_codec/v3"
	v4 "code.byted.org/aiops/metrics_codec/v4"
	"code.byted.org/gopkg/metrics_core/logs"
	"code.byted.org/gopkg/metrics_core/utils"
)

type mType int

type packet []byte

type mTags []metrics_codec.Tag

type mFields struct {
	// for codec-v3, it only has necessary codec name.
	// for codec-v4, it is all codec name.
	fieldNames []metrics_codec.Field

	indices []int32
	values  []float64

	// for sorting v4 indices
	pairs []Pair
}

func (fs *mFields) cleanPairs(num int) {
	if cap(fs.pairs) < num {
		fs.pairs = make([]Pair, num)
	} else {
		fs.pairs = fs.pairs[:num]
		for i := range fs.pairs {
			// fs.pairs[i].contain = false

			// It is faster than the above line, which modifies the fields of pairs
			fs.pairs[i] = Pair{}
		}
	}
}

type Pair struct {
	contain bool
	Value   float64
}

func (p *packet) Recycle() {
	if cap(*p) <= messageRecycleThreshold {
		*p = (*p)[:0]
		packetPool.Put(p)
	}
}

var (
	packetPool = sync.Pool{
		New: func() interface{} {
			p := packet(make([]byte, 0, 128))
			return &p
		},
	}
)

const (
	UnknownType mType = iota
	StoreType
	RateCounterType
	CounterType
	MeterType
	TimerType
	HistogramType
	MultiFieldsType
	DeltaCounterType
)

func (t mType) String() string {
	switch t {
	case CounterType:
		return "counter"
	case TimerType:
		return "timer"
	case StoreType:
		return "store"
	case RateCounterType:
		return "rate_counter"
	case MeterType:
		return "meter"
	case HistogramType:
		return "histogram"
	case MultiFieldsType:
		return "multi_fields"
	case DeltaCounterType:
		return "delta_counter"
	default:
		return "unknown"
	}
}

func (c *cursor) flatPack(sendTime time.Time, messages ...metrics_codec.Message) {
	if len(messages) == 0 {
		return
	}

	var message, lastMessage metrics_codec.Message
	if c.metric.client.withCodecV4 {
		message, lastMessage = messages[0], messages[0]
	} else {
		message, lastMessage = messages[0], messages[1]
	}

	node := c.values.getHead()
	var ok bool

	tagBytes, hashcode := c.tagBytes, c.hashcode
	// The tag bytes is not pre-computed, compute here.
	if len(tagBytes) == 0 {
		// merge global mTags and metric tags.
		allTags := c.metric.newTags(len(c.metric.tags) + len(c.metric.client.globalTags))
		defer c.metric.putTags(allTags)
		for i := range c.index {
			(*allTags)[c.metric.sortedUserTagPos[i]].Key = c.metric.tags[i]
			(*allTags)[c.metric.sortedUserTagPos[i]].Value = c.index[i]
		}

		for i := range c.metric.client.globalTags {
			(*allTags)[c.metric.sortedGlobalTagPos[i]] = c.metric.client.globalTags[i]
		}

		// compute and store the hashcode.
		if hashcode == 0 {
			hashcode = int32(utils.Fnv32aWithIndices(*allTags))
			c.hashcode = hashcode
		}

		// tagbytes is empty, we need to generate it here.
		p := c.metric.newPacket()
		defer c.metric.putPacket(p)
		*p = metrics_codec.Tags(*allTags).Encode(*p)
		tagBytes = *p
		// If the client enables TagCache, we save the tagBytes.
		if c.metric.client.cacheTags {
			c.tagBytes = append(c.tagBytes, tagBytes...)
		}
	}

	// merge all nodes for customized multi-field metrics.
	if c.metric.forceMultiFields {
		metricName := c.metric.name
		// This is a purely multi-field metric
		fields := c.metric.newFields()
		// for multi-field counter initial values
		lastCounterFields := c.metric.newFields()
		defer c.metric.putFields(fields)
		defer c.metric.putFields(lastCounterFields)

		// codec-v4 uses indices to indicate whether a filed has value.
		if c.metric.client.withCodecV4 {
			fields.fieldNames = append(fields.fieldNames, c.metric.fields.getAllCodecFields()...)
			lastCounterFields.fieldNames = append(lastCounterFields.fieldNames, c.metric.fields.getAllCodecFields()...)
		}

		for {
			node, ok = node.getNext()
			if !ok {
				break
			}

			vs := node.marshal()
			index := -1
			for _, v := range vs {
				if v.mType == TimerType && c.metric.timerOps.compatTimer {
					panic(fmt.Sprintf("a multi-field but with a compat timer, which should no happen: %s", v.suffix))
				}

				index = c.metric.fields.indexOf(v.suffix, v.mType)
				if index == -1 {
					logs.Error("metric %s is a user-defined multi-field metric, "+
						"but found undefined metric with suffix %s of type %s,",
						c.metric.name, v.suffix, v.mType.String())
					return
				}

				// codec-v3 use field names in metric header to indicate whether a field has value.
				if !c.metric.client.withCodecV4 {
					fields.fieldNames = append(fields.fieldNames, c.metric.fields.fields[index].meta.codecFields...)
				}

				actualIndex := c.metric.fields.indexOfActualField(v.suffix, v.mType)
				for i, val := range v.values {
					fields.values = append(fields.values, val)
					fields.indices = append(fields.indices, int32(actualIndex+i))
				}
			}

			if len(vs) > 0 {
				// only counter and delta_counter may have last values.
				// if the reservoir also has new values, append the last values to lastCounterFields
				// add initial values for multi-field metrics' counter fields.
				if c.metric.client.reportInitialValue && node.getReservoir().reportLastValues() {
					lastValues := node.getReservoir().getLastValues()

					for _, v := range lastValues {
						index := c.metric.fields.indexOf(v.suffix, v.mType)
						if index == -1 {
							logs.Error("metric %s is a user-defined multi-field metric, "+
								"but found undefined metric with suffix %s of type %s,",
								c.metric.name, v.suffix, v.mType.String())
							return
						}

						// codec-v3 stores field names which has values in metric header.
						if !c.metric.client.withCodecV4 {
							lastCounterFields.fieldNames = append(lastCounterFields.fieldNames,
								c.metric.fields.fields[index].meta.codecFields...)
						}

						actualIndex := c.metric.fields.indexOfActualField(v.suffix, v.mType)
						for i, val := range v.values {
							lastCounterFields.values = append(lastCounterFields.values, val)
							lastCounterFields.indices = append(lastCounterFields.indices, int32(actualIndex+i))
						}
					}
				}
			}
		}

		// message's nocheck flag is activated here, no need to check the error.
		if len(fields.indices) != len(fields.values) {
			logs.Error("field count not match: %v vs %v", fields.indices, fields.values)
		} else {

			if len(fields.values) > 0 {
				// compose the message for initial values of counter fields in multi-field metric
				// if the value slice is not empty.
				needToReportLastValues := len(lastCounterFields.values) > 0
				if needToReportLastValues {
					lastSendTime := sendTime.Add(time.Duration(-c.metric.client.timeIntervalSec * int(time.Second)))
					c.metric.appendMetricAndSeries(c.metric.client.withCodecV4, needToReportLastValues, lastSendTime,
						lastMessage, metricName, MultiFieldsType, hashcode, tagBytes, lastCounterFields)
				}

				c.metric.appendMetricAndSeries(c.metric.client.withCodecV4, needToReportLastValues, sendTime, message,
					metricName, MultiFieldsType, hashcode, tagBytes, fields)
			} else {
				logs.Debug("metric: %v has empty fields", c.metric.name)
			}
		}

	} else {
		// This metric is not declared as a multi-field metric
		// So we should create metrics_codec.Metric for each node.

		for {
			node, ok = node.getNext()
			if !ok {
				break
			}

			vs := node.marshal()

			// only report old value when the counter is updated.
			if len(vs) > 0 {
				// add initial values for counter and delta_counter metric
				var lastValues []value
				if c.metric.client.reportInitialValue {
					if node.getReservoir().reportLastValues() {
						lastValues = node.getReservoir().getLastValues() // make([]float64, len(v.values))
					}
				}

				lastSendTime := sendTime.Add(time.Duration(-c.metric.client.timeIntervalSec * int(time.Second)))
				for _, v := range lastValues {
					metricName := c.metric.name
					if v.suffix != "" {
						metricName += "." + v.suffix
					}
					// delta counter is a multi-field metric
					var fields []metrics_codec.Field
					if v.mType == DeltaCounterType {
						fields = c.metric.fields.getDeltaFields(v.suffix, false)
					}
					lastCounterFields := c.metric.newFields()
					lastCounterFields.fieldNames = append(lastCounterFields.fieldNames, fields...)
					lastCounterFields.values = append(lastCounterFields.values, v.values...)
					c.metric.appendMetricAndSeries(
						c.metric.client.withCodecV4,
						true,
						lastSendTime, lastMessage,
						metricName, v.mType,
						hashcode, tagBytes, lastCounterFields)
					c.metric.putFields(lastCounterFields)
				}
			}

			for _, v := range vs {
				if v.mType == MultiFieldsType { // should not happen
					logs.Error("invalid data %v", v)
					continue
				}

				metricName := c.metric.name
				if v.suffix != "" {
					metricName += "." + v.suffix
				}

				// for HistogramType
				var fields []metrics_codec.Field
				switch v.mType {
				case HistogramType:
					fields = c.metric.fields.getHistogramFields(v.suffix)
					if c.metric.client.sdk.sdkConfig.DebugMode {
						logs.Debug("The fields of histogram %s are %v", c.metric.name, fields)
					}
				case TimerType:
					if !c.metric.timerOps.compatTimer {
						fields = c.metric.fields.getTimerFields(v.suffix, false)
					}
				case DeltaCounterType:
					fields = c.metric.fields.getDeltaFields(v.suffix, false)
				}

				mFields := c.metric.newFields()
				mFields.fieldNames = append(mFields.fieldNames, fields...)
				mFields.values = append(mFields.values, v.values...)
				c.metric.appendMetricAndSeries(c.metric.client.withCodecV4, true, sendTime, message,
					metricName, v.mType, hashcode, tagBytes, mFields)
				c.metric.putFields(mFields)
			}

		}

	}
}

type sender struct {
	w   io.WriteCloser
	cli *client
}

func (s *sender) close() {
	_ = s.w.Close()
}

const retryCount = 3

func (s *sender) emit(p packet, metricCount int, seriesCount int) int64 {
	startTime := time.Now()
	var err error
	for i := 0; i < retryCount; i++ {
		_, err = s.w.Write(p)

		if err == nil {
			break
		}
		time.Sleep(time.Millisecond * 10)
	}

	if err != nil {
		if s.cli.sdk.sdkConfig.SelfMonitor.Enabled {
			if s.cli.sdk.sdkConfig.SelfMonitor.Enabled {
				if !s.cli.isMonitorClient {
					emitAndLog(s.cli.sdk.senderFailMetric.WithTagValues(s.cli.tenant, err.Error()),
						WithSuffix("").Incr(1))
					emitAndLog(s.cli.sdk.senderMetric.WithTagValues(s.cli.tenant, "drop"),
						WithField("bytes").Incr(len(p)),
						WithField("metrics").Incr(metricCount),
						WithField("series").Incr(seriesCount),
					)
				}
			}
		}
		logs.Error("metrics write message error: %s", err)
	}

	if s.cli.sdk.sdkConfig.SelfMonitor.Enabled {
		if !s.cli.isMonitorClient {
			emitAndLog(s.cli.sdk.senderMetric.WithTagValues(s.cli.tenant, "send"),
				WithField("bytes").Incr(len(p)),
				WithField("metrics").Incr(metricCount),
				WithField("series").Incr(seriesCount),
			)
		}
	}
	return time.Now().Sub(startTime).Microseconds()
}

func (m *metric) appendMetricAndSeries(useV4, needAppendMetricHeader bool,
	sendTime time.Time, message metrics_codec.Message, metricName string, metricType mType,
	hashcode int32, tagBytes []byte, fields *mFields) {

	// The message is not appended a metric header or it has to append a metric header with different timestamp
	// For codec-v3, we always need to append the metric header first even the metric is multi-field.
	if !useV4 || (needAppendMetricHeader || !message.IsSeriesAppendable()) {
		err := message.AppendMetric(metricName, metrics_codec.MType(metricType), fields.fieldNames, sendTime)

		if err != nil {
			logs.Warn("Sender for Client: %s of Tenant: %s failed to append metric header: %s due to %v",
				m.client.prefix, m.client.tenant, metricName, err)
			return
		}
	}

	var err error
	if useV4 {
		// sort the indices for multi field metric in codec-v4
		if len(fields.indices) > 0 {
			fields.cleanPairs(len(fields.fieldNames))

			// Following sorting is faster than sort.Sort.
			for i, v := range fields.indices {
				fields.pairs[v].contain, fields.pairs[v].Value = true, fields.values[i]
			}

			fields.indices = fields.indices[:0]
			fields.values = fields.values[:0]
			for i, v := range fields.pairs {
				if v.contain {
					fields.indices = append(fields.indices, int32(i))
					fields.values = append(fields.values, v.Value)
				}
			}
		}
		err = message.AppendMeasurement(hashcode, tagBytes, fields.indices, fields.values...)
	} else {
		err = message.AppendMeasurement(hashcode, tagBytes, fields.indices, fields.values...)
	}

	if err != nil {
		logs.Warn("Sender for Client: %s of Tenant: %s failed to append series: %s due to %v",
			m.client.prefix, m.client.tenant, err)
		return
	}

	checkMessages([]metrics_codec.Message{message}, m)
}

// packer packs and sends metrics bound to it.
// a metric is bound to the packer if its packerID equals to the packer's id.
type packer struct {
	id int
	c  *client

	// messages with higher indices have smaller timestamp
	messages []metrics_codec.Message
}

func (client *client) newPacker(id int) *packer {
	p := &packer{
		id: id,
		c:  client,
	}

	if client.withCodecV4 {
		p.messages = append(p.messages,
			v4.NewFlatMessage(v4.WithProperties(client.messageProperties...), v4.SetSkipCheck(), v4.WithMergeSeries()))
	} else {
		p.messages = append(p.messages,
			v3.NewFlatMessage(v3.WithProperties(client.messageProperties...), v3.SetSkipCheck(), v3.WithMergeSeries()),
			v3.NewFlatMessage(v3.WithProperties(client.messageProperties...), v3.SetSkipCheck(), v3.WithMergeSeries()),
		)
	}
	return p
}

// packMetrics packs the metrics bound to this packer.
// Note that the client is locked during this method.
func (p *packer) packMetrics(sendTime time.Time) {
	// Set timestamp for each message to send.
	for i, m := range p.messages {
		sendTime := sendTime.Add(-time.Duration(i*p.c.timeIntervalSec) * time.Second)
		_ = m.SetTimestamp(sendTime)
	}

	for _, ms := range p.c.metrics {
		for _, m := range ms {
			// Each packer only send the metrics bound to it.
			if p.id == m.packerID {
				// Need to append the metric header when switching to a new metric.
				for _, m := range p.messages {
					m.SetNeedAppendMetricNameFirst(true)
				}
				m.matrix.Pack(sendTime, p.messages...)
			}
		}
	}

	// Send from the end to the start since messages with higher indices have smaller timestamps.
	for i := len(p.messages) - 1; i >= 0; i-- {
		m := p.messages[i]
		sendMessage(p.c, m)
		m.Clear()
	}
}
