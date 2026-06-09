package config

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"code.byted.org/gopkg/apm_vendor_interface"
	"code.byted.org/gopkg/metrics_core/logs"
)

const (
	defaultTenant          = "default"
	defaultIntervalInSec   = 30
	defaultRefreshInterval = time.Minute * 30
	defaultCacheLiveTime   = time.Minute * 30
)

var (
	validDefaultIntervals = []int{30, 60, 300, 600, 900}
)

type config struct {
	refreshInterval time.Duration
	cacheLivetime   time.Duration
}

type Option func(conf *config)

func SetRefreshInterval(interval time.Duration) Option {
	return func(conf *config) {
		if interval > 0 {
			conf.refreshInterval = interval
		}
	}
}

func SetCacheExpiration(liveTime time.Duration) Option {
	return func(conf *config) {
		if liveTime > 0 {
			conf.cacheLivetime = liveTime
		}
	}
}

// MetricsConfigProviderManager is responsible for
// register metrics clients and notify these clients
// when their configuration is updated.
type MetricsConfigProviderManager struct {
	conf config
	sync.RWMutex
	sync.WaitGroup
	cancelFunc    context.CancelFunc
	providers     []apm_vendor_interface.MetricsConfigProvider
	configService apm_vendor_interface.MetricsConfigService

	// key is tenant
	configCache         map[string]*cacheConfig
	registeredListeners map[string][]ConfigListener
}

type cacheConfig struct {
	updateBy time.Time
	config   *apm_vendor_interface.MetricsConfig
}

func NewMetricsConfigProviderManager(ops ...Option) *MetricsConfigProviderManager {
	ctx, cancelFunc := context.WithCancel(context.TODO())
	m := &MetricsConfigProviderManager{
		conf:                config{refreshInterval: defaultRefreshInterval, cacheLivetime: defaultCacheLiveTime},
		cancelFunc:          cancelFunc,
		configCache:         make(map[string]*cacheConfig),
		registeredListeners: make(map[string][]ConfigListener),
	}

	for _, op := range ops {
		op(&m.conf)
	}

	m.Add(1)
	go m.run(ctx)
	return m
}

func (m *MetricsConfigProviderManager) RegisterConfigServiceProvider(provider apm_vendor_interface.MetricsConfigProvider) {
	if cfgSvr, ok := provider.(apm_vendor_interface.MetricsConfigService); ok {
		m.RegisterConfigService(cfgSvr)
	}

	m.Lock()
	defer m.Unlock()
	if m.exists(provider) {
		return
	}
	m.providers = append(m.providers, provider)
}

func (m *MetricsConfigProviderManager) RegisterListener(ln ConfigListener) {
	m.Lock()
	defer m.Unlock()
	m.registeredListeners[ln.GetTenant()] = append(m.registeredListeners[ln.GetTenant()], ln)
}

func (m *MetricsConfigProviderManager) RegisterConfigService(service apm_vendor_interface.MetricsConfigService) {
	m.Lock()
	defer m.Unlock()
	m.configService = service
}

func (m *MetricsConfigProviderManager) GetConfigService() apm_vendor_interface.MetricsConfigService {
	m.Lock()
	defer m.Unlock()
	return m.configService
}

// GetConfig manually fetches the configuration of a specific tenant.
func (m *MetricsConfigProviderManager) GetConfig(tenant string) (*apm_vendor_interface.MetricsConfig, error) {
	m.Lock()
	defer m.Unlock()
	return m.getConfig(tenant)
}

// getConfig fetches the config in the cache or via config providers.
// note that the lock is acquired during this function.
func (m *MetricsConfigProviderManager) getConfig(tenant string) (*apm_vendor_interface.MetricsConfig, error) {
	conf, ok := m.configCache[tenant]
	now := time.Now()

	if ok && conf != nil {
		if now.Sub(conf.updateBy) < m.conf.cacheLivetime {
			return conf.config, nil
		}
	}

	if len(m.providers) != 0 {
		atLeastOneProviderWorks := false
		conf := &apm_vendor_interface.MetricsConfig{
			Tenant: apm_vendor_interface.StringReference(tenant),
		}

		for i, provider := range m.providers {
			c, err := provider.GetConfig(tenant)

			if err != nil {
				logs.Warn("failed to get the config from provider %d: %v", i, err)
				continue
			}
			atLeastOneProviderWorks = true

			if c.IsTenantActive != nil {
				conf.IsTenantActive = c.IsTenantActive
			}

			if c.MinIntervalInSecond != nil {
				conf.MinIntervalInSecond = c.MinIntervalInSecond
			}

			if c.Extras != nil {
				if conf.Extras == nil {
					conf.Extras = make(map[string]interface{})
				}

				for k, v := range c.Extras {
					conf.Extras[k] = v
				}
			}
		}

		if !atLeastOneProviderWorks {
			return nil, fmt.Errorf("all config provider doesn't work")
		}

		m.configCache[tenant] = &cacheConfig{time.Now(), conf}
		return conf, nil
	}

	return nil, fmt.Errorf("metrics config provider manager has no available config providers")
}

func (m *MetricsConfigProviderManager) Notify(conf *apm_vendor_interface.MetricsConfig, listeners ...ConfigListener) {
	for _, ln := range listeners {
		ln.Load(conf)
	}
}

func (m *MetricsConfigProviderManager) Stop() {
	m.cancelFunc()
	m.Wait()
	logs.Warn("metrics config provider manager gracefully exited")
}

func (m *MetricsConfigProviderManager) run(ctx context.Context) {
	defer m.Done()
	ticker := time.NewTicker(m.conf.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Lock()
			for tenant, _ := range m.registeredListeners {
				conf, err := m.getConfig(tenant)

				if err != nil {
					logs.Warn("failed to get configuration for tenant: %s due to %v", tenant, err)
					continue
				}

				if !reflect.DeepEqual(conf, m.configCache[tenant].config) {
					m.configCache[tenant] = &cacheConfig{time.Now(), conf}
					listeners := m.registeredListeners[tenant]

					m.Notify(conf, listeners...)
				}
			}
			m.Unlock()
		}

	}
}

func (m *MetricsConfigProviderManager) exists(provider apm_vendor_interface.MetricsConfigProvider) bool {
	for _, configProvider := range m.providers {
		if configProvider == provider {
			return true
		}
	}
	return false
}
