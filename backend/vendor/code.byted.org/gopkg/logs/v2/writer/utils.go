package writer

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
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

type logFileTime struct {
	timeSeg osTime.Time
	seqNum  int
}

// After reports whether the log file time instant t is after u.
func (t logFileTime) After(u logFileTime) bool {
	return t.timeSeg.After(u.timeSeg) || (t.timeSeg.Equal(u.timeSeg) && t.seqNum > u.seqNum)
}

func getFileDate(name string) logFileTime {
	var t osTime.Time
	var n int
	var err error
	sn := strings.Split(name, ".")
	t, err = osTime.Parse(dateFormat, sn[len(sn)-1])
	if err != nil {
		t, _ = osTime.Parse(dateFormat, sn[len(sn)-2])
		n, _ = strconv.Atoi(sn[len(sn)-1])
	}
	return logFileTime{
		timeSeg: t,
		seqNum:  n,
	}
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

type CounterLimiterSyncMap struct {
	sMap sync.Map
}

func NewCountLimiterSyncMap() *CounterLimiterSyncMap {
	return &CounterLimiterSyncMap{sMap: sync.Map{}}
}

func (m *CounterLimiterSyncMap) Allow(key string, limit int) bool {
	if limit < 1 {
		return false
	}

	if limit == 1 {
		return true
	}

	var count uint64 = 1
	if countI, loaded := m.sMap.LoadOrStore(key, &count); loaded {
		if countI != nil {
			if count, ok := countI.(*uint64); ok {
				if atomic.AddUint64(count, 1)%uint64(limit) == 1 {
					return true
				}
			}

		}
		return false
	}
	return true
}

// CounterLimiterMap is an implementation of RateLimiters.
// It makes sure that only 1 log can be output every n logs.
type CounterLimiterMap struct {
	counterMap *map[string]*uint64
	sync.Mutex
}

func NewCountLimiterMap() *CounterLimiterMap {
	m := make(map[string]*uint64)
	return &CounterLimiterMap{
		counterMap: &m,
		Mutex:      sync.Mutex{},
	}
}

func (m *CounterLimiterMap) Allow(key string, limit int) bool {
	if limit < 1 {
		return false
	}

	if limit == 1 {
		return true
	}

	if count, ok := m.get(key); ok {
		if count != nil {
			if atomic.AddUint64(count, 1)%uint64(limit) == 1 {
				return true
			}
			return false
		}
	}
	return m.put(key)
}

func (m *CounterLimiterMap) get(location string) (*uint64, bool) {
	countMap := (*(map[string]*uint64))(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&m.counterMap))))
	if b, ok := (*countMap)[location]; ok {
		return b, ok
	}
	return nil, false
}

func (m *CounterLimiterMap) put(location string) bool {
	m.Lock()
	defer m.Unlock()
	if countP, ok := m.get(location); ok {
		atomic.AddUint64(countP, 1)
		return false
	}

	newMap := make(map[string]*uint64, len(*m.counterMap)+1)
	if m.counterMap != nil {
		for k, v := range *m.counterMap {
			newMap[k] = v
		}
	}
	var value uint64 = 1
	newMap[location] = &value
	atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&m.counterMap)), unsafe.Pointer(&newMap))
	return true
}

func (m *CounterLimiterMap) contains(location string) bool {
	bucketMap := (*map[string]*uint64)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&m.counterMap))))
	if _, ok := (*bucketMap)[location]; ok {
		return true
	}
	return false
}

type Packet *[]byte

// NewPacket gets a usable byte slice.
func NewPacket(length int) Packet {
	p := packetPool.Get().(Packet)
	*p = (*p)[:0]
	if length > 0 {
		*p = append(*p, make([]byte, length)...)
	}
	return p
}

func PutPacket(p Packet) {
	*p = (*p)[:0]
	packetPool.Put(p)
}

var packetPool = &sync.Pool{
	New: func() interface{} {
		data := make([]byte, 0, 32)
		return Packet(&data)
	},
}
