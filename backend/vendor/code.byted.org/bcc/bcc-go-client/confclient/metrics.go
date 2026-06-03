package confclient

import (
	"code.byted.org/bcc/bcc-go-client"
	metrics "code.byted.org/bcc/tools/bmetrics/v3"
	m "code.byted.org/gopkg/metrics/v3"
)

var metricClient *metrics.MetricsClient

var (
	useNewCoreClient  = "use.newcore.client" // with ns suffix
	slaRequest        = "sla.request.throughput"
	slaRequestError   = "sla.request.error"
	confParseError    = "parse.error"
	confCallbackError = "callback.error" // with ns suffix
	confWatchNum      = "watch.num"
	nsConfWatchNum    = "ns.watch.num" // with ns suffix
	confVersion       = "version"      // with ns suffix
	confUpdate        = "update"       // with ns suffix
	modelUnmarshalErr = "model.unmarshal.error"
	watchError        = "watch.error"
)

func init() {
	metricClient = metrics.NewMetricsClient(
		metrics.WithPrefix("bytedance.bcc.sdk.conf"),
		metrics.WithMetrics(map[string][]string{
			useNewCoreClient:  {"ns", "sdk_version"},
			slaRequest:        {"ns", "sdk_version"},
			slaRequestError:   {"ns", "sdk_version"},
			confParseError:    {"bc_key", "ns", "sdk_version", "reason"},
			confCallbackError: {"bc_key", "sdk_version", "reason"},
			confWatchNum:      {"ns", "sdk_version", "watch_type"},
			nsConfWatchNum:    {"ns", "key", "sdk_version", "watch_type"},
			confVersion:       {"conf", "client_name"},
			confUpdate:        {"bc_key", "version"},
			modelUnmarshalErr: {"ns", "bc_key"},
			watchError:        {"ns", "watch_type", "err_type"},
		}),
	)
}
func emitUseNewCoreClient(namespace string) {
	tags := []m.T{
		metrics.Tag("ns", namespace),
		metrics.Tag("sdk_version", bcc.SDKVersion()),
	}
	metricClient.EmitCounterWithSuffix(useNewCoreClient, namespace, 1, tags...)
}

// timer
func emitSLARequestThroughput(namespace string, cnt int) {
	tags := []m.T{
		metrics.Tag("ns", namespace),
		metrics.Tag("sdk_version", bcc.SDKVersion()),
	}
	metricClient.EmitCounter(slaRequest, cnt, tags...)
}

func emitConfParseError(ns, bcKey, reason string) {
	tags := []m.T{
		metrics.Tag("bc_key", bcKey),
		metrics.Tag("ns", ns),
		metrics.Tag("sdk_version", bcc.SDKVersion()),
		metrics.Tag("reason", reason),
	}
	metricClient.EmitCounter(confParseError, 1, tags...)
}

func emitModelUnmarshalError(ns, bcKey string) {
	tags := []m.T{
		metrics.Tag("ns", ns),
		metrics.Tag("bc_key", bcKey),
	}
	metricClient.EmitCounter(modelUnmarshalErr, 1, tags...)
}

func emitCallbackError(ns, bcKey, reason string) {
	tags := []m.T{
		metrics.Tag("bc_key", bcKey),
		metrics.Tag("sdk_version", bcc.SDKVersion()),
		metrics.Tag("reason", reason),
	}
	metricClient.EmitCounterWithSuffix(confCallbackError, ns, 1, tags...)
}

func emitConfWatchNum(ns, key, watchType string) {
	if ns == "" {
		return
	}
	tags := []m.T{
		metrics.Tag("ns", ns),
		metrics.Tag("sdk_version", bcc.SDKVersion()),
		metrics.Tag("watch_type", watchType),
	}
	metricClient.EmitCounter(confWatchNum, 1, tags...)
	if key == "" {
		return
	}
	nsTags := []m.T{
		metrics.Tag("key", key),
		metrics.Tag("sdk_version", bcc.SDKVersion()),
		metrics.Tag("watch_type", watchType),
	}
	metricClient.EmitCounterWithSuffix(nsConfWatchNum, ns, 1, nsTags...)
}

// timer
func emitConfVersion(clientName, ns, bcKey string, version int64) {
	tags := []m.T{
		metrics.Tag("client_name", clientName),
		metrics.Tag("conf", bcKey),
		//SDKVersion信息也是比较重要的，但这个tag会使得tagkv数据量变得大许多。为此这里就不打印了。
		//由于比较重要，在排查的时候可能需要用到。此时可以通过emitWatchNum、emitSlaRequestThroughput结合pod_name
		//查询到某个pod使用的sdk_version信息。
		//bmetrics.Tag("sdk_version", SDKVersion()),
	}

	metricClient.EmitStoreWithSuffix(confVersion, ns, int(version), tags...)
}

func emitConfUpdate(ns, bcKey string, version int64) {
	tags := []m.T{
		metrics.Tag("bc_key", bcKey),
		metrics.Tag("version", version),
	}
	metricClient.EmitCounterWithSuffix(confUpdate, ns, 1, tags...)
}

func emitWatchError(ns, watchType, errType string, count int) {
	tags := []m.T{
		metrics.Tag("ns", ns),
		metrics.Tag("watch_type", watchType),
		metrics.Tag("err_type", errType),
	}
	metricClient.EmitCounter(watchError, count, tags...)
}
