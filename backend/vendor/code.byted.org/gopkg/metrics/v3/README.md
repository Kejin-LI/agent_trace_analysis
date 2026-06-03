# Metrics V3

公司 metrics server client 的下一代 Go 实现。解决了 V2 中的丢点问题，并发性能较 V2 有明显的提升，同时还保留了一些动态能力，同时保证预聚合后的打点不会丢弃。V3 与 V2，V1 的 API 不兼容，可以与 V1 V2 一同使用。

## Install

`go get -u code.byted.org/gopkg/metrics/v3`

## Document

GoDoc: https://codebase.byted.org/godoc/code.byted.org/gopkg/metrics/v3/

Design Document: https://bytedance.feishu.cn/docs/doccnmZIWzCpNrJPQdrkGlIhm2e#

## Attention

The instance of `Metric` **HAVE TO BE a global variable**.

`Metric` 实例**必须是全局变量**，不可以在每次打点前临时新建 `Metric` 实例

关于此处 API 的设计可参考 [Prometheus Client API](https://github.com/prometheus/client_golang/blob/master/examples/random/main.go#L44) ，
与 Kubernetes 源码中如何使用 Prometheus Client 进行监控打点：[blob](https://github.com/kubernetes/kubernetes/blob/989b2fd3715d01a7757e891de2a17de5a5c2cc91/pkg/scheduler/metrics/metrics.go#L42) 。

You should flush the instance of `Metric` of `Client` to graceful exit.

如果进程将会快速退出，缓存的打点可能来不及发送，因此需要调用 `Client.Close` 方法发送所有缓存中的打点，已经关闭的 client 将无法继续提供打点服务。
如果只希望发送打点而不关闭 client，可以使用 `Client.Flush` 发送缓存打点。

## Example

```go
package main

import (
	"fmt"

	m "code.byted.org/gopkg/metrics/v3"
)

var client m.Client
var metric m.Metric

func init() {
	// Initialize client with options.
	client = m.NewClient(
		"metrics.sdk",
		// Client options.
		m.SetTceTags(), m.SetGlobalTags(m.T{Name: "hello", Value: "world"}),
	)

	// Declare a new metric with tag keys, tag values is not declaration required.
	// The instance of Metric **HAVE TO BE A GLOBAL VARIABLE**, 
	// do not new Metric every emits.
	metric = client.NewMetric("test", []string{"foo", "bar", "baz"}...)
}

func main() {

	// Send both timer and counter metrics with the same tag keys as above, tag value can be grabbed in runtime.
	tags := []m.T{m.T{Name: "foo", Value: "a"}, m.T{Name: "bar", Value: "b"}, m.T{Name: "baz", Value: "c"}}
	err := metric.WithTags(tags...).Emit(
		// A rate counter metric with the default suffix "rate"
		m.Incr(1),
		// Another rate counter type metric with the suffix "send-size" at the tail of the metric name
		m.WithSuffix("send_size").Incr(1),
		// A timer type metric with the suffix "latency" at the tail of the metric name
		m.WithSuffix("latency").Observe(100),
		// It is OK to use multiple counter metrics. 
		m.WithSuffix("recv_size").Incr(1),
	)
	if err != nil {
		fmt.Printf("Emit metrics error: %s", err.Error())
	}

    // Measure again with the same metric. 
    _ = metric.WithTags(tags...).Emit(
        m.Incr(1),
    )

    // Close client and graceful exit.
    client.Close()
}
```

### Effect

The example code emits metrics with the following names, which can be queried on metrics-fe or Grafana:

```
metrics.sdk.test.rate
                 ↑m.Incr(1)

metrics.sdk.test.send_size
                 ↑m.WithSuffix("send_size").Incr(1)
metrics.sdk.test.recv_size
                 ↑m.WithSuffix("recv_size").Incr(1)

metrics.sdk.test.latency.min
metrics.sdk.test.latency.max
metrics.sdk.test.latency.avg
metrics.sdk.test.latency.sum
metrics.sdk.test.latency.counter
metrics.sdk.test.latency.pct50
metrics.sdk.test.latency.pct90
metrics.sdk.test.latency.pct95
metrics.sdk.test.latency.pct99
                 ↑m.WithSuffix("latency").Observe(100)
                 See also https://site.bytedance.net/docs/2080/2717/36907/#%E6%B3%A8%E6%84%8F
```

The above metrics are all attached with 4 tags: `hello=world, foo=a, bar=b, baz=c`.
