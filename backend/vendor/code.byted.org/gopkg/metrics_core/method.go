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

// Stat increments a histogram type metric as a default suffix "histogram".
func Stat(n int) *Value {
	return WithSuffix("histogram").Stat(n)
}

// Add increments a value as the delta_counter type metric as a default suffix "delta_counter"
func Add(n int) *Value {
	return WithSuffix("delta_counter").Add(n)
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

// Statf increments a histogram type metric as a default suffix "histogram".
func Statf(n float64) *Value {
	return WithSuffix("histogram").Statf(n)
}

// Addf increments a value as the delta_counter type metric as a default suffix "delta_counter"
func Addf(n float64) *Value {
	return WithSuffix("delta_counter").Addf(n)
}

// WithSuffix allows users to define a custom prefix to a metric,
// it would panic if suffix string is invalid.
func WithSuffix(s string) Measurer {
	m := measurePool.Get().(*measure)
	m.v.suffix = s
	m.v.mType = -1
	return m
}

// WithField allow users set the value of one field in a multi-field metric.
func WithField(s string) Measurer {
	m := measurePool.Get().(*measure)
	m.v.suffix = s
	m.v.mType = -1
	return m
}

type Value struct {
	suffix             string
	mType              mType
	value              int64
	valuef             float64
	buckets            []float64
	timerOption        *timerOption
	interval           int
	reportInitialValue bool
}

type measure struct {
	v Value
}

func (m *measure) Stat(n int) *Value {
	return m.measure(HistogramType, n, 0)
}

func (m *measure) Incr(n int) *Value {
	return m.measure(RateCounterType, n, 0)
}

func (m *measure) Observe(n int) *Value {
	return m.measure(TimerType, n, 0)
}

func (m *measure) Store(n int) *Value {
	return m.measure(StoreType, n, math.Inf(-1))
}

func (m *measure) IncrMeter(n int) *Value {
	return m.measure(MeterType, n, 0)
}

func (m *measure) IncrCounter(n int) *Value {
	return m.measure(CounterType, n, 0)
}

func (m *measure) Add(n int) *Value {
	return m.measure(DeltaCounterType, n, 0)
}

func (m *measure) Statf(n float64) *Value {
	return m.measure(HistogramType, 0, n)
}

func (m *measure) Incrf(n float64) *Value {
	return m.measure(RateCounterType, 0, n)
}

func (m *measure) Observef(n float64) *Value {
	return m.measure(TimerType, 0, n)
}

func (m *measure) Storef(n float64) *Value {
	return m.measure(StoreType, 0, n)
}

func (m *measure) IncrMeterf(n float64) *Value {
	return m.measure(MeterType, 0, n)
}

func (m *measure) IncrCounterf(n float64) *Value {
	return m.measure(CounterType, 0, n)
}

func (m *measure) Addf(n float64) *Value {
	return m.measure(DeltaCounterType, 0, n)
}

func (m *measure) measure(t mType, n int, nf float64) *Value {
	m.v.mType = t
	m.v.value = int64(n)
	m.v.valuef = nf
	return &m.v
}

// NewRateCounterField creates a RateCounter field
func NewRateCounterField(fieldName string) Field {
	return Field{Name: fieldName, Type: RateCounterType}
}

// NewCounterField creates a Counter field
func NewCounterField(fieldName string) Field {
	return Field{Name: fieldName, Type: CounterType}
}

// NewMeterField creates a MeterCounter field
func NewMeterField(fieldName string) Field {
	return Field{Name: fieldName, Type: MeterType}
}

// NewStoreField creates a Store field
func NewStoreField(fieldName string) Field {
	return Field{Name: fieldName, Type: StoreType}
}

// NewTimerField creates a Timer field
func NewTimerField(fieldName string) Field {
	return Field{Name: fieldName, Type: TimerType}
}

// NewHistogramField creates a Histogram field. Users need to provide the buckets.
func NewHistogramField(fieldName string, buckets []float64) Field {
	return Field{Name: fieldName, Type: HistogramType, Buckets: buckets}
}

// NewDeltaCounterField creates a DeltaCounter field. It will be expanded to three fields:
// fieldname.delta, fieldname.rate, fieldname.counter
func NewDeltaCounterField(fieldName string) Field {
	return Field{Name: fieldName, Type: DeltaCounterType}
}
