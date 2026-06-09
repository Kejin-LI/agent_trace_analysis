package metrics

import (
	core "code.byted.org/gopkg/metrics_core"
)

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
	return core.WithSuffix(s)
}

// WithField allow users set the value of one field in a multi-field metric.
func WithField(s string) Measurer {
	return core.WithField(s)
}

type Value = core.Value

// NewRateCounterField creates a RateCounter field
func NewRateCounterField(fieldName string) Field {
	return core.NewRateCounterField(fieldName)
}

// NewCounterField creates a Counter field
func NewCounterField(fieldName string) Field {
	return core.NewCounterField(fieldName)
}

// NewMeterField creates a MeterCounter field
func NewMeterField(fieldName string) Field {
	return core.NewMeterField(fieldName)
}

// NewStoreField creates a Store field
func NewStoreField(fieldName string) Field {
	return core.NewStoreField(fieldName)
}

// NewTimerField creates a Timer field
func NewTimerField(fieldName string) Field {
	return core.NewTimerField(fieldName)
}

// NewHistogramField creates a Histogram field. Users need to provide the buckets.
func NewHistogramField(fieldName string, buckets []float64) Field {
	return core.NewHistogramField(fieldName, buckets)
}

// NewDeltaCounterField creates a DeltaCounter field. It will be expanded to three fields:
// fieldname.delta, fieldname.rate, fieldname.counter
func NewDeltaCounterField(fieldName string) Field {
	return core.NewDeltaCounterField(fieldName)
}
