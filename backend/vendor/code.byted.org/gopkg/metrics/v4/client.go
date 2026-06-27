package metrics

import (
	"os"

	core "code.byted.org/gopkg/metrics_core"
)

const (
	errorPrefix = "[Metrics(v4)]"

	// LogLevelEnvKey is environment variable to set the default log level via
	LogLevelEnvKey = "METRICS_LOG_LEVEL"
)

var (

	// polishLog indicates whether to print the log in Prometheus style.
	polishLog = os.Getenv("METRICS_LOG_DETAIL") != ""

	// Debug is a trigger to toggle debug message or not.
	Debug     = os.Getenv("DEBUG_GOPKG_METRICS") != ""
	logPrefix = []byte("[Metrics(v4)] ")
)

// T is a tag struct.
type T = core.T

// TagIndex is a cached string slice,
// it was generated internally, user should use NewTagIndex to construct a TagIndex
type TagIndex = core.TagIndex

// Client is the metrics client instance in v4.
// Users can specify global tags when creating a Client.
// Users can also set the flush interval, tenant, watermark, vendor tag provider, sender,
// and some other properties of the client.
type Client = core.Client

// Metric is the metric instances, it contains a pre-declared tag names cache tree.
type Metric = core.Metric

// Emitter provide once emitting with variable methods.
type Emitter = core.Emitter

// Measurer provides supported metric methods.
type Measurer = core.Measurer

// NewClient creates a default client with agent writer.
func NewClient(prefix string, ops ...ClientOption) (Client, error) {
	allOps := append(defaultClientOptions, ops...)
	return core.GetDefaultSDK().NewClient(prefix, allOps...)
}

// Tag create metrics.T struct
func Tag(name, value string) T {
	return T{Name: name, Value: value}
}
