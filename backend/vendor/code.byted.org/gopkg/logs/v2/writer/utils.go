package writer

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	osTime "time"
	"unsafe"

	"golang.org/x/time/rate"
)

type syncWriter struct {
	*bufio.Writer
	sync.Mutex
}

func newSyncWriter(w io.Writer) *syncWriter {
	return &syncWriter{
		Writer: bufio.NewWriterSize(w, 8*1024),
	}
}

type rotatedFile struct {
	w *syncWriter
	sync.WaitGroup
	done chan bool
}

func newRotatedFile(file io.WriteCloser) *rotatedFile {
	f := &rotatedFile{
		newSyncWriter(file),
		sync.WaitGroup{},
		make(chan bool),
	}
	f.Add(1)
	ticker := osTime.NewTicker(5 * osTime.Second)
	go func() {
		for {
			select {
			case <-f.done:
				ticker.Stop()
				_ = f.Flush()
				f.Done()
				return
			case <-ticker.C:
				err := f.Flush()
				if err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "log writes file error: %s", err)
				}
			}
		}
	}()
	return f
}

func (f *rotatedFile) Close() error {
	f.done <- true
	f.Wait()
	return nil
}

func (f *rotatedFile) Rotate(w io.WriteCloser) {
	f.w.Lock()
	defer f.w.Unlock()
	_ = f.w.Flush()
	f.w.Reset(w)
}

func (f *rotatedFile) Flush() error {
	var err error
	f.w.Lock()
	defer f.w.Unlock()
	err = f.w.Writer.Flush()
	if err != nil {
		return err
	}
	return nil
}

func (f *rotatedFile) Write(c []byte) (int, error) {
	f.w.Lock()
	defer f.w.Unlock()
	if f.w.Buffered()+len(c) > 4096 {
		_ = f.w.Flush()
	}
	n, err := f.w.Write(c)
	if err != nil {
		return n, err
	}
	if len(c) == 0 || c[len(c)-1] != '\n' {
		_, _ = f.w.Write([]byte{'\n'})
	}
	return n + 1, nil
}

func logIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return "-"
	}
	val := ctx.Value("K_LOGID")
	if val != nil {
		logID := val.(string)
		return logID
	}
	return "-"
}

func spanIDFromContext(ctx context.Context) uint64 {
	if ctx == nil {
		return 0
	}
	val := ctx.Value("K_SPANID")
	if val != nil {
		spanID, valid := val.(uint64)
		if valid {
			return spanID
		}
	}
	return 0
}

func getFileDate(name string) osTime.Time {
	sn := strings.Split(name, ".")
	t, _ := osTime.Parse(dateFormat, sn[len(sn)-1])
	return t
}

// The parameter is a string unsafely converted from byte slice
func deepCopyStr(s string) string {
	bytes := make([]byte, 0)
	bytes = append(bytes, s...)
	return string(bytes)
}

func deepCopyStrSlice(ss []string) []string {
	data := make([]string, len(ss))
	copy(data, ss)
	return data
}

// RateLimiters is an interface, it currently has one implementation based on Google's rate.Limiter
// Google's rate.Limiter performs slightly better than Juju's ratelimit.Bucket.
type RateLimiters interface {
	// Allow returns a bool based on the key and rate limit. The key can be file location.
	// If it is the first time calling Allow, it creates an instance.
	Allow(key string, limit int) bool
}

// RateLimiterMap is an implementation of RateLimiters.
// It controls write frequency for each line.
type RateLimiterMap struct {
	limiterMap *map[string]*rate.Limiter
	sync.Mutex
}

func NewRateLimiterMap() *RateLimiterMap {
	m := make(map[string]*rate.Limiter)
	return &RateLimiterMap{
		limiterMap: &m,
		Mutex:      sync.Mutex{},
	}
}

func (m *RateLimiterMap) Allow(key string, limit int) bool {
	if limiter, ok := m.get(key); ok {
		return limiter.Allow()
	}
	m.put(key, rate.NewLimiter(rate.Limit(limit), limit))
	return true
}

func (m *RateLimiterMap) get(location string) (*rate.Limiter, bool) {
	bucketMap := (*map[string]*rate.Limiter)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&m.limiterMap))))
	if b, ok := (*bucketMap)[location]; ok {
		return b, ok
	}
	return nil, false
}

func (m *RateLimiterMap) contains(location string) bool {
	bucketMap := (*map[string]*rate.Limiter)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&m.limiterMap))))
	if _, ok := (*bucketMap)[location]; ok {
		return true
	}
	return false
}

func (m *RateLimiterMap) put(location string, bucket *rate.Limiter) {
	m.Lock()
	defer m.Unlock()
	if m.contains(location) {
		return
	}
	newMap := make(map[string]*rate.Limiter, len(*m.limiterMap))
	if m.limiterMap != nil {
		for k, v := range *m.limiterMap {
			newMap[k] = v
		}
	}
	newMap[location] = bucket
	atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&m.limiterMap)), unsafe.Pointer(&newMap))
}
