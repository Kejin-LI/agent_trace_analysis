package metrics

import (
	"math"
	"sync"
)

var measurePool = sync.Pool{
	New: func() interface{} {
		return &measure{}
	},
}

// Incr increments a rate counter type metric as a default suffix "rate".
func Incr(n int) *Value {
	return WithSuffix("rate").Incr(n)
}

// Observe increments a timer type metric as a default suffix "timer".
func Observe(n int) *Value {
	return WithSuffix("timer").Observe(n)
}

// Store store a metric as a default suffix "store".
func Store(n int) *Value {
	return WithSuffix("store").Store(n)
}

// IncrMeter increments a meter type metric as a default suffix "meter".
func IncrMeter(n int) *Value {
	return WithSuffix("meter").IncrMeter(n)
}

// IncrCounter increments a counter type metric as a default suffix "counter".
func IncrCounter(n int) *Value {
	return WithSuffix("counter").IncrCounter(n)
}

// Incrf increments a rate counter type metric as a default suffix "rate".
func Incrf(n float64) *Value {
	return WithSuffix("rate").Incrf(n)
}

// Observef increments a timer type metric as a default suffix "timer".
func Observef(n float64) *Value {
	return WithSuffix("timer").Observef(n)
}

// Storef store a metric as a default suffix "store".
func Storef(n float64) *Value {
	return WithSuffix("store").Storef(n)
}

// IncrMeterf increments a meter type metric as a default suffix "meter".
func IncrMeterf(n float64) *Value {
	return WithSuffix("meter").IncrMeterf(n)
}

// IncrCounterf increments a counter type metric as a default suffix "counter".
func IncrCounterf(n float64) *Value {
	return WithSuffix("counter").IncrCounterf(n)
}

// WithSuffix allows define a custom prefix to a metric,
// it would panic if suffix string is invalid.
func WithSuffix(s string) Measurer {
	m := measurePool.Get().(*measure)
	m.v.suffix = s
	m.v.mType = unknownType
	return m
}

type Value struct {
	suffix string
	mType  mType
	value  int64
	valuef float64
}

func (v *Value) equal(i interface{}) bool {
	return v.suffix == i.(*Value).suffix
}

type measure struct {
	v Value
}

func (m *measure) Incr(n int) *Value {
	return m.measure(rateCounterType, n, 0)
}

func (m *measure) Observe(n int) *Value {
	return m.measure(timerType, n, 0)
}

func (m *measure) Store(n int) *Value {
	return m.measure(storeType, n, math.Inf(-1))
}

func (m *measure) IncrMeter(n int) *Value {
	return m.measure(meterType, n, 0)
}

func (m *measure) IncrCounter(n int) *Value {
	return m.measure(counterType, n, 0)
}

func (m *measure) Incrf(n float64) *Value {
	return m.measure(rateCounterType, 0, n)
}

func (m *measure) Observef(n float64) *Value {
	return m.measure(timerType, 0, n)
}

func (m *measure) Storef(n float64) *Value {
	return m.measure(storeType, 0, n)
}

func (m *measure) IncrMeterf(n float64) *Value {
	return m.measure(meterType, 0, n)
}

func (m *measure) IncrCounterf(n float64) *Value {
	return m.measure(counterType, 0, n)
}

func (m *measure) measure(t mType, n int, nf float64) *Value {
	m.v.mType = t
	m.v.value = int64(n)
	m.v.valuef = nf
	return &m.v
}
