package writer

import "io"

type noopWriter struct{}

func NewNoopWriter() io.WriteCloser {
	return &noopWriter{}
}

func (w *noopWriter) Write(b []byte) (int, error) {
	return len(b), nil
}

func (w *noopWriter) Close() error {
	return nil
}
