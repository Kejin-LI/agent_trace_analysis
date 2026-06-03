package apm_vendor_interface

type MetricsConfig struct {
	Tenant              *string                `yaml:"Tenant"`              // Tenant Name
	IsTenantActive      *bool                  `yaml:"IsTenantActive"`      // Whether the tenant is active. SDK discards the metrics if the tenant is inactive.
	MinIntervalInSecond *int                   `yaml:"MinIntervalInSecond"` // Minimal Interval in second that the tenant configured
	Extras              map[string]interface{} `yaml:"Extras"`
}

type MetricsConfigProvider interface {
	GetConfig(tenant string) (*MetricsConfig, error)
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
