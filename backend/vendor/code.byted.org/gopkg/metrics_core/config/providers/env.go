package providers

import (
	"github.com/caarlos0/env/v6"

	"code.byted.org/gopkg/apm_vendor_interface"
)

type envConfigProvider struct {
}

func NewEnvConfigProvider() apm_vendor_interface.MetricsConfigProvider {
	return &envConfigProvider{}
}

func ParseFromEnv(conf interface{}) error {
	return env.Parse(conf)
}

func (e *envConfigProvider) GetConfig(tenant string) (*apm_vendor_interface.MetricsConfig, error) {
	conf := &apm_vendor_interface.MetricsConfig{}
	err := ParseFromEnv(conf)
	return conf, err
}
