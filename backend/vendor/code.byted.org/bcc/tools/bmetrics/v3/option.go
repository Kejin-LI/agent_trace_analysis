package metrics

import (
	"io"

	m "code.byted.org/gopkg/metrics/v3"
)

type Option = func(config *config)

func IgnoreEmptyTag() Option {
	return func(config *config) {
		config.IgnoreEmptyTag = true
	}
}

func UnsetTceTags() Option {
	return func(config *config) {
		config.UnsetTceTags = true
	}
}

func WithPrefix(prefix string) Option {
	return func(config *config) {
		config.Prefix = &prefix
	}
}

func WithGlobalTag(k, v string) Option {
	return func(config *config) {
		config.globalTags = append(config.globalTags, m.T{Name: k, Value: v})
	}
}

func WithMetric(name string, tags ...string) Option {
	return func(config *config) {
		config.Metrics = append(config.Metrics, MetricInfo{
			Name: name,
			Tags: tags,
		})
	}
}

func WithMetrics(mcs map[string][]string) Option {
	return func(config *config) {
		for name, tags := range mcs {
			config.Metrics = append(config.Metrics, MetricInfo{
				Name: name,
				Tags: tags,
			})
		}
	}
}

func WithMapCollector() Option {
	return func(config *config) {
		config.collectorFactory = m.Map
	}
}

func SetGlobalKeys(key ...string) Option {
	return func(config *config) {
		config.GlobalKeys = append(config.GlobalKeys, key...)
	}
}

// SetWriter allows users to set their custom writer,
// writer should be synchronous,
// the default writer is metrics agent writer.
func SetWriter(w io.WriteCloser) Option {
	return func(c *config) {
		c.sender = w
	}
}
