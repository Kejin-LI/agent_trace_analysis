package metrics

import (
	"math"
	"sync/atomic"
	"time"
	"unsafe"
)

var (
	histogramWindow = 30 * time.Second
	nInf            = math.Inf(-1)
)

func newReservoir(v *Value) reservoir {
	var r reservoir
	switch v.mType {
	case rateCounterType, counterType, meterType:
		r = &additionRes{
			res: res{
				suffix: v.suffix,
				mType:  v.mType,
			},
		}
	case storeType:
		r = &idempotenceRes{
			res: res{
				suffix: v.suffix,
				mType:  v.mType,
			},
			valueBits: math.Float64bits(nInf),
		}
	case timerType:
		r = newHistogramRes(v.suffix, v.mType)
	default:
		// never arrived
		panic("wrong metric type")
	}
	r.merge(v)
	measurePool.Put((*measure)(unsafe.Pointer(v)))
	return r
}

type reservoir interface {
	getSuffix() string
	merge(*Value) bool
	marshal() []Value
}

type res struct {
	suffix string
	mType  mType
}

func (r *res) getSuffix() string {
	return r.suffix
}

type additionRes struct {
	// ensure the value/valuef go first in the struct
	// to guarantee alignment for atomic operations.
	// http://golang.org/pkg/sync/atomic/#pkg-note-BUG
	value      int64
	valuefBits uint64
	res

	// indicate if zero-value exists
	vflag int32
}

func (r *additionRes) resetVFlag() bool {
	return atomic.CompareAndSwapInt32(&r.vflag, 1, 0)
}

func (r *additionRes) setVFlag() {
	atomic.StoreInt32(&r.vflag, 1)
}

func (r *additionRes) merge(value *Value) bool {
	if value.suffix != r.suffix {
		return false
	}
	if value.value != 0 {
		atomic.AddInt64(&r.value, value.value)
		return true
	}

	if value.valuef == 0 {
		r.setVFlag()
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

func (r *additionRes) marshal() []Value {
	hasZeros := r.resetVFlag()

	// reset the value/valuef to zero, ms2 will accumulate the values
	v := atomic.SwapInt64(&r.value, 0)
	vf := math.Float64frombits(atomic.SwapUint64(&r.valuefBits, math.Float64bits(float64(0))))
	vf += float64(v)

	if vf == 0 && !hasZeros {
		return nil
	}

	return []Value{{r.suffix, r.mType, 0, vf}}
}

type idempotenceRes struct {
	valueBits uint64
	res
}

func (r *idempotenceRes) merge(value *Value) bool {
	if value.suffix != r.suffix {
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

func (r *idempotenceRes) marshal() []Value {
	vf := math.Float64frombits(atomic.SwapUint64(&r.valueBits, math.Float64bits(nInf)))
	if vf == nInf {
		return nil
	}
	return []Value{{r.suffix, r.mType, 0, vf}}
}

type windowHistogram struct {
	res
	histogram  *Histogram
	lastWindow time.Time
}

func newHistogramRes(suffix string, mType mType) *windowHistogram {
	now := time.Now()
	return &windowHistogram{
		res: res{
			suffix: suffix,
			mType:  mType,
		},
		histogram:  NewHistogram(100),
		lastWindow: now,
	}
}

func (r *windowHistogram) merge(value *Value) bool {
	if value.suffix != r.suffix {
		return false
	}
	r.histogram.Insert(float64(value.value) + value.valuef)
	return true
}

func (r *windowHistogram) marshal() []Value {
	if r.histogram.Count() == 0 {
		return nil
	}

	now := time.Now()
	if now.Sub(r.lastWindow) < histogramWindow {
		return nil
	}

	vs := make([]Value, 0, 9)
	r.lastWindow = now

	r.histogram.Lock()
	vs = append(vs, Value{r.suffix + ".min", storeType, 0, r.histogram.Min()})
	vs = append(vs, Value{r.suffix + ".max", storeType, 0, r.histogram.Max()})
	vs = append(vs, Value{r.suffix + ".avg", storeType, 0, r.histogram.Avg()})
	vs = append(vs, Value{r.suffix + ".sum", storeType, 0, r.histogram.Sum()})
	vs = append(vs, Value{r.suffix + ".counter", storeType, 0, float64(r.histogram.Count())})
	vs = append(vs, Value{r.suffix + ".pct50", storeType, 0, r.histogram.Percentile(0.5)})
	vs = append(vs, Value{r.suffix + ".pct90", storeType, 0, r.histogram.Percentile(0.9)})
	vs = append(vs, Value{r.suffix + ".pct95", storeType, 0, r.histogram.Percentile(0.95)})
	vs = append(vs, Value{r.suffix + ".pct99", storeType, 0, r.histogram.Percentile(0.99)})
	r.histogram.Reset()
	r.histogram.Unlock()

	return vs
}
