package metrics

import (
	"sync"
	"sync/atomic"

	"github.com/bytedance/gopkg/collection/skipmap"
)

const (
	cacheCapacity = 0x3fff
)

var asyncChan chan asyncTask

type asyncTask struct {
	sender   senderInterface
	entities *metricEntities
}

func init() {
	asyncChan = make(chan asyncTask, asyncChanBuffer)
	for i := 0; i < asyncGoroutines; i++ {
		go func() {
			for t := range asyncChan {
				if t.entities == nil { // BUG?
					continue
				}
				if v := *t.entities; len(v) > 0 {
					if v.IsCounter() {
						t.sender.SendCounter(v)
					} else {
						t.sender.Send(v)
					}
				}
				mesPool.Put(t.entities)
			}
		}()
	}
}

type cachedTags struct {
	tb []byte
}

func (e *cachedTags) Bytes() []byte {
	if e == nil {
		return nil
	}
	return e.tb
}

type tagCache struct {
	m *skipmap.StringMap

	setn uint64
}

func newTagCache() *tagCache {
	return &tagCache{m: skipmap.NewString()}
}

func (c *tagCache) Get(key []byte) *cachedTags {
	t, ok := c.m.Load(ss(key))
	if ok {
		return t.(*cachedTags)
	}
	return nil
}

func (c *tagCache) Set(key []byte, tt *cachedTags) {
	if atomic.AddUint64(&c.setn, 1)%cacheCapacity == 0 {
		// every cacheCapacity times call, we clear the map for memory leak issue
		// there is no reason to have so many tags
		// FIXME: sync.Map don't have Len method and `setn` may not equal to the len in concurrency env
		samples := make([]interface{}, 0, 3)
		c.m.Range(func(key string, _ interface{}) bool {
			c.m.Delete(key)
			if len(samples) < cap(samples) {
				samples = append(samples, key)
			}
			return true
		}) // clear map
		logfunc("[WARN] gopkg/metrics: too many tags. samples: %v", samples)
	}
	c.m.Store(string(key), tt)
}

func (c *tagCache) GetOrCreate(tags []T, extTagBytes []byte) *cachedTags {
	k := make([]byte, 0, 512)
	// XXX: we dont sort the tags to improve performance
	// for v2 api, the tags list should be stable all the time
	// for v1 api which use map to store tags, we sort it in Map2Tags
	k = appendTags(k, tags)
	// Lazy generate value.
	valuef := func() interface{} {
		b := make([]byte, 0, len(k)+1+len(extTagBytes))
		b = append(b, k...)
		if len(extTagBytes) > 0 {
			if len(b) > 0 {
				b = append(b, '|')
			}
			gTags.RLock()
			b = append(b, extTagBytes...)
			gTags.RUnlock()
		}
		e := &cachedTags{b}
		return e
	}
	actual, loaded := c.m.LoadOrStoreLazy(string(k), valuef)
	if !loaded {
		// use % instead of &.
		if atomic.AddUint64(&c.setn, 1)%cacheCapacity == 0 {
			// every cacheCapacity times call, we clear the map for memory leak issue
			// there is no reason to have so many tags
			samples := make([]interface{}, 0, 3)
			c.m.Range(func(key string, _ interface{}) bool {
				c.m.Delete(key)
				if len(samples) < cap(samples) {
					samples = append(samples, key)
				}
				return c.m.Len() > cacheCapacity
			}) // make sure at most cacheCapacity tags in the cache
			logfunc("[WARN] gopkg/metrics: too many tags. samples: %v", samples)
		}
	}
	return actual.(*cachedTags)
}

func (c *tagCache) MakeMetricEntity(mt metricsType, prefix string, name string, v float64, ts int64, tags []T) metricEntity {
	e := metricEntity{mt: mt, prefix: prefix, name: name, ts: ts, v: v}
	gTags.RLock()
	extTagBytes := gTags.Bytes()
	gTags.RUnlock()
	e.tt = c.GetOrCreate(tags, extTagBytes)
	return e
}

type metricCache struct {
	s senderInterface

	block int32

	counters mcacheEntities
	others   mcacheEntities
}

func newMetricCache(s senderInterface) *metricCache {
	return &metricCache{s: s}
}

func (m *metricCache) SetBlock(v bool) {
	if v {
		atomic.StoreInt32(&m.block, 1)
	} else {
		atomic.StoreInt32(&m.block, 0)
	}
}

func (m *metricCache) Send(e metricEntity) error {
	var mm *metricEntities
	if e.IsCounter() {
		mm = m.counters.Add(e, maxPendingSize)
	} else {
		mm = m.others.Add(e, maxPendingSize)
	}
	if mm == nil {
		return nil
	}
	return m.send(mm)
}

func (m *metricCache) send(mm *metricEntities) error {
	if atomic.LoadInt32(&m.block) != 0 {
		if mm.IsCounter() {
			m.s.SendCounter(*mm)
		} else {
			m.s.Send(*mm)
		}
		mesPool.Put(mm)
		return nil
	}
	select {
	case asyncChan <- asyncTask{sender: m.s, entities: mm}:
		return nil
	default:
		return errEmitBufferFull
	}
}

func (m *metricCache) Flush() {
	mm := mesPoolGet()
	defer mesPool.Put(mm)

	m.counters.AppendAndReset(mm)
	if mm.Len() > 0 {
		m.s.SendCounter(*mm)
	}

	mm.Reset()

	m.others.AppendAndReset(mm)
	if mm.Len() > 0 {
		m.s.Send(*mm)
	}
}

type metricEntities []metricEntity

func (mm *metricEntities) Reset() {
	*mm = (*mm)[:0]
}

func (mm *metricEntities) IsCounter() bool {
	return len(*mm) > 0 && (*mm)[0].IsCounter()
}

func (mm *metricEntities) Len() int {
	return len(*mm)
}

type mcacheEntities struct {
	mu sync.Mutex
	mm *metricEntities
}

func (s *mcacheEntities) Add(m metricEntity, max int) (mm *metricEntities) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mm == nil {
		s.mm = mesPoolGet()
	}
	*s.mm = append(*s.mm, m)
	if len(*s.mm) >= max {
		mm, s.mm = s.mm, mesPoolGet()
	}
	return
}

func (s *mcacheEntities) AppendAndReset(mm *metricEntities) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mm != nil && len(*s.mm) > 0 {
		*mm = append(*mm, *s.mm...)
		s.mm.Reset()
	}
}

func mesPoolGet() *metricEntities {
	mm := mesPool.Get().(*metricEntities)
	mm.Reset()
	return mm
}

var mesPool = sync.Pool{
	New: func() interface{} {
		mm := make(metricEntities, maxPendingSize)
		return &mm
	},
}
