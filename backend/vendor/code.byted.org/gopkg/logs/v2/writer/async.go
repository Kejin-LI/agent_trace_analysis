package writer

import (
	"fmt"
	"os"
	"sync"
)

// AsyncWriter provides a asynchronous wrapper to another writer,
// it is useful to wrap another blocking writer like FileWriter,
// to the side chain and avoid some overheads in user thread.
type AsyncWriter struct {
	LogWriter
	done    *sync.WaitGroup
	ch      chan RecyclableLog
	flush   chan bool
	flushed chan error
	omit    bool
}

// NewAsyncWriter creates a AsyncWriter,
// omit allows AsyncWriter omits the log if the buffer is full or not.
func NewAsyncWriter(w LogWriter, omit bool) LogWriter {
	asyncWriter := &AsyncWriter{
		LogWriter: w,
		done:      &sync.WaitGroup{},
		ch:        make(chan RecyclableLog, 1024),
		flush:     make(chan bool),
		flushed:   make(chan error),
		omit:      omit,
	}
	go asyncWriter.runWorker()
	return asyncWriter
}

// NewAsyncWriterWithChanSzie creates a AsyncWriter,
// chanLen is the length of ch,
// omit allows AsyncWriter omits the log if the buffer is full or not.
func NewAsyncWriterWithChanLen(w LogWriter, chanLen int, omit bool) LogWriter {
	asyncWriter := &AsyncWriter{
		LogWriter: w,
		done:      &sync.WaitGroup{},
		ch:        make(chan RecyclableLog, chanLen),
		flush:     make(chan bool),
		flushed:   make(chan error),
		omit:      omit,
	}
	go asyncWriter.runWorker()
	return asyncWriter
}

func (w *AsyncWriter) runWorker() {
	for {
		select {
		case log, ok := <-w.ch:
			if !ok {
				return
			}
			err := w.LogWriter.Write(log)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "log async writes error: %s\n", err)
			}
			w.done.Done()
		case <-w.flush:
			for i := 0; i < len(w.ch); i++ {
				log := <-w.ch
				err := w.LogWriter.Write(log)
				if err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "log async writes error: %s\n", err)
				}
				w.done.Done()
			}
			w.flushed <- w.LogWriter.Flush()
		}
	}
}

func (w *AsyncWriter) Write(log RecyclableLog) error {
	w.done.Add(1)
	if w.omit {
		select {
		case w.ch <- log:
		default:
			w.done.Done()
			log.Recycle()
		}
	} else {
		w.ch <- log
	}
	return nil
}

func (w *AsyncWriter) Flush() error {
	w.flush <- true
	return <-w.flushed
}

func (w *AsyncWriter) Close() error {
	close(w.ch)
	w.done.Wait()
	return w.LogWriter.Close()
}
