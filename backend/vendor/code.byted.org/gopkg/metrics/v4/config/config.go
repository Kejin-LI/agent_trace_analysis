package config

import (
	"os"

	"code.byted.org/gopkg/metrics_core/config/providers"
)

const (
	ExportRemoteMS2      ExportRemoteType = "ms2"
	ExportRemoteOneAgent ExportRemoteType = "oneagent"
)

const (
	ConfigServerNone         ConfigServerType = "none"
	ConfigServerMetrics      ConfigServerType = "metrics"
	ConfigServerCloudMonitor ConfigServerType = "cloudmonitor"
)

var DefaultGlobalConfig = &GlobalConfig{}

type ExportRemoteType string

type ConfigServerType string

// GlobalConfig controls the behaviour of sdk.
// Unlike GlobalConfig in metrics_core, it can contain Bytedance-specific information.
type GlobalConfig struct {
	InTTP        bool `env:"METRICS_IN_TTP" envDefault:"false"`
	InGCP        bool `env:"METRICS_IN_GCP" envDefault:"false"`
	Exporting    ExportConfig
	ConfigServer ConfigServerType `env:"METRICS_CONFIG_SERVER" envDefault:"metrics"`
}

type ExportConfig struct {
	RemoteType ExportRemoteType `env:"METRICS_EXPORT_REMOTE_TYPE" envDefault:"ms2"`
}

func (c *GlobalConfig) GetExportRemoteEndpoints() []string {
	switch c.Exporting.RemoteType {
	case ExportRemoteMS2:
		if isAgentSidecar() {
			return []string{"/sidecar/sock/metric_seqpacket.sock"}
		} else {
			return []string{"/opt/tmp/sock/metric_seqpacket.sock"}
		}
	case ExportRemoteOneAgent:
		if isAgentSidecar() {
			return []string{"/sidecar/sock/oneagent_metric_seqpacket.sock"}
		} else {
			return []string{"/opt/tmp/sock/oneagent_metric_seqpacket.sock"}
		}
	default:
		return nil
	}
}

func isAgentSidecar() bool {
	if sidecar := os.Getenv("METRICSERVER2_SIDECAR"); sidecar == "1" {
		return true
	}

	if sidecar := os.Getenv("ONEAGENT_SIDECAR"); sidecar == "1" {
		return true
	}

	return false
}

func loadDefaultGlobalConfig() {
	providers.ParseFromEnv(DefaultGlobalConfig)
}

func init() {
	loadDefaultGlobalConfig()
}
