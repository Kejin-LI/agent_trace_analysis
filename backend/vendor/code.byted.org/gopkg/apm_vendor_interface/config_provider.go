package apm_vendor_interface

import "net"

type MetricsConfig struct {
	Tenant              *string                `yaml:"Tenant"`              // Tenant Name
	IsTenantActive      *bool                  `yaml:"IsTenantActive"`      // Whether the tenant is active. SDK discards the metrics if the tenant is inactive.
	MinIntervalInSecond *int                   `yaml:"MinIntervalInSecond"` // Minimal Interval in second that the tenant configured
	Extras              map[string]interface{} `yaml:"Extras"`
}

type MetricsConfigProvider interface {
	GetConfig(tenant string) (*MetricsConfig, error)
}

// MetricsConfigService provides an interface for managing and retrieving metrics configurations.
type MetricsConfigService interface {
	MetricsConfigProvider

	// Description returns a brief, one-sentence description of the config service.
	Description() string

	// GetMeta retrieves the meta information associated with a key.
	// It returns the value of the meta information or an error if it fails to retrieve the value.
	GetMeta(key string) (string, error)

	// GetDestServiceInstances retrieves the instances of the destination service.
	// The 'options' parameter can be used to specify additional criteria for the instances to retrieve.
	// for example, tenant, dc.
	GetDestServiceInstances(options map[string]string) ([]Instance, error)

	// Refresh forces an update of the cached configuration data inside the config service.
	Refresh() error
}

// Instance contains information of an instance from the target service.
type Instance interface {
	Address() net.Addr
	Weight() int
	Tag(key string) (value string, exist bool)
}

func StringReference(s string) *string {
	return &s
}

func IntReference(i int) *int {
	return &i
}

func BoolReference(b bool) *bool {
	return &b
}
