# Metrics Core
![coverage](https://badge.byted.org/ci/coverage/gopkg/metrics_core)

metrics_core是字节跳动内场metrics sdk：metrics/v4的核心模块，
定义了打点API，实现了内存索引结构以及发送逻辑。同时也支持注册Vendor信息，配置中心等能力。

## Install

`go get -u code.byted.org/gopkg/metrics_core`

## Document

GoDoc: https://codebase.byted.org/godoc/code.byted.org/gopkg/metrics_core



## Attention

The instance of `Metric` **HAVE TO BE a global variable**.

`Metric` 实例**必须是全局变量**，不可以在每次打点前临时新建 `Metric` 实例

关于此处 API 的设计可参考 [Prometheus Client API](https://github.com/prometheus/client_golang/blob/master/examples/random/main.go#L44) ，
与 Kubernetes 源码中如何使用 Prometheus Client 进行监控打点：[blob](https://github.com/kubernetes/kubernetes/blob/989b2fd3715d01a7757e891de2a17de5a5c2cc91/pkg/scheduler/metrics/metrics.go#L42) 。

You should flush the instance of `Metric` of `Client` to graceful exit.

如果进程将会快速退出，缓存的打点可能来不及发送，因此需要调用 `Client.Close` 方法发送所有缓存中的打点，已经关闭的 client 将无法继续提供打点服务。
如果只希望发送打点而不关闭 client，可以使用 `Client.Flush` 发送缓存打点。


## V3 to Metrics 2.0

### Features

1. Allow users to specify tenant info
2. Allow users to set the flush interval
3. Support Histogram type
4. Compact mode timer type with lower sending overhead
5. Support user-defined multi-field metric type


## Example

```go
package main

import (
	"fmt"
	"math/rand"
	"time"

	metrics "code.byted.org/gopkg/metrics_core"
)

var sdk metrics.SDK
var client metrics.Client
var metric metrics.Metric

func init() {
	var err error
	sdk = metrics.NewSDK()
	client, err = sdk.NewClient("metrics.sdk.demo", metrics.SetTenant("inf.metrics"))
	if err != nil {
		fmt.Printf("failed to create the client: %v\n", err)
	}
	metric, err = client.NewMetricWithOps("my.metric", []string{"tag0", "tag1", "tag2"},
		metrics.SetHistogramBucket(metrics.LinearBuckets(1, 2, 7)),
		metrics.SetMultiFieldTimer(),
	)

	if err != nil {
		fmt.Printf("failed to create the metric: %v\n", err)
	}

}

func main() {
	ticker := time.NewTicker(3 * time.Second)
	for _ = range ticker.C {
		err := metric.WithTags(
			metrics.T{"tag0", "value0"},
			metrics.T{"tag1", "value1"},
			metrics.T{"tag2", "value2"},
		).Emit(
			metrics.IncrCounter(rand.Intn(10)), // Counter类型
			metrics.Incr(rand.Intn(10)),        // RateCounter类型
			metrics.IncrMeter(rand.Intn(10)),   // Meter类型
			metrics.Observe(rand.Intn(500)),    // Timer类型
			metrics.Stat(rand.Intn(500)),       // Histogram类型
		)

		if err != nil {
			fmt.Printf("failed to emit metric: %v\n", err)
		}
	}
}


// Close client and graceful exit.
client.Close()
}
```

### Effect

The example code emits metrics with the following names, which can be queried on metrics-fe or Grafana:

```
metrics.sdk.demo.my.metric.store                 // a Store metric with default suffix
metrics.sdk.demo.my.metric.rate                 // a RateCounter metric with default suffix
metrics.sdk.demo.my.metric.meter                // a Counter metric from the meter metric
metrics.sdk.demo.my.metric.meter.rate           // a RateCounter metric from the meter metric
metrics.sdk.demo.my.metric.timer[min,           // a timer metric with ten fields 
                                 max,
                                 avg,
                                 sum, 
                                 counter, 
                                 pct50, 
                                 pct90, 
                                 pct95, 
                                 pct99, 
                                 pct999]
metrics.sdk.demo.my.metric.histogram[hist:b-1.0,    // a histogram with 9 fields
                                     hist:b-3.0,
                                     hist:b-5.0,
                                     hist:b-7.0,
                                     hist:b-9.0,
                                     hist:b-11.0,
                                     hist:b-13.0,
                                     hist:sum,
                                     hist:count]
                             
```





