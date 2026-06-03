package bmetrics

import (
	"sync/atomic"
	"time"

	"code.byted.org/gopkg/env"
	"code.byted.org/gopkg/logs"
	"code.byted.org/gopkg/metrics"
)

type ClientV2 struct {
	client *metrics.MetricsClientV2 //
	errMax int64                    //错误最多打印次数
	errNow int64                    //累计错误数
}

func newClientV2(psm string, errMax int) *ClientV2 {
	t := &ClientV2{
		errMax: int64(errMax),
	}
	if psm != "" {
		t.client = metrics.NewDefaultMetricsClientV2(psm, true)
	}
	return t
}

func (t *ClientV2) Flush() {
	t.client.Flush()
}

func (t *ClientV2) EmitCounter(name string, value interface{}, tags ...metrics.T) {
	if t.client == nil {
		return
	}
	err := t.client.EmitCounter(name, value, tags...)
	if err != nil {
		t.printErr(name, err)
	}
}

func (t *ClientV2) EmitRateCounter(name string, value interface{}, tags ...metrics.T) {
	if t.client == nil {
		return
	}
	err := t.client.EmitRateCounter(name, value, tags...)
	if err != nil {
		t.printErr(name, err)
	}
}

func (t *ClientV2) EmitMeter(name string, value interface{}, tags ...metrics.T) {
	if t.client == nil {
		return
	}
	err := t.client.EmitMeter(name, value, tags...)
	if err != nil {
		t.printErr(name, err)
	}
}

func (t *ClientV2) EmitTimer(name string, value interface{}, tags ...metrics.T) {
	if t.client == nil {
		return
	}
	if t0, ok := value.(time.Time); ok { //特殊转换为微妙
		value = time.Since(t0).Nanoseconds() / 1000
	} else if t0, ok := value.(time.Duration); ok { //特殊转换为微妙
		value = int(t0.Nanoseconds()) / 1000
	}
	err := t.client.EmitTimer(name, value, tags...)
	if err != nil {
		t.printErr(name, err)
	}
}

func (t *ClientV2) EmitStore(name string, value interface{}, tags ...metrics.T) {
	if t.client == nil {
		return
	}
	err := t.client.EmitStore(name, value, tags...)
	if err != nil {
		t.printErr(name, err)
	}
}

func (t *ClientV2) printErr(name string, err error) {
	count := atomic.AddInt64(&t.errNow, int64(1))
	if count <= t.errMax {
		logs.Error("bmetrics name=%v err=%v", name, err)
	}
}

func (t *ClientV2) EmitError(msg string) {
	t.EmitCounter("go.error", 1, Tag("cluster", env.Cluster()), Tag("env", env.Env()), Tag("msg", msg))
}

func (t *ClientV2) EmitWarn(msg string) {
	t.EmitCounter("go.warn", 1, Tag("cluster", env.Cluster()), Tag("env", env.Env()), Tag("msg", msg))
}

func (t *ClientV2) EmitAlarm(msg string) {
	t.EmitCounter("go.alarm", 1, Tag("cluster", env.Cluster()), Tag("env", env.Env()), Tag("msg", msg))
}

// 通过defer使用
func (t *ClientV2) EmitFunc(module string, key string, err *error, timeout time.Duration, t0 time.Time) {
	if module == "" {
		module = "none"
	}
	if key == "" {
		logs.Error("bmetrics EmitFunc empty key")
		return
	}

	isTimeout := false
	if timeout > 0 {
		if cost := time.Since(t0); cost >= timeout {
			isTimeout = true
			logs.Warn("bmetrics EmitFunc cost=%v timeout=%v module=%v key=%v", cost, timeout, module, key)
		}
	}

	isSuccess := true
	if *err != nil {
		isSuccess = false
		// logs.Warn("bmetrics EmitFunc err=%v module=%v key=%v", *err, module, key)
	}

	tags := []T{
		Tag("cluster", env.Cluster()),
		Tag("env", env.Env()),
		Tag("module", module),
		Tag("key", key),
		Tag("success", isSuccess),
		Tag("timeout", isTimeout),
	}
	t.EmitCounter("go.func.throughput", 1, tags...)
	t.EmitTimer("go.func.latency", t0, tags...)
}

func (t *ClientV2) EmitGoStore(name string, value int) {
	t.EmitStore("go.store", value, Tag("cluster", env.Cluster()), Tag("env", env.Env()), Tag("name", name))
}

func (t *ClientV2) EmitGoCounter(name string, value int) {
	t.EmitCounter("go.counter", value, Tag("cluster", env.Cluster()), Tag("env", env.Env()), Tag("name", name))
}

func (t *ClientV2) EmitGoTimer(name string, value interface{}) {
	t.EmitTimer("go.timer", value, Tag("cluster", env.Cluster()), Tag("env", env.Env()), Tag("name", name))
}
