package metrics

import (
	"io"

	"code.byted.org/gopkg/env"
	"code.byted.org/gopkg/logs"
	m "code.byted.org/gopkg/metrics/v3"
)

type config struct {
	Prefix         *string      `yaml:"Prefix"`
	GlobalTags     []GlobalTag  `yaml:"GlobalTags,omitempty"`
	GlobalKeys     []string     `yaml:"GlobalKeys,omitempty"`
	Metrics        []MetricInfo `yaml:"Metrics"`
	IgnoreEmptyTag bool         `yaml:"IgnoreEmptyTag"`
	UnsetTceTags   bool         `yaml:"UnsetTceTags"`
	globalTags     []m.T

	//v3 option
	sender           io.WriteCloser
	collectorFactory m.Collector
	HistogramTimer   bool `yaml:"HistogramTimer"`
	HighestWaterMark int  `yaml:"HighestWaterMark"`
	TimerBuf         *int `yaml:"TimerBuf"`
}
type GlobalTag struct {
	Name  string `yaml:"Name"`
	Value string `yaml:"Value"`
}

type MetricInfo struct {
	Name string   `yaml:"Name"`
	Tags []string `yaml:"Tags"`
}

func newConfig() *config {
	return &config{
		GlobalTags: []GlobalTag{
			{
				Name:  "host",
				Value: env.HostIP(),
			},
		},
	}
}

func (c *config) getPrefix() string {
	if c.Prefix == nil {
		return ""
	}
	return *c.Prefix
}
func (c *config) validate() {
	if c.Prefix == nil {
		c.Prefix = ptrTo(env.PSM())
	}

	for _, t := range c.GlobalTags {
		if t.Value == "" {
			logs.Warnf("skip tag key:%v value is empty", t.Name)
			continue
		}
		c.globalTags = append(c.globalTags, m.T{
			Name:  t.Name,
			Value: t.Value,
		})
	}
}

func (c *config) options() []m.ClientOption {
	ops := []m.ClientOption{
		m.SetGlobalTags(c.globalTags...),
	}

	if !c.UnsetTceTags {
		ops = append(ops, m.SetTceTags())
	}
	if c.HistogramTimer {
		ops = append(ops, m.SetHistogramTimer())
	}
	if c.HighestWaterMark > 0 {
		ops = append(ops, m.SetHighestWaterMark(c.HighestWaterMark))
	}
	if c.TimerBuf != nil {
		ops = append(ops, m.SetTimerBufSize(*c.TimerBuf))
	}
	if c.collectorFactory != nil {
		ops = append(ops, m.SetCollector(c.collectorFactory))
	}
	if c.sender != nil {
		ops = append(ops, m.SetWriter(c.sender))
	}

	return ops
}
