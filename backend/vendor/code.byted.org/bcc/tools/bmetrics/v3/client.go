package metrics

import (
	"fmt"
	"sync/atomic"
	"time"

	"code.byted.org/gopkg/logs"
	m "code.byted.org/gopkg/metrics/v3"
)

type MetricsClient struct {
	*config
	client  m.Client
	metricM map[string]m.Metric
	errMax  int64 //错误最多打印次数
	errNow  int64 //累计错误数
}

func NewMetricsClient(ops ...Option) *MetricsClient {
	conf := newConfig()
	for _, op := range ops {
		op(conf)
	}

	conf.validate()
	cli := m.NewClient(
		conf.getPrefix(),
		conf.options()...,
	)

	//初始化metric
	metricM := make(map[string]m.Metric, len(conf.Metrics))
	for _, mc := range conf.Metrics {
		tags := make([]string, len(mc.Tags))
		copy(tags, mc.Tags)
		tags = append(tags, conf.GlobalKeys...)

		metricM[mc.Name] = cli.NewMetric(mc.Name, tags...)
	}

	mc := &MetricsClient{
		config:  conf,
		client:  cli,
		metricM: metricM,
		errMax:  100,
		errNow:  0,
	}

	return mc
}

func (c *MetricsClient) Flush() {
	c.client.Flush()
}
func (c *MetricsClient) EmitCounter(name string, value int, tags ...m.T) {

	nts, err := c.doTagCheck(name, tags)
	if err != nil {
		c.recordError(name, err)
		return
	}

	err = c.metricM[name].WithTags(nts...).Emit(m.WithSuffix("").IncrCounter(value))
	if err != nil {
		c.recordError(name, err)
	}
}
func (c *MetricsClient) EmitRateCounter(name string, value int, tags ...m.T) {
	nts, err := c.doTagCheck(name, tags)
	if err != nil {
		c.recordError(name, err)
		return
	}

	err = c.metricM[name].WithTags(nts...).Emit(m.WithSuffix("").Incr(value))
	if err != nil {
		c.recordError(name, err)
	}
}
func (c *MetricsClient) EmitMeter(name string, value int, tags ...m.T) {
	nts, err := c.doTagCheck(name, tags)
	if err != nil {
		c.recordError(name, err)
		return
	}

	err = c.metricM[name].WithTags(nts...).Emit(m.WithSuffix("").IncrMeter(value))
	if err != nil {
		c.recordError(name, err)
	}
}
func (c *MetricsClient) EmitTimer(name string, value interface{}, tags ...m.T) {
	nts, err := c.doTagCheck(name, tags)
	if err != nil {
		c.recordError(name, err)
		return
	}

	if t0, ok := value.(time.Time); ok { //特殊转换为微妙
		value = int(time.Since(t0).Nanoseconds() / 1000)
	}
	var realValue int
	var ok bool
	if realValue, ok = value.(int); !ok {
		c.recordError(name, fmt.Errorf("value type:%T , value:%v is not int", value, value))
		return
	}
	err = c.metricM[name].WithTags(nts...).Emit(m.WithSuffix("").Observe(realValue))
	if err != nil {
		c.recordError(name, err)
	}
}
func (c *MetricsClient) EmitStore(name string, value int, tags ...m.T) {
	nts, err := c.doTagCheck(name, tags)
	if err != nil {
		c.recordError(name, err)
		return
	}

	err = c.metricM[name].WithTags(nts...).Emit(m.WithSuffix("").Store(value))
	if err != nil {
		c.recordError(name, err)
	}
	if err != nil {
		c.recordError(name, err)
	}
}

func (c *MetricsClient) EmitCounterWithSuffix(name string, suffix string, value int, tags ...m.T) {

	nts, err := c.doTagCheck(name, tags)
	if err != nil {
		c.recordError(name, err)
		return
	}

	err = c.metricM[name].WithTags(nts...).Emit(m.WithSuffix(suffix).IncrCounter(value))
	if err != nil {
		c.recordError(name, err)
	}
}
func (c *MetricsClient) EmitRateCounterWithSuffix(name string, suffix string, value int, tags ...m.T) {
	nts, err := c.doTagCheck(name, tags)
	if err != nil {
		c.recordError(name, err)
		return
	}

	err = c.metricM[name].WithTags(nts...).Emit(m.WithSuffix(suffix).Incr(value))
	if err != nil {
		c.recordError(name, err)
	}
}
func (c *MetricsClient) EmitMeterWithSuffix(name string, suffix string, value int, tags ...m.T) {
	nts, err := c.doTagCheck(name, tags)
	if err != nil {
		c.recordError(name, err)
		return
	}

	err = c.metricM[name].WithTags(nts...).Emit(m.WithSuffix(suffix).IncrMeter(value))
	if err != nil {
		c.recordError(name, err)
	}
}
func (c *MetricsClient) EmitTimerWithSuffix(name string, suffix string, value interface{}, tags ...m.T) {
	nts, err := c.doTagCheck(name, tags)
	if err != nil {
		c.recordError(name, err)
		return
	}

	if t0, ok := value.(time.Time); ok { //特殊转换为微妙
		value = int(time.Since(t0).Nanoseconds() / 1000)
	}
	var realValue int
	var ok bool
	if realValue, ok = value.(int); !ok {
		c.recordError(name, fmt.Errorf("value %T , %v is not int", value, value))
		return
	}
	err = c.metricM[name].WithTags(nts...).Emit(m.WithSuffix(suffix).Observe(realValue))
	if err != nil {
		c.recordError(name, err)
	}
}
func (c *MetricsClient) EmitStoreWithSuffix(name string, suffix string, value int, tags ...m.T) {
	nts, err := c.doTagCheck(name, tags)
	if err != nil {
		c.recordError(name, err)
		return
	}

	err = c.metricM[name].WithTags(nts...).Emit(m.WithSuffix(suffix).Store(value))
	if err != nil {
		c.recordError(name, err)
	}
	if err != nil {
		c.recordError(name, err)
	}
}

func (c *MetricsClient) doTagCheck(name string, tags []m.T) ([]m.T, error) {
	if _, ok := c.metricM[name]; !ok {
		logs.Error("metrics %v, need define before emit", name)
		return tags, fmt.Errorf("metrics %v, need define before emit", name)
	}
	if c.IgnoreEmptyTag {
		head, tail := 0, len(tags)-1
		for head <= tail {
			if tags[head].Value != "" {
				head++
				continue
			}
			if tags[tail].Value == "" {
				tail--
				continue
			}
			tags[head] = tags[tail]
			tail--
		}

		return tags[:head], nil
	}
	return tags, nil
}

func (c *MetricsClient) recordError(name string, err error) {
	if err == nil {
		return
	}
	var prefix string
	prefix = c.getPrefix()
	if c.getPrefix() == "" {
		prefix = "-"
	}
	if name == "" {
		name = "-"
	}
	count := atomic.AddInt64(&c.errNow, int64(1))
	if count <= c.errMax {
		logs.Info("metrics prefix:%v name=%v err=%v", prefix, name, err)
	}
}
