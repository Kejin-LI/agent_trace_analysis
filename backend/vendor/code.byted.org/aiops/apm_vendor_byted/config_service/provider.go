package config_service

import (
	"sync"
	"time"

	"code.byted.org/aiops/apm_vendor_byted/vendor_tags"
	apm_vendor "code.byted.org/gopkg/apm_vendor_interface"
	"code.byted.org/gopkg/env"
)

var (
	defaultMetricsConfig = apm_vendor.MetricsConfig{
		Tenant:              apm_vendor.StringReference("default"),
		IsTenantActive:      apm_vendor.BoolReference(true),
		MinIntervalInSecond: apm_vendor.IntReference(30),
		Extras:              nil,
	}
)

type config struct {
	dc          string
	cacheConfig bool
	retryTimes  int
	timeout     time.Duration
	regionType  RegionType
	inBoe       bool
}

func (conf *config) inTTP() bool {
	return conf.regionType == REGION_TTP
}

func (conf *config) inGCP() bool {
	return conf.regionType == REGION_GCP
}

type Option func(conf *config)

func SetRetryTimes(times int) Option {
	return func(conf *config) {
		if times > 0 {
			conf.retryTimes = times
		}
	}
}

func SetHttpClientTimeout(duration time.Duration) Option {
	return func(conf *config) {
		if duration > 0 {
			conf.timeout = duration
		}
	}
}

func SetCacheConfig(isEnable bool) Option {
	return func(conf *config) {
		conf.cacheConfig = isEnable
	}
}

func SetDC(dc string) Option {
	return func(conf *config) {
		if len(dc) > 0 {
			conf.dc = dc
		}
	}
}

func SetInTTP() Option {
	return func(conf *config) {
		conf.regionType = REGION_TTP
	}
}

func SetInGCP() Option {
	return func(conf *config) {
		conf.regionType = REGION_GCP
	}
}

func SetTnBoe() Option {
	return func(conf *config) {
		conf.inBoe = true
	}
}

type BytedMetricsConfigProvider struct {
	conf config
	sync.RWMutex

	cli    *metricsHttpClient
	caches map[string]*apm_vendor.MetricsConfig
}

func NewBytedMetricConfigProvider(ops ...Option) *BytedMetricsConfigProvider {
	p := &BytedMetricsConfigProvider{
		conf: config{
			dc:          vendor_tags.GetDC(),
			cacheConfig: true,
			retryTimes:  defaultRetryTimes,
			timeout:     defaultTimeOut,
			regionType:  DetermineRegion(),
			inBoe:       env.IsBoe(),
		},
		caches: make(map[string]*apm_vendor.MetricsConfig),
	}

	for _, op := range ops {
		op(&p.conf)
	}

	p.cli = newMetricsHttpClientWithConf(&p.conf)
	return p
}

func (p *BytedMetricsConfigProvider) GetConfig(tenant string) (*apm_vendor.MetricsConfig, error) {
	if tenant == "default" {
		return &defaultMetricsConfig, nil
	}

	p.RLock()
	if conf, ok := p.caches[tenant]; ok {
		// Users may update the tenant configuration after their service deployment.
		if conf != nil && conf.IsTenantActive != nil && (*conf.IsTenantActive) == true {
			p.RUnlock()
			return conf, nil
		}
	}
	p.RUnlock()

	p.Lock()
	defer p.Unlock()
	interval, err := p.cli.getMinInterval(tenant)
	if err != nil {
		return nil, err
	}

	conf := &apm_vendor.MetricsConfig{
		Tenant:              apm_vendor.StringReference(tenant),
		IsTenantActive:      apm_vendor.BoolReference(true),
		MinIntervalInSecond: apm_vendor.IntReference(interval),
		Extras:              nil,
	}

	if p.conf.cacheConfig {
		p.caches[tenant] = conf
	}
	return conf, nil
}

func (p *BytedMetricsConfigProvider) GetProducerDC() (string, error) {
	return p.cli.getProducerDC()
}

func (p *BytedMetricsConfigProvider) GetProducer() (string, error) {
	return p.cli.getProducer()
}

func (p *BytedMetricsConfigProvider) GetProducerEndpoints() ([]string, error) {
	return p.cli.getProducerEndpoints()
}
