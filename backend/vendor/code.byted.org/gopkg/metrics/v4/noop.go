package metrics

import (
	core "code.byted.org/gopkg/metrics_core"
)

// NewNoopClient creates a noop client without any features,
// it is useful in test cases.
func NewNoopClient() Client {
	return core.NewNoopClient()
}

type noopWriter struct{}

func (w *noopWriter) Write(b []byte) (int, error) {
	return len(b), nil
}

func (w *noopWriter) Close() error {
	return nil
}
