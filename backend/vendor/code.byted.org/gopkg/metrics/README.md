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
	// Important: keep elements of tags slice in the same order with in the slice passed to client.NewMetric(name,tags)'s second parameter
	// to get best performance
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
	
	// Or you can measure with only tag values.
	// Important: elements in the tagValues should keep in same order with  in the slice passed to client.NewMetric(name,tags)'s second parameter
	tagValues := []string{"a","b","c"}
	_ = metric.WithTagValues(tagValues...).Emit(
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

# Metrics

公司 metrics server client 的 Go 实现。

- 出于性能考虑数据异步，maxPendingSize=1000 or emitInterval=200ms 两个条件满足之一才发送；
    - 也可以使用 Flush 方法保证数据同步写到远程
- 在 `metrics.NewDefaultMetricsClientV2` 时指定 nocheck=true 可以忽略烦人的 DefineXXX 调用；
- Value 支持类型 float32 float64 int int8 int16 int32 int64 uint8 uint16 uint32 uint64 time.Duration
    - 其中 time.Duration 将表示为 nanosecond
- v1 与 v2 的区别 (MetricsClient vs MetricsClientV2):
    - v1 在 new 时要求namespace，而 emit 时除了 metrics name 还要输入 prefix，容易让人误用。v2 统一了在New时指定前缀，后面只能emit 后缀；
    - v1 tags 用的 map 结构，在高并发下，遍历map性能消耗高。v2 换成了 slice 而且如果没tags时可以省略；
    - 当前 v1 底层实际用了 v2 的逻辑，tags map 转 slice 时为了内部优化做了sort，有额外消耗;
    - 新的项目应该都使用 v2;
    - 对于v2: 请保证没有重复tag name，否则metrics查询出来的结果是未定义的；
- 性能优化提示
    - 对QPS高，又是动态tag组合的场景无解, 请确保tag组合是可以预定义的;
    - 通过 RegisterCounter / RegisterTimer 的方式预定义metrics，可以大量减少每次Emit导致的额外CPU计算消耗；
    - 如不通过预定义metrics，请尽量在业务侧对数据进行累加(如通过int64定期reset和emit);
    - 如不通过预定义metrics, 对于Timer类的数据，当前metrics server要求需要全部数据都发往server，而协议比较低效;
    - 重复: 请使用 MetricsClientV2, 而不是 MetricsClient, MetricsClient 主要为了兼容历史代码;
- 示例代码，详见 example/main.go
- [Metrics系统使用说明](https://bytedance.feishu.cn/docs/GHFmzle2R6a7cGvAqMWlbc#)
