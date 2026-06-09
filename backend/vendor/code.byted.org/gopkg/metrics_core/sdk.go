package metrics

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"code.byted.org/gopkg/apm_vendor_interface"
	"code.byted.org/gopkg/metrics_core/config"
	"code.byted.org/gopkg/metrics_core/logs"
	"code.byted.org/gopkg/metrics_core/utils"
	"code.byted.org/gopkg/metrics_core/writers"
)

var (
	sdk_id     int64
	defaultSDK atomic.Value
)

func init() {
	defaultConfigProvider := &NoopMetricConfigProvider{}
	defaultSDK.Store(NewSDK(SetMetricsConfigProvider(defaultConfigProvider)))
}

// SetDefaultSDK registers newSDK as the global default SDK.
func SetDefaultSDK(newSDK SDK) {
	current := GetDefaultSDK()

	if newSDK != nil && newSDK != current {
		defaultSDK.Store(newSDK)
	}
}

// GetDefaultSDK gets the global default SDK.
func GetDefaultSDK() SDK {
	return defaultSDK.Load().(SDK)
}

type SDKOption func(s *sdk)

type SDK interface {
	NewClient(prefix string, ops ...ClientOption) (Client, error)
	GetConfigService() apm_vendor_interface.MetricsConfigService
	PrintStatus() error
}

// sdk defines some common attributes, e.g., debug mode, log level, sdk self-monitor and so on.
// It also binds necessary resources, like vendor tags to metrics clients.
// Note: SDK should be read-only after it is created.
type sdk struct {
	sdkID          int64
	lock           sync.Mutex
	clients        []Client // clients bound to this sdk.
	sdkMonitor              // sdk monitor metrics
	sdkVersion     string
	tagValidator   TagValidator
	metricPreparer MetricPreparer

	// attributes
	vendorTagsProvider          apm_vendor_interface.VendorTagsProvider
	metricConfigProviderManager *config.MetricsConfigProviderManager
	sdkConfig                   *config.GlobalConfig
	tenantConnPools             map[string]writers.ConnPoolManager
	codec                       CodecVersion
}

// NewSDK create a new
func NewSDK(ops ...SDKOption) SDK {
	sdkInstance := newSDK()
	for _, op := range ops {
		op(sdkInstance)
	}

	if sdkInstance.sdkConfig != nil && sdkInstance.sdkConfig.SelfMonitor.Enabled {
		sdkInstance.sdkMonitor = sdkInstance.newMonitor()
		go sdkInstance.reportStatus()
		if sdkInstance.sdkMonitor.err != nil {
			logs.Error("failed to create self-monitor metrics client")
		}
	}

	runtime.SetFinalizer(sdkInstance, func(s *sdk) {
		s.sdkMonitor.close()
		logs.Warn("sdk is cleaned")
	})
	return sdkInstance
}

func (sdkInstance *sdk) reportStatus() {
	if sdkInstance.sdkMonitor.err != nil {
		return
	}

	ticker := time.NewTicker(time.Second * 15)
	defer ticker.Stop()
	for range ticker.C {
		emitAndLog(sdkInstance.versionMetric.WithTagValues(sdkInstance.sdkVersion), WithSuffix("").Store(1))

		sdkInstance.lock.Lock()
		for _, cli := range sdkInstance.clients {
			if c, ok := cli.(*client); ok {
				c.reportMetricStatus()
			}
		}
		sdkInstance.lock.Unlock()
	}
}

func (c *client) reportMetricStatus() {
	c.Lock()
	defer c.Unlock()

	for metricName, metrics := range c.metrics {
		metricMem, seriesCount := 0.0, 0.0
		for _, metric := range metrics {
			for _, values := range metric.status.cacheMem.values() {
				for _, v := range values.values {
					metricMem += v
				}
			}

			for _, values := range metric.status.cacheSeries.values() {
				for _, v := range values.values {
					seriesCount += v
				}
			}
		}

		emitAndLog(c.sdk.emitMetric.WithTagValues(c.tenant, metricName),
			WithField("cache.series").Storef(seriesCount),
			WithField("cache.mem").Storef(metricMem),
		)
	}
}

func SetVendorTagsProvider(provider apm_vendor_interface.VendorTagsProvider) SDKOption {
	return func(s *sdk) {
		s.vendorTagsProvider = provider
	}
}

func SetMetricsConfigProvider(providers ...apm_vendor_interface.MetricsConfigProvider) SDKOption {
	return func(s *sdk) {
		for _, p := range providers {
			s.metricConfigProviderManager.RegisterConfigServiceProvider(p)
		}
	}
}

func SetStaticConfig(ops ...config.GlobalConfigOption) SDKOption {
	return func(s *sdk) {
		for _, op := range ops {
			op(s.sdkConfig)
		}
	}
}

func SetSDKCodec(codec CodecVersion) SDKOption {
	return func(s *sdk) {
		s.codec = codec
	}
}

// SetKeepClosedClient will keep clients in sdk even if the client is closed.
// It is useful in e2e tests.
func SetKeepClosedClient() SDKOption {
	return func(s *sdk) {
		if s.sdkConfig != nil {
			s.sdkConfig.RemoveClosedClients = false
		}
	}
}

// SetTagValueValidator sets the validator of tag values.
func SetTagValueValidator(validator TagValidator) SDKOption {
	return func(s *sdk) {
		if validator != nil {
			s.tagValidator = validator
		}
	}
}

// SetMetricPreparer sets the pre-options of metrics
func SetMetricPreparer(prepare MetricPreparer) SDKOption {
	return func(s *sdk) {
		if prepare != nil {
			s.metricPreparer = prepare
		}
	}
}

// SetVersion sets the version(tag) of this sdk
func SetVersion(version string) SDKOption {
	return func(s *sdk) {
		s.sdkVersion = version
	}
}

func newSDK() *sdk {
	sdk := &sdk{
		sdkID:           atomic.AddInt64(&sdk_id, 1) - 1,
		tenantConnPools: map[string]writers.ConnPoolManager{},
		codec:           CodecV4,
		sdkVersion:      "unknown",
	}
	sdk.tagValidator = newValidator(func(index []string) error {
		for _, value := range index {
			if !utils.IsValidTagValue(value) {
				return fmt.Errorf("invalid tag value: %v", value)
			}
		}
		return nil
	})
	sdk.metricPreparer = newDefaultPreparer()
	sdk.vendorTagsProvider = &NoopVendorTagProvider{}
	sdk.sdkConfig = config.NewSDKConfig()
	logs.Setup(sdk.sdkConfig.Logging.LogLevel, sdk.sdkConfig.Logging.PolishStyle, sdk.sdkConfig.Logging.FilePath)
	sdk.metricConfigProviderManager = config.NewMetricsConfigProviderManager()
	return sdk
}

func (sdkInstance *sdk) NewClient(prefix string, ops ...ClientOption) (Client, error) {
	if sdkInstance == nil {
		return nil, fmt.Errorf("failed to create client with prefix: %sdkInstance since SDK is not initialized", prefix)
	}

	client, err := sdkInstance.newClient(prefix, false, ops...)

	if err != nil {
		return nil, err
	}

	sdkInstance.lock.Lock()
	defer sdkInstance.lock.Unlock()
	sdkInstance.clients = append(sdkInstance.clients, client)
	return client, nil
}

func (sdkInstance *sdk) newMonitorClient(prefix string, ops ...ClientOption) (Client, error) {
	client, err := sdkInstance.newClient(prefix, true, ops...)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (sdkInstance *sdk) newClient(prefix string, isSDKMonitorClient bool, ops ...ClientOption) (*client, error) {
	client := &client{
		sdk:                  sdkInstance,
		prefix:               prefix,
		metrics:              make(map[string][]*metric, 8),
		metricExpireDuration: defaultExpireDuration,
		metricGCInterval:     defaultGCInterval,
		metricGCThreshold:    defaultGCThreshold,
		messageSizeLimit:     sdkInstance.sdkConfig.ResourcesLimit.MaxBatchSize,
		close:                make(chan bool),

		withCodecV4:           sdkInstance.codec == CodecV4,
		forceDrop:             (1 << 13) - 1,
		tenant:                sdkInstance.sdkConfig.DefaultTenant,
		timeIntervalSec:       sdkInstance.sdkConfig.DefaultInterval,
		allowDuplicateMetrics: false,
		vendorTagsProvider:    sdkInstance.vendorTagsProvider,
		deepCopyTagIndex:      true,
		messageProperties:     make([]string, 0),
		tenantStatus:          tenantInitializing,
		isMonitorClient:       isSDKMonitorClient,
		packerParallelism:     1,
	}

	if client.messageSizeLimit <= 0 {
		client.messageSizeLimit = defaultMessageSizeLimit
	}

	// load options
	for _, option := range ops {
		option(client)
	}

	if len(client.prefix) > 0 && !utils.IsValidString(client.prefix) {
		return nil, fmt.Errorf("prefix (%sdkInstance) is not valid", client.prefix)
	}

	if !utils.IsValidString(client.tenant) {
		return nil, fmt.Errorf("tenant (%sdkInstance) is not valid", client.tenant)
	}

	for _, t := range client.globalTags {
		if !utils.IsValidTagValue(t.Value) || !utils.IsValidString(t.Key) {
			return nil, fmt.Errorf("global tag (%sdkInstance=%sdkInstance) is not valid", t.Key, t.Value)
		}
	}

	client.checkConfig()

	if client.sender == nil {
		var err error
		var w io.WriteCloser

		if client.exporterFactory != nil {
			err = client.setExporter(client.exporterFactory(client))
		}

		if err != nil || client.exporterFactory == nil {
			cpm := sdkInstance.getOrCreateConnPoolManager(client)
			w, err = writers.NewAgentWriter(
				writers.WithPoolManager(cpm),
				writers.WithConnNetwork(sdkInstance.sdkConfig.Exporting.DefaultTransport.Network),
				writers.WithAddrs(strings.Split(sdkInstance.sdkConfig.Exporting.DefaultTransport.Addrs, ",")...),
			)
			if err != nil {
				logs.Warn(
					"metrics new writer error: %s, all metrics would be dropped",
					err,
				)
			}
		}

		if w != nil {
			client.sender = client.newSender(w)
		}
	}

	if client.collectorFactory == nil {
		client.collectorFactory = KDTree
	}

	client.messageProperties = append(client.messageProperties,
		"tenant", client.tenant,
		"sdk", sdkInstance.sdkVersion,
		"account_id", "",
		"project_id", sdkInstance.vendorTagsProvider.GetTags()[keyPrimaryPSM],
	)

	// default tenant is always active.
	if client.tenant == client.sdk.sdkConfig.DefaultTenant {
		if !client.IsTenantActive() && client.timeIntervalSec < client.sdk.sdkConfig.DefaultInterval { // interval may be set in ut.
			client.timeIntervalSec = sdkInstance.sdkConfig.DefaultInterval
		}
		client.updateTenantStatus(true)
	}

	// Start the sender goroutine if the client is active, otherwise fetch tenant config in async way.
	if client.IsTenantActive() {
		client.startSender()
	} else {
		go client.checkTenant()
	}

	go client.gc()

	return client, nil
}

// checkTenant is called in async way.
func (client *client) checkTenant() {
	conf, err := client.sdk.metricConfigProviderManager.GetConfig(client.tenant)

	if err != nil {
		logs.Warn("Client: %sdkInstance of Tenant: %sdkInstance failed to fetch config: %v", client.prefix, client.tenant, err)
	} else {
		client.Load(conf)
	}
	// Register the client to the config provider manager

	client.sdk.metricConfigProviderManager.RegisterListener(client)

	if !client.IsTenantActive() {
		logs.Warn("Client: %s of Tenant: %s is not active", client.prefix, client.tenant)
	} else {
		client.startSender()
	}
}

func (client *client) startSender() {
	if atomic.CompareAndSwapInt32(&client.senderStatus, StatusStop, StatusRunning) {
		if client.sdk.sdkConfig.DebugMode {
			logs.Info("activate Client: %s of Tenant: %s at the end of newClient", client.prefix, client.tenant)
		}

		for i := 0; i < client.packerParallelism; i++ {
			client.packers = append(client.packers, client.newPacker(i))
		}

		client.workers.Add(1)
		go client.send()
	}
}

func (sdkInstance *sdk) PrintStatus() error {
	sdkInstance.lock.Lock()
	defer sdkInstance.lock.Unlock()

	logs.Info("sdk has %d clients. vendor tags: %#v", len(sdkInstance.clients), sdkInstance.vendorTagsProvider.GetTags())
	return nil
}

func (sdkInstance *sdk) GetConfigService() apm_vendor_interface.MetricsConfigService {
	return sdkInstance.metricConfigProviderManager.GetConfigService()
}

func (sdkInstance *sdk) RemoveClient(c Client) {
	if sdkInstance.sdkConfig != nil && sdkInstance.sdkConfig.RemoveClosedClients {
		sdkInstance.removeClient(c)
	}
}

func (sdkInstance *sdk) removeClient(c Client) {
	sdkInstance.lock.Lock()
	defer sdkInstance.lock.Unlock()
	for i, cli := range sdkInstance.clients {
		if cli == c {
			nextLen := len(sdkInstance.clients) - 1
			sdkInstance.clients[i] = sdkInstance.clients[nextLen]
			// set the last element nil to avoid memory leak.
			sdkInstance.clients[nextLen] = nil
			sdkInstance.clients = sdkInstance.clients[:nextLen]
			break
		}
	}
}

func (sdkInstance *sdk) getOrCreateConnPoolManager(client *client) writers.ConnPoolManager {
	sdkInstance.lock.Lock()
	defer sdkInstance.lock.Unlock()
	exists, ok := sdkInstance.tenantConnPools[client.GetTenant()]
	if ok {
		return exists
	}

	adjustInterval := sdkInstance.sdkConfig.Exporting.DefaultTransport.ConnPooling.Adjust.IntervalSec
	if adjustInterval <= 0 {
		adjustInterval = client.timeIntervalSec
	}

	cpm := writers.NewConnPoolManager(
		sdkInstance.sdkConfig.Exporting.DefaultTransport.ConnPooling.MaxCap,
		sdkInstance.sdkConfig.Exporting.DefaultTransport.ConnPooling.MaxIdle,
		sdkInstance.sdkConfig.Exporting.DefaultTransport.ConnPooling.InitialCap,
		time.Duration(adjustInterval)*time.Second,
		sdkInstance.sdkConfig.Exporting.DefaultTransport.ConnPooling.Adjust.Threshold,
		sdkInstance.sdkConfig.Exporting.DefaultTransport.ConnPooling.Adjust.Step,
	)

	sdkInstance.tenantConnPools[client.GetTenant()] = cpm
	return cpm
}

type NoopVendorTagProvider struct{}

func (p *NoopVendorTagProvider) GetTags() map[string]string {
	return nil
}

func (p *NoopVendorTagProvider) GetImmutableTags() map[string]string {
	return nil
}

func (p *NoopVendorTagProvider) GetRewritableTags() map[string]string {
	return nil
}

type NoopMetricConfigProvider struct{}

func (p *NoopMetricConfigProvider) GetConfig(tenant string) (*apm_vendor_interface.MetricsConfig, error) {
	return &apm_vendor_interface.MetricsConfig{
		Tenant:              apm_vendor_interface.StringReference(tenant),
		IsTenantActive:      apm_vendor_interface.BoolReference(true),
		MinIntervalInSecond: apm_vendor_interface.IntReference(1),
		Extras:              nil,
	}, nil
}

// TagValidator validate tag values
type TagValidator interface {
	Validate(index []string) error
}

type validator func(index []string) error

func (v validator) Validate(index []string) error {
	return v(index)
}

func newValidator(f func(index []string) error) TagValidator {
	return validator(f)
}

type MetricPreparer interface {
	Prepare(m *metric)
}

type metricPreparer func(m *metric)

func (p metricPreparer) Prepare(m *metric) {
	p(m)
}

func newPreparer(f func(m *metric)) MetricPreparer {
	return metricPreparer(f)
}

func newDefaultPreparer() MetricPreparer {
	return newPreparer(func(m *metric) {
		tagNames := m.tags
		c := m.client
		sortedTags := make([]string, 0, len(tagNames))
		sortedTags = append(sortedTags, tagNames...)
		for _, v := range c.globalTags {
			sortedTags = append(sortedTags, v.Key)
		}
		sort.Strings(sortedTags)

		// Save the tag index in sorted slice.
		foundHostInTags, foundDCInTags := c.foundHostInGlobalTags, c.foundDCInGlobalTags
		for i, sortedTag := range sortedTags {
			for j, uTag := range tagNames {
				if uTag == sortedTag {
					m.sortedUserTagPos[j] = i
					break
				}
			}

			for j, gTag := range c.globalTags {
				if sortedTag == gTag.Key {
					m.sortedGlobalTagPos[j] = i
					break
				}
			}
			if !foundDCInTags && sortedTag == "dc" {
				foundDCInTags = true
			}

			if !foundHostInTags && sortedTag == "host" {
				foundHostInTags = true
			}
		}

		m.fnv32aIndices = append(m.fnv32aIndices, m.sortedUserTagPos...)
		sort.Ints(m.fnv32aIndices)

		if foundDCInTags && foundHostInTags {
			m.flags |= 0x07
		}
	})
}

type sdkMonitor struct {
	err                 error
	monitorClient       Client
	versionMetric       Metric
	emitMetric          Metric
	senderMetric        Metric
	senderLatencyMetric Metric
	senderFailMetric    Metric
}

func (m sdkMonitor) newMonitorMetric(name string, tagNames []string, options ...MetricOption) Metric {
	if m.err != nil {
		return &noopMetric{}
	}

	metric, err := m.monitorClient.NewMetricWithOps(name, tagNames, options...)
	if err != nil {
		logs.Warn("new sdk monitor metric %s failed: %s", name, err.Error())
	}
	return metric
}

func (m sdkMonitor) close() {
	if m.err == nil && m.monitorClient != nil {
		m.monitorClient.Close()
	}
}

func (sdkInstance *sdk) newMonitor() sdkMonitor {
	m := sdkMonitor{}
	tags := make([]T, 0)
	tags = append(tags,
		T{"component", "bytetsd"},
		T{"language", "go"},
		T{"client_id", strconv.Itoa(int(sdkInstance.sdkID))},
		T{"pid", strconv.Itoa(os.Getpid())},
	)
	for k, v := range sdkInstance.sdkConfig.SelfMonitor.GlobalTags {
		tags = append(tags, T{k, v})
	}

	m.monitorClient, m.err = sdkInstance.newMonitorClient(
		"inf.apm.ingress",
		SetTenant(internalTenant),
		SetTimeInterval(selfMonitorFlushInterval),
		SetAllowDuplicateMetricNames(),
		SetDiscardInvalidTag(),
		SetMetricsExpireDuration(0),
		SetGlobalTags(tags...),
		SetIgnoreUnchangedCounter(),
		SetReportInitialCounter(),
	)
	m.versionMetric = m.newMonitorMetric("version", []string{"sdk_version"})
	m.emitMetric = m.newMonitorMetric("metric", []string{"tenant", "metric_name"},
		SetMultiFields([]Field{
			NewRateCounterField("emit.success"),
			NewRateCounterField("emit.fail"),
			NewStoreField("cache.series"),
			NewStoreField("cache.mem"),
		}),
	)
	m.senderMetric = m.newMonitorMetric("sender", []string{"tenant", "cause"},
		SetMultiFields([]Field{
			NewRateCounterField("bytes"),
			NewRateCounterField("metrics"),
			NewRateCounterField("series"),
		}),
	)
	m.senderLatencyMetric = m.newMonitorMetric("sender.latency", []string{"tenant", "name"}, SetMultiFieldTimer())
	m.senderFailMetric = m.newMonitorMetric("sender.fail", []string{"tenant", "status"})
	return m
}
