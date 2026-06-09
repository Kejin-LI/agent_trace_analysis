package writers

import (
	"io"
	"os"
	"sync"
)

type packet []byte

var (
	packetPool = sync.Pool{
		New: func() interface{} {
			p := packet(make([]byte, 0, 128))
			return &p
		},
	}
)

func (p *packet) Recycle() {
	if cap(*p) <= 1<<16 {
		*p = (*p)[:0]
		packetPool.Put(p)
	}
}

type ConsoleWriterOption func(w *ConsoleWriter)

func WithPrefix(prefix string) ConsoleWriterOption {
	return func(w *ConsoleWriter) {
		w.prefix = []byte(prefix)
	}
}

type ConsoleWriter struct {
	prefix []byte
	io.Writer
}

func NewConsoleWriter(ops ...ConsoleWriterOption) *ConsoleWriter {
	w := &ConsoleWriter{
		prefix: nil,
		Writer: os.Stdout,
	}
	for _, op := range ops {
		op(w)
	}
	return w
}

func (w *ConsoleWriter) Write(bytes []byte) (int, error) {
	if len(w.prefix) == 0 {
		if len(bytes) > 0 && bytes[len(bytes)-1] != '\n' {
			bytes = append(bytes, '\n')
		}
		return w.Writer.Write(bytes)
	}

	p := packetPool.Get().(*packet)
	*p = (*p)[:0]
	defer p.Recycle()
	*p = append(*p, w.prefix...)
	*p = append(*p, bytes...)
	if len(*p) > 0 && (*p)[len(*p)-1] != '\n' {
		*p = append(*p, '\n')
	}
	return w.Writer.Write(*p)
}
