package config

import (
	"code.byted.org/gopkg/apm_vendor_interface"
)

// ConfigListener implements the following two methods
// 1. it can tell the config provider manager its tenent.
// 2. it can receive new metric config to update.
type ConfigListener interface {
	GetTenant() string
	Load(config *apm_vendor_interface.MetricsConfig)
}
