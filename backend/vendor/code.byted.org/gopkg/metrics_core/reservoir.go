package metrics

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/beorn7/perks/quantile"

	"code.byted.org/gopkg/metrics_core/utils"
)

var (
	nInf = math.Inf(-1)
)

func newReservoir(v *Value) reservoir {
	var r reservoir
	switch v.mType {
	case CounterType:
		r = newAddition(v.suffix, v.mType, v.reportInitialValue)
	case RateCounterType:
		r = newRatio(v.suffix, v.mType, v.interval)
	case DeltaCounterType:
		r = newDeltaCounter(v.suffix, v.mType, v.reportInitialValue, v.interval)
	case StoreType:
		r = newStore(v.suffix, v.mType)
	case TimerType:
		if v.timerOption.useCKMS {
			r = newCkmsSummary(v.suffix, v.mType, *v.timerOption)
		} else {
			r = newSummaryRes(v.suffix, v.mType, v.timerOption)
		}
	case HistogramType:
		r = newHistogramRes(v.suffix, v.mType, v.buckets)
	default:
		// never arrived
		panic("wrong metric type")
	}
	r.merge(v)
	measurePool.Put((*measure)(unsafe.Pointer(v)))
	return r
}

type value struct {
	suffix string
	mType  mType
	values []float64
}

type reservoir interface {
	getType() mType
	getSuffix() string
	merge(*Value) bool
	values() []value
	size() int
	setReportLastValues()
	reportLastValues() bool // should not be called concurrently.
	getLastValues() []value
}

type res struct {
	suffix string
	mType  mType
}

func (r *res) getSuffix() string {
	return r.suffix
}

type addition struct {
	// ensure the value/valuef go first in the struct
	// to guarantee alignment for atomic operations.
	// http://golang.org/pkg/sync/atomic/#pkg-note-BUG
	value      int64
	valuefBits uint64
	res
	currValue          float64
	lastValue          float64
	reportInitialValue bool
}

func newAddition(suffix string, mType2 mType, reportInitialValue bool) *addition {
	return &addition{
		value:      0,
		valuefBits: 0,
		res: res{
			suffix: suffix,
			mType:  mType2,
		},
		reportInitialValue: reportInitialValue,
	}
}

func (r *addition) getType() mType {
	return CounterType
}

func (r *addition) merge(value *Value) bool {
	if value.suffix != r.suffix || value.mType != r.mType {
		return false
	}
	if value.value != 0 {
		atomic.AddInt64(&r.value, value.value)
		return true
	}

	if value.valuef == 0 {
		return true
	}

	for {
		oldBits := atomic.LoadUint64(&r.valuefBits)
		newBits := math.Float64bits(math.Float64frombits(oldBits) + value.valuef)
		if atomic.CompareAndSwapUint64(&r.valuefBits, oldBits, newBits) {
			break
		}
	}
	return true
}

func (r *addition) values() []value {
	v := atomic.LoadInt64(&r.value)
	vf := math.Float64frombits(atomic.LoadUint64(&r.valuefBits))
	vf += float64(v)
	r.lastValue, r.currValue = r.currValue, vf
	return []value{{r.suffix, r.mType, []float64{vf}}}
}

func (r *addition) getLastValues() []value {
	return []value{{r.suffix, r.mType, []float64{r.lastValue}}}
}

func (r *addition) size() int {
	return 49 + len(r.suffix) // values have 16 bytes, a string has 2*8 bytes, mType has 8 bytes by default
}

func (r *addition) reportLastValues() bool {
	if !r.reportInitialValue {
		return false
	}
	r.reportInitialValue = false
	return true
}

func (r *addition) setReportLastValues() {
	r.reportInitialValue = true
}

type ratio struct {
	value      int64
	valuefBits uint64
	res
	interval int
}

func newRatio(suffix string, mType mType, interval int) *ratio {
	return &ratio{
		res: res{
			suffix: suffix,
			mType:  mType,
		},
		interval: interval,
	}
}

func (r *ratio) getType() mType {
	return RateCounterType
}

func (r *ratio) merge(value *Value) bool {
	if value.suffix != r.suffix || value.mType != r.mType {
		return false
	}
	if value.value != 0 {
		atomic.AddInt64(&r.value, value.value)
		return true
	}

	if value.valuef == 0 {
		return true
	}

	for {
		oldBits := atomic.LoadUint64(&r.valuefBits)
		newBits := math.Float64bits(math.Float64frombits(oldBits) + value.valuef)
		if atomic.CompareAndSwapUint64(&r.valuefBits, oldBits, newBits) {
			break
		}
	}
	return true
}

func (r *ratio) values() []value {
	v := atomic.SwapInt64(&r.value, 0)
	vf := math.Float64frombits(atomic.SwapUint64(&r.valuefBits, math.Float64bits(float64(0))))
	vf += float64(v)

	return []value{{r.suffix, r.mType, []float64{vf / float64(r.interval)}}}
}

func (r *ratio) getLastValues() []value {
	return nil
}

func (r *ratio) reportLastValues() bool {
	return false
}

func (r *ratio) setReportLastValues() {
}

func (r *ratio) size() int {
	return 48 + len(r.suffix) // values and interval have 24 bytes, a string has 2*8 bytes, mType has 8 bytes by default
}

type idempotence struct {
	valueBits uint64
	res
}

func newStore(suffix string, mType mType) *idempotence {
	return &idempotence{
		valueBits: math.Float64bits(nInf),
		res: res{
			suffix: suffix,
			mType:  mType,
		},
	}
}

func (r *idempotence) getType() mType {
	return StoreType
}
func (r *idempotence) merge(value *Value) bool {
	if value.suffix != r.suffix || value.mType != r.mType {
		return false
	}

	var vf float64
	if value.valuef == nInf {
		vf = float64(value.value)
	} else {
		vf = value.valuef
	}

	atomic.StoreUint64(&r.valueBits, math.Float64bits(vf))
	return true
}

func (r *idempotence) size() int {
	return 32 + len(r.suffix)
}

func (r *idempotence) values() []value {
	vf := math.Float64frombits(atomic.SwapUint64(&r.valueBits, math.Float64bits(nInf)))

	if vf == nInf {
		return nil
	}
	return []value{{r.suffix, r.mType, []float64{vf}}}
}

func (r *idempotence) getLastValues() []value {
	return nil
}

func (r *idempotence) reportLastValues() bool {
	return false
}

func (r *idempotence) setReportLastValues() {
}

type summaryRes struct {
	res
	sync.RWMutex
	hot, cold []float64

	min, max, sum float64
	count         int64

	opt *timerOption
}

func newSummaryRes(suffix string, mType mType, opt *timerOption) *summaryRes {
	hot := make([]float64, 0)
	cold := make([]float64, 0)
	return &summaryRes{
		res: res{
			suffix: suffix,
			mType:  mType,
		},
		hot:  hot,
		cold: cold,
		min:  math.MaxInt64,
		max:  math.MinInt64,
		opt:  opt,
	}
}

func (r *summaryRes) getType() mType {
	return TimerType
}

func (r *summaryRes) size() int {
	r.RLock()
	defer r.RUnlock()
	return 24 + len(r.suffix) + // res
		24 + // mutex
		24 + 24 + 8*(len(r.hot)+len(r.cold)) + // two buffers
		32 + 8 // min, max, sum, count, opt
}

func (r *summaryRes) merge(value *Value) bool {
	if value.suffix != r.suffix || value.mType != r.mType {
		return false
	}
	r.Lock()
	vf := value.valuef + float64(value.value)

	if vf < r.min {
		r.min = vf
	}
	if vf > r.max {
		r.max = vf
	}
	r.sum += vf
	r.count += 1

	if r.opt.cacheCapacity > 0 && len(r.hot) > r.opt.cacheCapacity {
		r.Unlock()
		return true
	}
	r.hot = append(r.hot, vf)
	r.Unlock()
	return true
}

func (r *summaryRes) values() []value {
	var min, max, sum float64
	var count int64
	r.Lock()
	r.hot, r.cold = r.cold, r.hot
	min, r.min = r.min, float64(math.MaxInt64)
	max, r.max = r.max, float64(math.MinInt64)
	count, r.count = r.count, 0
	sum, r.sum = r.sum, 0
	r.Unlock()

	sort.Sort(samples(r.cold))

	vs := make([]value, 0, 10)
	if len(r.cold) == 0 {
		return vs
	}
	if !r.opt.compatTimer {
		ms := make([]float64, 0, 10)
		ms = append(ms, min)
		ms = append(ms, max)
		ms = append(ms, sum/float64(count))
		ms = append(ms, sum)
		ms = append(ms, float64(count))
		ms = append(ms, percentile(r.cold, 0.5))
		ms = append(ms, percentile(r.cold, 0.9))
		ms = append(ms, percentile(r.cold, 0.95))
		ms = append(ms, percentile(r.cold, 0.99))
		ms = append(ms, percentile(r.cold, 0.999))
		vs = append(vs, value{r.suffix, r.mType, ms})
	} else {
		suffix := r.suffix
		if len(suffix) > 0 && suffix[len(suffix)-1] != '.' {
			suffix += "."
		}

		vs = append(vs, value{suffix + "min", StoreType, []float64{min}})
		vs = append(vs, value{suffix + "max", StoreType, []float64{max}})
		vs = append(vs, value{suffix + "avg", StoreType, []float64{sum / float64(count)}})
		vs = append(vs, value{suffix + "sum", StoreType, []float64{sum}})
		vs = append(vs, value{suffix + "counter", StoreType, []float64{float64(count)}})
		vs = append(vs, value{suffix + "pct50", StoreType, []float64{percentile(r.cold, 0.5)}})
		vs = append(vs, value{suffix + "pct90", StoreType, []float64{percentile(r.cold, 0.9)}})
		vs = append(vs, value{suffix + "pct95", StoreType, []float64{percentile(r.cold, 0.95)}})
		vs = append(vs, value{suffix + "pct99", StoreType, []float64{percentile(r.cold, 0.99)}})
		vs = append(vs, value{suffix + "pct999", StoreType, []float64{percentile(r.cold, 0.999)}})

	}
	r.cold = r.cold[:0]
	return vs
}

func (r *summaryRes) getLastValues() []value {
	return nil
}

func (r *summaryRes) reportLastValues() bool {
	return false
}

func (r *summaryRes) setReportLastValues() {
}

type bucket struct {
	left float64

	count int64
}

func newBucket(left float64) *bucket {
	return &bucket{
		left: left,
	}
}

type histogram struct {
	sync.RWMutex

	res
	buckets []*bucket
	sum     float64
	count   int64
}

func newHistogramRes(suffix string, mType mType, buckets []float64) *histogram {
	bs := make([]*bucket, 0, len(buckets))
	for _, b := range buckets {
		bs = append(bs, newBucket(b))
	}
	return &histogram{
		res: res{
			suffix: suffix,
			mType:  mType,
		},
		buckets: bs,
	}
}

func (r *histogram) getType() mType {
	return HistogramType
}

func (h *histogram) size() int {
	h.RLock()
	defer h.RUnlock()
	return 24 + len(h.suffix) + // res
		24 + // mutex
		24 + len(h.buckets)*(8+8+8) + // buckets
		8 + 8 // sum + count

}

func (h *histogram) merge(value *Value) bool {
	if value.suffix != h.suffix || value.mType != h.mType {
		return false
	}
	fv := float64(value.value) + value.valuef
	for i, bucket := range h.buckets {
		var right float64
		if i < len(h.buckets)-1 {
			right = h.buckets[i+1].left
		} else {
			right = math.Inf(1)
		}
		if fv >= bucket.left && fv < right {
			h.RLock()
			atomic.AddInt64(&bucket.count, 1)
			atomic.AddInt64(&h.count, 1)
			utils.AtomicAddFloat64(&h.sum, fv)
			h.RUnlock()
			break
		}
	}
	return true
}

func (h *histogram) values() []value {
	currCount := atomic.LoadInt64(&h.count)
	if currCount == 0 {
		return nil
	}
	vs := make([]value, 0, 1)
	ms := make([]float64, 0, 10)

	h.Lock()
	for _, bucket := range h.buckets {
		ms = append(ms, float64(bucket.count))
		bucket.count = 0
	}

	ms = append(ms, h.sum)            // sum
	ms = append(ms, float64(h.count)) // count
	h.sum = 0
	h.count = 0
	h.Unlock()
	return append(vs, value{h.suffix, h.mType, ms})
}

func (r *histogram) getLastValues() []value {
	return nil
}

func (h *histogram) reportLastValues() bool {
	return false
}

func (r *histogram) setReportLastValues() {
}

const (
	DefEpsilon    = 0.01
	DefMaxAge     = 10 * time.Minute
	DefAgeBuckets = 5
	DefBufCap     = 500
)

// ckmsSummary use the CKMS approximate algorithm to aggregate timer data points.
// reference:
// https://grafana.com/blog/2022/03/01/how-summary-metrics-work-in-prometheus/
// https://github.com/prometheus/client_golang/blob/main/prometheus/summary.go
type ckmsSummary struct {
	res
	compatMode bool
	bufMtx     sync.Mutex
	mtx        sync.Mutex

	hotBuf, coldBuf []float64
	min, max, sum   float64
	count           int64

	objectives       map[float64]float64
	sortedObjectives []float64

	streams                          []*quantile.Stream
	streamDuration                   time.Duration
	headStream                       *quantile.Stream
	headStreamIdx                    int
	headStreamExpTime, hotBufExpTime time.Time
}

func (s *ckmsSummary) getType() mType {
	return TimerType
}

func (s *ckmsSummary) size() int {
	s.bufMtx.Lock()
	defer s.bufMtx.Unlock()
	return 24 + len(s.suffix) + // res
		1 + // compatMode
		8 + 8 + // two mutex
		24 + len(s.hotBuf)*8 + 24 + len(s.coldBuf)*8 + // hot and cold bufs
		(8 + 1 + 1 + 1 + 2 + 4 + 8 + 8 + 8 + 8) + len(s.objectives)*(8+8) + // approximate size of objectives, reference: https://segmentfault.com/q/1010000023060584
		24 + len(s.sortedObjectives)*8 + // sorted objectives
		24 + len(s.streams)*(8+(8+(8+8+24+128*8+8))+24+1) + //
		8 + // streamDuration
		8 + // headStream
		8 + // headStreamIdx
		8 + 8 //  headStreamExpTime, hotBufExpTime
}

func newCkmsSummary(suffix string, mType mType, ops timerOption) *ckmsSummary {
	s := &ckmsSummary{
		res: res{
			suffix: suffix,
			mType:  mType,
		},
		bufMtx:           sync.Mutex{},
		mtx:              sync.Mutex{},
		hotBuf:           make([]float64, 0, ops.bufCap),
		coldBuf:          make([]float64, 0, ops.bufCap),
		min:              math.MaxInt64,
		max:              math.MinInt64,
		sum:              0,
		count:            0,
		objectives:       ops.objectives,
		sortedObjectives: make([]float64, 0, len(ops.objectives)),
		compatMode:       ops.compatTimer,
		streamDuration:   ops.maxAge / time.Duration(ops.ageBuckets),
	}

	s.headStreamExpTime = time.Now().Add(s.streamDuration)
	s.hotBufExpTime = s.headStreamExpTime
	for i := uint32(0); i < ops.ageBuckets; i++ {
		s.streams = append(s.streams, s.newStream())
	}
	s.headStream = s.streams[0]

	for qu := range s.objectives {
		s.sortedObjectives = append(s.sortedObjectives, qu)
	}

	sort.Float64s(s.sortedObjectives)

	return s
}

func (s *ckmsSummary) merge(value *Value) bool {
	if value.suffix != s.suffix || value.mType != s.mType {
		return false
	}

	s.bufMtx.Lock()
	defer s.bufMtx.Unlock()

	vf := value.valuef + float64(value.value)
	if vf < s.min {
		s.min = vf
	}
	if vf > s.max {
		s.max = vf
	}
	s.sum += vf
	s.count += 1

	now := time.Now()
	if now.After(s.hotBufExpTime) {
		s.asyncFlush(now)
	}

	s.hotBuf = append(s.hotBuf, vf)
	if len(s.hotBuf) == cap(s.hotBuf) {
		s.asyncFlush(now)
	}
	return true
}

func (s *ckmsSummary) values() []value {
	s.bufMtx.Lock()
	s.mtx.Lock()
	defer s.mtx.Unlock()
	defer s.bufMtx.Unlock()

	// Swap bufs even if hotBuf is empty to set new hotBufExpTime.
	s.swapBufs(time.Now())
	var min, max, sum float64
	var count int64

	min, s.min = s.min, float64(math.MaxInt64)
	max, s.max = s.max, float64(math.MinInt64)
	count, s.count = s.count, 0
	sum, s.sum = s.sum, 0

	if count == 0 {
		return nil
	}

	s.flushColdBuf()
	vs := make([]value, 0, 10)
	pctValues := make([]float64, 0, 5)

	for _, rank := range s.sortedObjectives {
		var q float64
		if s.headStream.Count() == 0 {
			q = math.NaN()
		} else {
			q = s.headStream.Query(rank)
		}
		pctValues = append(pctValues, q)
	}
	s.reset()

	if !s.compatMode {
		ms := make([]float64, 0, 10)
		ms = append(ms, min)
		ms = append(ms, max)
		ms = append(ms, sum/float64(count))
		ms = append(ms, sum)
		ms = append(ms, float64(count))
		ms = append(ms, pctValues[0])
		ms = append(ms, pctValues[1])
		ms = append(ms, pctValues[2])
		ms = append(ms, pctValues[3])
		ms = append(ms, pctValues[4])
		vs = append(vs, value{s.suffix, s.mType, ms})
	} else {
		suffix := s.suffix
		if len(suffix) > 0 && suffix[len(suffix)-1] != '.' {
			suffix += "."
		}

		vs = append(vs, value{suffix + "min", StoreType, []float64{min}})
		vs = append(vs, value{suffix + "max", StoreType, []float64{max}})
		vs = append(vs, value{suffix + "avg", StoreType, []float64{sum / float64(count)}})
		vs = append(vs, value{suffix + "sum", StoreType, []float64{sum}})
		vs = append(vs, value{suffix + "counter", StoreType, []float64{float64(count)}})
		vs = append(vs, value{suffix + "pct50", StoreType, []float64{pctValues[0]}})
		vs = append(vs, value{suffix + "pct90", StoreType, []float64{pctValues[1]}})
		vs = append(vs, value{suffix + "pct95", StoreType, []float64{pctValues[2]}})
		vs = append(vs, value{suffix + "pct99", StoreType, []float64{pctValues[3]}})
		vs = append(vs, value{suffix + "pct999", StoreType, []float64{pctValues[4]}})

	}
	return vs
}

func (s *ckmsSummary) reset() {
	// Already hold two locks.
	for i := range s.streams {
		s.streams[i].Reset()
	}
	s.coldBuf = s.coldBuf[:0]
	s.hotBuf = s.hotBuf[:0]
	s.headStreamIdx = 0
	s.headStreamExpTime = time.Now().Add(s.streamDuration)
	s.hotBufExpTime = s.headStreamExpTime
}

func (s *ckmsSummary) getLastValues() []value {
	return nil
}

func (s *ckmsSummary) reportLastValues() bool {
	return false
}

func (s *ckmsSummary) setReportLastValues() {
}

func (s *ckmsSummary) newStream() *quantile.Stream {
	return quantile.NewTargeted(s.objectives)
}

// asyncFlush needs bufMtx locked.
func (s *ckmsSummary) asyncFlush(now time.Time) {
	s.mtx.Lock()
	s.swapBufs(now)

	// Unblock the original goroutine that was responsible for the mutation
	// that triggered the compaction.  But hold onto the global non-buffer
	// state mutex until the operation finishes.
	go func() {
		defer func() {
			if err := recover(); err != nil {
				buf := make([]byte, 256)
				n := runtime.Stack(buf, false)
				_, _ = fmt.Fprintf(os.Stderr, string(buf[:n]))
			}
		}()

		s.flushColdBuf()
		s.mtx.Unlock()
	}()
}

// rotateStreams needs mtx AND bufMtx locked.
func (s *ckmsSummary) maybeRotateStreams() {
	for !s.hotBufExpTime.Equal(s.headStreamExpTime) {
		s.headStream.Reset()
		s.headStreamIdx++
		if s.headStreamIdx >= len(s.streams) {
			s.headStreamIdx = 0
		}
		s.headStream = s.streams[s.headStreamIdx]
		s.headStreamExpTime = s.headStreamExpTime.Add(s.streamDuration)
	}
}

// flushColdBuf needs mtx locked.
func (s *ckmsSummary) flushColdBuf() {
	for _, v := range s.coldBuf {
		for _, stream := range s.streams {
			stream.Insert(v)
		}
	}
	s.coldBuf = s.coldBuf[0:0]
	s.maybeRotateStreams()
}

// swapBufs needs mtx AND bufMtx locked, coldBuf must be empty.
func (s *ckmsSummary) swapBufs(now time.Time) {
	if len(s.coldBuf) != 0 {
		panic("coldBuf is not empty")
	}
	s.hotBuf, s.coldBuf = s.coldBuf, s.hotBuf
	// hotBuf is now empty and gets new expiration set.
	for now.After(s.hotBufExpTime) {
		s.hotBufExpTime = s.hotBufExpTime.Add(s.streamDuration)
	}
}

type samples []float64

func (s samples) Len() int { return len(s) }

func (s samples) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func (s samples) Less(i, j int) bool { return s[i] < s[j] }

func percentile(s []float64, p float64) float64 {
	return s[int(float64(len(s))*p)]
}

type deltaCounter struct {
	// ensure the value/valuef go first in the struct
	// to guarantee alignment for atomic operations.
	// http://golang.org/pkg/sync/atomic/#pkg-note-BUG
	value      int64
	valuefBits uint64
	res
	currValue          float64
	lastValue          float64
	reportInitialValue bool
	interval           int
}

func newDeltaCounter(suffix string, mType2 mType, reportInitialValue bool, interval int) *deltaCounter {
	return &deltaCounter{
		value:      0,
		valuefBits: 0,
		res: res{
			suffix: suffix,
			mType:  mType2,
		},
		reportInitialValue: reportInitialValue,
		interval:           interval,
	}
}

func (r *deltaCounter) getType() mType {
	return DeltaCounterType
}

func (r *deltaCounter) merge(value *Value) bool {
	if value.suffix != r.suffix || value.mType != r.mType {
		return false
	}
	if value.value != 0 {
		atomic.AddInt64(&r.value, value.value)
		return true
	}

	if value.valuef == 0 {
		return true
	}

	for {
		oldBits := atomic.LoadUint64(&r.valuefBits)
		newBits := math.Float64bits(math.Float64frombits(oldBits) + value.valuef)
		if atomic.CompareAndSwapUint64(&r.valuefBits, oldBits, newBits) {
			break
		}
	}
	return true
}

func (r *deltaCounter) values() []value {
	v := atomic.LoadInt64(&r.value)
	counter := math.Float64frombits(atomic.LoadUint64(&r.valuefBits))
	counter += float64(v)

	delta := counter - r.currValue
	rate := delta / float64(r.interval)

	r.lastValue, r.currValue = r.currValue, counter
	return []value{{r.suffix, r.mType, []float64{delta, rate, counter}}}
}

func (r *deltaCounter) getLastValues() []value {
	return []value{{r.suffix, r.mType, []float64{0, 0, r.lastValue}}}
}

func (r *deltaCounter) size() int {
	return 57 + len(r.suffix) // values have 16 bytes, a string has 2*8 bytes, mType has 8 bytes by default
}

func (r *deltaCounter) reportLastValues() bool {
	if !r.reportInitialValue {
		return false
	}
	r.reportInitialValue = false
	return true
}

func (r *deltaCounter) setReportLastValues() {
	r.reportInitialValue = true
}
