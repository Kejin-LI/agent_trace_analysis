package writer

// NoopWriter does nothing in any cases, it is useful in writing tests.
type NoopWriter struct{}

func (w *NoopWriter) Write(l RecyclableLog) error {
	defer l.Recycle()
	return nil
}

func (w *NoopWriter) Close() error { return nil }

func (w *NoopWriter) Flush() error { return nil }
