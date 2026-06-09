package config

import (
	"log"

	"github.com/mohae/deepcopy"

	"code.byted.org/gopkg/metrics_core/config/providers"
	"code.byted.org/gopkg/metrics_core/utils"
)

type GlobalConfig struct {
	DebugMode           bool `env:"DEBUG_GOPKG_METRICS" envDefault:"false"`
	SelfMonitor         SelfMonitorConfig
	ResourcesLimit      ResourcesLimitConfig
	Logging             LoggingConfig
	Exporting           ExportConfig
	DefaultTenant       string `env:"METRICS_DEFAULT_TENANT" envDefault:"default"`
	DefaultInterval     int    `env:"METRICS_DEFAULT_INTERVAL_IN_SECOND" envDefault:"30"`
	RemoveClosedClients bool   `env:"METRICS_CLEAN_CLIENT" envDefault:"true"`
}

func NewSDKConfig() *GlobalConfig {
	conf := &GlobalConfig{
		SelfMonitor: SelfMonitorConfig{
			Enabled:    false,
			GlobalTags: map[string]string{},
		},
		ResourcesLimit: ResourcesLimitConfig{
			MaxBatchSize:        128 * 1024,
			MaxMetricsPerClient: 1024,
			MaxFieldsPerMetric:  1024,
		},
		Logging: LoggingConfig{
			LogLevel: "info",
		},
		DefaultTenant:   defaultTenant,
		DefaultInterval: defaultIntervalInSec,
		Exporting: ExportConfig{
			DefaultTransport: TransportConfig{
				Addrs:   "/tmp/sock/metric_seqpacket.sock",
				Network: "unixpacket",
				ConnPooling: ConnectionPoolConfig{
					InitialCap: 1,
					MaxCap:     -1,
					MaxIdle:    1024,
					Adjust: ConnectionAdjustConfig{
						IntervalSec: 5,
						Threshold:   10,
						Step:        0.1,
					},
				},
			},
		},
	}
	err := providers.ParseFromEnv(conf)
	if err != nil {
		log.Printf("failed to parse config from env: %v", err)
	}

	if !utils.Contains(validDefaultIntervals, conf.DefaultInterval) {
		log.Printf("set an invalid time interval: %d, reset to %d.", conf.DefaultInterval, defaultIntervalInSec)
		conf.DefaultInterval = defaultIntervalInSec
	}
	return conf
}

func (s *GlobalConfig) UpdateSDKConfig(ops ...GlobalConfigOption) {
	for _, op := range ops {
		op(s)
	}
}

func (s *GlobalConfig) Copy() GlobalConfig {
	return deepcopy.Copy(*s).(GlobalConfig)
}

type SelfMonitorConfig struct {
	Enabled    bool `env:"METRICS_ENABLE_MONITOR" envDefault:"false"`
	GlobalTags map[string]string
}

type ResourcesLimitConfig struct {
	MaxBatchSize        int `env:"METRICS_MAX_BATCH_SIZE" envDefault:"131072"` // 128k
	MaxMetricsPerClient int `env:"METRICS_MAX_METRICS_PER_CLIENT" envDefault:"1024"`
	MaxFieldsPerMetric  int `env:"METRICS_MAX_FIELDS_PER_METRIC" envDefault:"1024"`
}

type LoggingConfig struct {
	LogLevel    string `env:"METRICS_LOG_LEVEL" envDefault:"info"`
	PolishStyle bool   `env:"METRICS_LOG_DETAIL" envDefault:"false"`
	FilePath    string `env:"METRICS_LOG_FILE"`
}

type ExportConfig struct {
	DefaultTransport TransportConfig
}

type TransportConfig struct {
	Addrs       string `env:"METRICS_EXPORT_TRANSPORT_ADDR" envDefault:"/opt/tmp/sock/metric_seqpacket.sock"` // multi addrs split by ','
	Network     string `env:"METRICS_EXPORT_TRANSPORT_NETWORK" envDefault:"unixpacket"`
	ConnPooling ConnectionPoolConfig
}

type ConnectionPoolConfig struct {
	MaxCap     int `env:"METRICS_EXPORT_TRANSPORT_POOL_MAXCAP" envDefault:"-1"` // -1, means use real writer refs count as MaxCap
	MaxIdle    int `env:"METRICS_EXPORT_TRANSPORT_POOL_MAXIDLE" envDefault:"1024"`
	InitialCap int `env:"METRICS_EXPORT_TRANSPORT_POOL_INITIALCAP" envDefault:"1"`
	Adjust     ConnectionAdjustConfig
}

type ConnectionAdjustConfig struct {
	IntervalSec int     `env:"METRICS_EXPORT_TRANSPORT_POOL_ADJUST_INTERVAL" envDefault:"5"`   // -1, means use the tenant interval
	Threshold   int     `env:"METRICS_EXPORT_TRANSPORT_POOL_ADJUST_THRESHOLD" envDefault:"10"` // -1, means disable the background periodically adjust
	Step        float64 `env:"METRICS_EXPORT_TRANSPORT_POOL_ADJUST_STEP" envDefault:"0.1"`     // means how many(<1:percent; >=1:num) connections will be injected in one adjust
}

type GlobalConfigOption func(conf *GlobalConfig)
