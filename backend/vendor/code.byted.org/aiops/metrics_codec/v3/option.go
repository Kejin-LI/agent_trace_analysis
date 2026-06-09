package v3

import (
	"fmt"

	"code.byted.org/aiops/metrics_codec"
)

type MessageOption func(m *FlatMessage) error

func SetSkipCheck() MessageOption {
	return func(m *FlatMessage) error {
		m.noCheck = true
		return nil
	}
}

func WithTenant(tenant string) MessageOption {
	return func(m *FlatMessage) error {
		return m.SetTenant(tenant)
	}
}

func WithDC(dc string) MessageOption {
	return func(m *FlatMessage) error {
		if metrics_codec.IsValidTagValue(dc) {
			m.dc = dc
			return nil
		}
		return metrics_codec.ErrInvalidString
	}
}

func WithHost(host string) MessageOption {
	return func(m *FlatMessage) error {
		if metrics_codec.IsValidTagValue(host) {
			m.host = host
			return nil
		}
		return metrics_codec.ErrInvalidString
	}
}

func WithHeader(header *MessageHeader) MessageOption {
	return func(m *FlatMessage) error {
		if header != nil {
			m.SetMessageHeader(header)
			return nil
		}
		return fmt.Errorf("empty message header")
	}
}

func WithProperties(properties ...string) MessageOption {
	return func(m *FlatMessage) error {
		return m.SetProperties(properties...)
	}
}

func WithFlag(flag int32) MessageOption {
	return func(m *FlatMessage) error {
		return m.SetFlags(flag)
	}
}

func WithMergeSeries() MessageOption {
	return func(m *FlatMessage) error {
		m.isSeriesMergeable = true
		return nil
	}
}
