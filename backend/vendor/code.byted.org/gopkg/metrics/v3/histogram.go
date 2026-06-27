package metrics

import (
	"math"
	"sync"
	"sync/atomic"
	"unsafe"
)

var binValuePool = sync.Pool{
	New: func() interface{} {
		return &binValue{}
	},
}

type binValue struct {
	count int64
	sum   float64
	mean  float64
	sync.RWMutex
}

func newBinValue(count int64, sum, mean float64) *binValue {
	value := binValuePool.Get().(*binValue)
	value.Lock()
	value.count = count
	value.sum = sum
	value.mean = mean
	value.Unlock()
	return value
}

type bin struct {
	*binValue
	next *bin
}

func newBin(n float64, next *bin) *bin {
	return &bin{binValue: &binValue{count: 1, sum: n, mean: n}, next: next}
}

func (b *bin) getCountSumMean() (int64, float64, float64) {
	value := (*binValue)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&b.binValue))))
	value.RLock()
	count, sum, mean := value.count, value.sum, value.mean
	value.RUnlock()
	return count, sum, mean
}

func (b *bin) mergeNext() {
	b.count += b.next.count
	b.sum += b.next.sum
	b.mean = b.sum / float64(b.count)
	b.next = b.next.next
}

type hReservoir struct {
	maxBins uint64
	binNum  uint64
	head    *bin

	sideWriteLock sync.RWMutex
}

func newHReservoir(maxBins uint) hReservoir {
	return hReservoir{
		maxBins: uint64(maxBins),
		head:    &bin{binValue: &binValue{}},
	}
}

func (r *hReservoir) compress() {
	r.sideWriteLock.Lock()
	defer r.sideWriteLock.Unlock()

	minGapAt := r.head.next
	minGap := math.MaxFloat64
	prev := r.head.next
	for {
		if prev.next == nil {
			minGapAt.mergeNext()
			return
		}
		gap := prev.next.mean - prev.mean
		if gap <= minGap {
			minGap = gap
			minGapAt = prev
		}
		prev = prev.next
	}
}

func (r *hReservoir) insert(n float64) {
	r.sideWriteLock.RLock()
	prev := r.head
RANGE:
	for {
		nextPtr := (*unsafe.Pointer)(unsafe.Pointer(&prev.next))
		next := (*bin)(atomic.LoadPointer(nextPtr))
		// insert at tail
		if next == nil {
			if !atomic.CompareAndSwapPointer(nextPtr, nil, unsafe.Pointer(newBin(n, nil))) {
				continue
			}
			if atomic.LoadUint64(&r.binNum) != r.maxBins {
				atomic.AddUint64(&r.binNum, 1)
				r.sideWriteLock.RUnlock()
			} else {
				r.sideWriteLock.RUnlock()
				r.compress()
			}
			return
		}
		// merge
		for {
			nextValuePtr := atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&next.binValue)))
			value := (*binValue)(nextValuePtr)

			value.RLock()
			if value.mean < n {
				value.RUnlock()
				break
			}
			count, sum := value.count, value.sum
			value.RUnlock()
			count += 1
			sum += n
			newValue := newBinValue(count, sum, sum/float64(count))
			if atomic.CompareAndSwapPointer((*unsafe.Pointer)(unsafe.Pointer(&next.binValue)), nextValuePtr, unsafe.Pointer(newValue)) {
				r.sideWriteLock.RUnlock()
				// putting back the old value would makes other goroutines get the wrong mean value above,
				// however it does not matter, those goroutine would not pass this CAS check,
				// and continue range in the post logic
				binValuePool.Put(value)
				return
			}
			binValuePool.Put(newValue)
			continue RANGE
		}
		prev = next
	}
}

func (r *hReservoir) reset() {
	r.sideWriteLock.Lock()
	defer r.sideWriteLock.Unlock()
	r.binNum = 0
	r.head = &bin{binValue: &binValue{}}
}

// Histogram implements a high dynamic ranged histogram inspired by https://github.com/beorn7/perks,
// and makes it more powerful under high parallel writing.
// It can be used in the massive writing scene.
type Histogram struct {
	count   int64
	sum     int64
	sumBits uint64
	res     hReservoir

	sync.RWMutex
}

// NewHistogram creates a Histogram with max bins number.
func NewHistogram(maxBins uint) *Histogram {
	return &Histogram{res: newHReservoir(maxBins)}
}

// Insert inserts a float64 number into Histogram.
func (h *Histogram) Insert(n float64) {
	if n <= 0 {
		return
	}
	h.RLock()
	atomic.AddInt64(&h.count, 1)
	vi := int64(n)
	if float64(vi) == n {
		atomic.AddInt64(&h.sum, vi)
	} else {
		for {
			oldBits := atomic.LoadUint64(&h.sumBits)
			newBits := math.Float64bits(math.Float64frombits(oldBits) + n)
			if atomic.CompareAndSwapUint64(&h.sumBits, oldBits, newBits) {
				break
			}
		}
	}
	h.res.insert(n)
	h.RUnlock()
}

func (h *Histogram) bottomN(i int64) float64 {
	h.res.sideWriteLock.Lock()
	defer h.res.sideWriteLock.Unlock()

	s := i
	prev := h.res.head
	var n int64
	for {
		nextPtr := (*unsafe.Pointer)(unsafe.Pointer(&prev.next))
		next := (*bin)(atomic.LoadPointer(nextPtr))
		count, _, mean := prev.getCountSumMean()
		n += count
		if n >= s || next == nil {
			return mean
		}
		prev = prev.next
	}
}

// Percentile returns the percentage value,
// For example, Percentile(0.9) means acquire the pct90 value in the Histogram.
func (h *Histogram) Percentile(f float64) float64 {
	return h.bottomN(int64(math.Ceil(float64(h.count) * f)))
}

// Min returns the minimal value in the Histogram.
func (h *Histogram) Min() float64 {
	return h.bottomN(1)
}

// Max returns the maximal value in the Histogram.
func (h *Histogram) Max() float64 {
	return h.bottomN(h.count)
}

// Avg returns the average number in the Histogram.
func (h *Histogram) Avg() float64 {
	if h.count == 0 {
		return 0
	}
	return (float64(h.sum) + math.Float64frombits(atomic.LoadUint64(&h.sumBits))) / float64(h.count)
}

// Count returns the count of the Histogram insertion.
func (h *Histogram) Count() int64 {
	return atomic.LoadInt64(&h.count)
}

// Sum returns the sum of all Histogram values.
func (h *Histogram) Sum() float64 {
	return float64(h.sum) + math.Float64frombits(atomic.LoadUint64(&h.sumBits))
}

// Reset resets the histogram.
func (h *Histogram) Reset() {
	h.sum = 0
	h.sumBits = 0
	h.count = 0
	h.res.reset()
}
