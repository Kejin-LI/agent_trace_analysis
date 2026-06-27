package config_service

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	gcpConf "code.byted.org/aiops/apm_vendor_byted/config_service/conf/gcp"
	rowConf "code.byted.org/aiops/apm_vendor_byted/config_service/conf/row"
	ttpConf "code.byted.org/aiops/apm_vendor_byted/config_service/conf/ttp"
	"code.byted.org/gopkg/env"
)

const (
	tenantIntervalAPI    = "/v1/tenant_interval"
	producerEndpointsAPI = "/v1/consul_nodes_info"
	keyInterval          = "intervalInSec"
	keyVDC               = "vdc"
	keyProducer          = "producerConsul"
	keyConfigSvcDomain   = "configSvcDomain"
	defaultProducer      = "inf.bytetsd.producer"
	defaultProducerInBoe = "inf.bytetsd.producer_boe"
)

const (
	defaultRetryTimes = 3
	defaultTimeOut    = 5 * time.Second
	defaultInterval   = 30
)

var logfunc = log.Printf

// newMetricsHttpClientWithConf new a metrics http client with a specific config.
func newMetricsHttpClientWithConf(conf *config) *metricsHttpClient {
	c := &metricsHttpClient{
		conf:      conf,
		cli:       http.Client{Timeout: conf.timeout},
		rwLock:    sync.RWMutex{},
		routerMap: make(map[string]string),
	}

	if conf.inTTP() {
		c.routerURl = ttpConf.RouterPrefix + conf.dc
	} else if conf.inGCP() {
		c.routerURl = gcpConf.RouterPrefix + conf.dc
	} else {
		c.routerURl = rowConf.RouterPrefix + conf.dc
	}
	go c.initRouterMap()
	return c
}

type metricsHttpClient struct {
	conf      *config
	rwLock    sync.RWMutex
	cli       http.Client
	routerURl string

	routerMap map[string]string
	once      sync.Once
}

// getMinInterval get the minimal tenant flush in interval from metrics config server.
func (c *metricsHttpClient) getMinInterval(tenant string) (int, error) {
	c.rwLock.RLock()
	if _, ok := c.routerMap[keyConfigSvcDomain]; !ok {
		c.rwLock.RUnlock()
		err := c.initRouterMap()
		if err != nil {
			return defaultInterval, err
		}

		// When will the code reach here?
		// The client failed to init the router map in init but later succeeded to do it.
		c.rwLock.RLock()
	}
	urlPrefix := c.routerMap[keyConfigSvcDomain]
	c.rwLock.RUnlock()

	url, err := composeUrl(false, urlPrefix, tenantIntervalAPI, "tenantNames", tenant)
	if err != nil {
		return defaultInterval, err
	}

	body, err := c.getResponseBody(c.conf.retryTimes, url)
	if err != nil {
		return defaultInterval, err
	}

	if len(body) == 0 {
		return defaultInterval, fmt.Errorf("failed to get the tenant minimal time interval info, body is empty")
	}
	interval, err := c.parseTenantIntervalResp(tenant, body)
	return interval, err
}

// getProducerDC gets the vdc
func (c *metricsHttpClient) getProducerDC() (string, error) {
	c.rwLock.RLock()
	if vdc, ok := c.routerMap[keyVDC]; ok {
		c.rwLock.RUnlock()
		return vdc, nil
	}
	c.rwLock.RUnlock()
	err := c.initRouterMap()
	if err != nil {
		return c.conf.dc, err
	}
	c.rwLock.RLock()
	defer c.rwLock.RUnlock()
	return c.routerMap[keyVDC], nil
}

// getProducer gets the consul producer service name
func (c *metricsHttpClient) getProducer() (string, error) {
	c.rwLock.RLock()
	if producerName, ok := c.routerMap[keyProducer]; ok {
		c.rwLock.RUnlock()
		return producerName, nil
	}
	c.rwLock.RUnlock()
	err := c.initRouterMap()
	if err != nil {
		if c.conf.inBoe {
			return defaultProducerInBoe, err
		}
		return defaultProducer, err
	}
	c.rwLock.RLock()
	defer c.rwLock.RUnlock()
	return c.routerMap[keyProducer], nil
}

// getProducerEndpoints gets the producer endpoints
func (c *metricsHttpClient) getProducerEndpoints() ([]string, error) {
	producerServiceName, err := c.getProducer()
	if err != nil {
		return nil, err
	}

	if strings.Contains(producerServiceName, ",") {
		return nil, fmt.Errorf("this api: %s does not support multi-producers: %s", producerEndpointsAPI, producerServiceName)
	}

	urlPrefix, ok := c.routerMap[keyConfigSvcDomain]
	if !ok {
		return nil, fmt.Errorf("unknown config service domain")
	}

	url, err := composeUrl(false, urlPrefix, producerEndpointsAPI, "consulName", producerServiceName)
	if err != nil {
		return nil, err
	}

	body, err := c.getResponseBody(c.conf.retryTimes, url)
	if err != nil {
		return nil, err
	}

	if len(body) == 0 {
		return nil, fmt.Errorf("failed to get the producer endpoints, body is empty")
	}
	endpoints, err := c.parseProducerEndpointsResp(body)
	return endpoints, err
}

// initRouterMap updates the router map and producer name.
func (c *metricsHttpClient) initRouterMap() error {
	c.rwLock.Lock()
	defer c.rwLock.Unlock()
	body, err := c.getResponseBody(defaultRetryTimes, c.routerURl)
	if err != nil {
		return err
	}

	if len(body) == 0 {
		return fmt.Errorf("failed to get the domain info, body is empty")
	}
	err = c.parseRouterResp(body)
	return err
}

// getResponseBody gets the response.body from url for at most retryTimes
func (c *metricsHttpClient) getResponseBody(retryTimes int, url string) ([]byte, error) {
	var body []byte
	var err error
	for i := 0; i < retryTimes; i++ {
		err = func() error {
			resp, err := c.cli.Get(url)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			body, err = ioutil.ReadAll(resp.Body)
			if err != nil {
				return err
			}

			if resp.StatusCode != 200 {
				logfunc("[WARN] response status code %v", resp.StatusCode)
			}

			return nil
		}()

		if err == nil {
			return body, nil
		}

		time.Sleep(time.Millisecond * 100)
	}

	return body, err
}

// parseRouterResp updates the routerMap.
// Note that the lock is already hold.
func (c *metricsHttpClient) parseRouterResp(body []byte) error {
	var routerResponse RouterResponse
	err := json.Unmarshal(body, &routerResponse)

	if err != nil {
		return fmt.Errorf("failed to unmarshal router response: %w", err)
	}

	if routerResponse.Result == nil {
		return fmt.Errorf("retrieved an invalid result: %v for dc: %v", routerResponse.Result, c.conf.dc)
	}

	c.routerMap[keyVDC] = routerResponse.Result.Vdc
	c.routerMap[keyProducer] = routerResponse.Result.ProducerConsul

	if routerResponse.Result.Domains != nil {
		c.routerMap[keyConfigSvcDomain] = routerResponse.Result.Domains.ConfigSvcDomain
	}
	return nil
}

func (c *metricsHttpClient) parseTenantIntervalResp(tenant string, body []byte) (int, error) {
	var tenantResponse TenantIntervalResponse
	err := json.Unmarshal(body, &tenantResponse)

	if err != nil {
		return defaultInterval, fmt.Errorf("failed to unmarshal tenant interval response: %w", err)
	}

	if len(tenantResponse.Result) == 0 {
		return defaultInterval, fmt.Errorf("didn't find any configurations for this tenant: %s, "+
			"make sure you provided the correct tenant name", tenant)
	}

	for _, v := range tenantResponse.Result {
		if v.Tenant == tenant {
			return v.IntervalInSec, nil
		}
	}

	return defaultInterval, fmt.Errorf("didn't find any configurations for this tenant: %s, "+
		"make sure you provided the correct tenant name", tenant)
}

func (c *metricsHttpClient) parseProducerEndpointsResp(body []byte) ([]string, error) {

	var consulNodeResponse ConsulNodeResponse
	err := json.Unmarshal(body, &consulNodeResponse)

	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal producer consul node response: %w", err)
	}

	if consulNodeResponse.Result == nil || len(consulNodeResponse.Result) == 0 {
		return nil, fmt.Errorf("empty result of producer consul nodes")
	}

	endpoints := make([]string, 0, len(consulNodeResponse.Result))

	for _, v := range consulNodeResponse.Result {
		endpoint, err := v.toNode()
		if err != nil {
			continue
		}

		if env.IsDualStack() {
			endpoints = append(endpoints, endpoint)
		} else if env.IsIPV4Only() {
			// ipv4-only nodes cannot connect to ipv6 endpoints.
			if !isEndpointIpv6(endpoint) {
				endpoints = append(endpoints, endpoint)
			}
		} else if env.IsIPV6Only() {
			// ipv6-only nodes cannot connect to ipv4 endpoints.
			if isEndpointIpv6(endpoint) {
				endpoints = append(endpoints, endpoint)
			}
		}
	}
	if len(endpoints) == 0 {
		return endpoints, fmt.Errorf("no usable endpoints")
	}
	return endpoints, nil
}

func isIPV6(ip string) bool {
	return strings.Contains(ip, ":")
}

func isEndpointIpv6(endpoint string) bool {
	return strings.HasPrefix(endpoint, "[")
}

func composeUrl(https bool, domain string, api string, key string, values ...string) (string, error) {
	if len(domain) == 0 {
		return "", fmt.Errorf("empty domain")
	}

	if len(values) == 0 { // no values
		return "", fmt.Errorf("invalid value")
	}

	var res string
	if https {
		res += "https://" + domain
	} else {
		res += "http://" + domain
	}
	if !strings.HasPrefix(api, "/") {
		res += "/"
	}
	res += api + "?"

	// There is at lease 1 value.
	res += key + "=" + values[0]
	for _, v := range values[1:] {
		res += ("&" + key + "=" + v)
	}

	return res, nil
}
