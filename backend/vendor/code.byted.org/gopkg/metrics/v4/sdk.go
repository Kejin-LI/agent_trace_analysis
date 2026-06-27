package metrics

import (
	"strings"

	"code.byted.org/aiops/apm_vendor_byted/config_service"
	"code.byted.org/aiops/apm_vendor_byted/vendor_tags"
	"code.byted.org/gopkg/apm_vendor_interface"
	"code.byted.org/gopkg/metrics/v4/config"
	core "code.byted.org/gopkg/metrics_core"
	coreConfig "code.byted.org/gopkg/metrics_core/config"
)

type SDK = core.SDK
type SDKOption = core.SDKOption
type GlobalConfig = coreConfig.GlobalConfig
type GlobalConfigOption = coreConfig.GlobalConfigOption

// NewSDK allows users to create a customized SDK instance.
// It uses the default bytedance vendor provider and config provider implementation.
// Users can also manually set the vendor tag provider, config provider, and some other configurations.
func NewSDK(ops ...SDKOption) SDK {
	allOps := append(defaultOptions, ops...)
	return core.NewSDK(allOps...)
}

// SetVendorTagsProvider sets the vendor tag provider when initializing the sdk.
func SetVendorTagsProvider(provider apm_vendor_interface.VendorTagsProvider) SDKOption {
	return core.SetVendorTagsProvider(provider)
}

// SetMetricsConfigProvider sets the metrics config provider when initializing the sdk.
// SDK will not emit the metrics if there is no valid config provider.
func SetMetricsConfigProvider(providers ...apm_vendor_interface.MetricsConfigProvider) SDKOption {
	return core.SetMetricsConfigProvider(providers...)
}

// SetStaticConfig sets the static config the sdk, i.e., DebugMode, SDKSelfMonitor.
func SetStaticConfig(ops ...GlobalConfigOption) SDKOption {
	return core.SetStaticConfig(ops...)
}

// SetKeepClosedClient will keep clients in sdk even if the client is closed.
func SetKeepClosedClient() SDKOption {
	return core.SetKeepClosedClient()
}

func SetSDKCodec(codec CodecVersion) SDKOption {
	return core.SetSDKCodec(codec)
}

var defaultOptions []SDKOption
var defaultClientOptions []ClientOption

func init() {
	vendorTagProvider := vendor_tags.NewBytedVendorTagsProvider()
	var configProvider apm_vendor_interface.MetricsConfigProvider = &noopMetricConfigProvider{}
	switch config.DefaultGlobalConfig.ConfigServer {
	case config.ConfigServerMetrics:
		configSvrOptions := make([]config_service.Option, 0)

		// no need to check the region here, vendor will do it.
		if config.DefaultGlobalConfig.InTTP {
			configSvrOptions = append(configSvrOptions, config_service.SetInTTP())
		} else if config.DefaultGlobalConfig.InGCP {
			configSvrOptions = append(configSvrOptions, config_service.SetInGCP())
		}
		configProvider = config_service.NewBytedMetricConfigProvider(configSvrOptions...)
	case config.ConfigServerCloudMonitor:
	}

	sdkVersion := getVersionInternal("code.byted.org/gopkg/metrics/v4")
	defaultOptions = append(defaultOptions,
		core.SetVersion(sdkVersion), // set tag of metrics/v4 as the sdk version.
		SetVendorTagsProvider(vendorTagProvider),
		SetMetricsConfigProvider(configProvider),
		SetStaticConfig(
			func(conf *GlobalConfig) {
				// set the default endpoint
				remoteEndpoints := config.DefaultGlobalConfig.GetExportRemoteEndpoints()
				if len(remoteEndpoints) > 0 {
					conf.Exporting.DefaultTransport.Addrs = strings.Join(remoteEndpoints, ",")
				}
			}),
	)

	defaultByteDSDK := NewSDK()
	core.SetDefaultSDK(defaultByteDSDK)
	defaultClientOptions = append(defaultClientOptions, SetReportInitialCounter(), SetReportUnchangedCounter(false))
}

type noopMetricConfigProvider struct{}

func (p *noopMetricConfigProvider) GetConfig(tenant string) (*apm_vendor_interface.MetricsConfig, error) {
	return &apm_vendor_interface.MetricsConfig{
		Tenant:              apm_vendor_interface.StringReference(tenant),
		IsTenantActive:      apm_vendor_interface.BoolReference(true),
		MinIntervalInSecond: apm_vendor_interface.IntReference(30),
		Extras:              nil,
	}, nil
}
