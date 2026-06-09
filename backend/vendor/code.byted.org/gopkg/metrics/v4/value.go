package metrics

import (
	core "code.byted.org/gopkg/metrics_core"
)

const (
	StoreType        = core.StoreType
	RateCounterType  = core.RateCounterType
	CounterType      = core.CounterType
	MeterType        = core.MeterType
	TimerType        = core.TimerType
	HistogramType    = core.HistogramType
	DeltaCounterType = core.DeltaCounterType
)

type Field = core.Field
