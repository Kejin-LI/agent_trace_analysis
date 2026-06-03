package metrics

import (
	"errors"
)

// NewNoopClient creates a noop client without any features,
// it is useful in test cases.
func NewNoopClient() Client {
	return &noopClient{}
}

type noopClient struct{}

type noopMetric struct{}

type noopCursor struct{}

func (c *noopCursor) Emit(values ...*Value) error {
	return nil
}

func (c *noopCursor) Emit1(value *Value, values ...*Value) error {
	return nil
}

func (c *noopCursor) Emit2(_ *Value, _ *Value, _ ...*Value) error {
	return nil
}

func (c *noopClient) NewMetric(metricName string, tagNames ...string) Metric {
	return &noopMetric{}
}

func (c *noopClient) Close() {}

func (c *noopClient) Flush() {}

func (m *noopMetric) WithTags(tags ...T) Emitter {
	return &noopCursor{}
}

func (m *noopMetric) WithTagValues(_ ...string) Emitter {
	return &noopCursor{}
}

func (m *noopMetric) WithTagIndex(index TagIndex) Emitter {
	return &noopCursor{}
}

func (m *noopMetric) NewTagIndex() TagIndex {
	s := make([]string, 128)
	return &s
}

func (m *noopMetric) AppendTag(index TagIndex, name, value string) {}

func (m *noopMetric) Close() {}

func (m *noopMetric) Flush() {}

type noopWriter struct{}

func (w *noopWriter) Write(b []byte) (int, error) {
	return len(b), nil
}

func (w *noopWriter) Close() error {
	return nil
}

type errorWriter struct{}

func (w *errorWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("socket does not init")
}

func (w *errorWriter) Close() error {
	return nil
}
