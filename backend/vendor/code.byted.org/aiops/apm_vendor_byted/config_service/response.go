package config_service

import (
	"encoding/json"
	"fmt"
	"net"
)

type RouterResponse struct {
	Code   int           `json:"code"`
	Msg    string        `json:"msg"`
	Result *RouterResult `json:"result"`
}

func (c RouterResponse) String() string {
	jsonData, err := json.Marshal(c)
	if err != nil {
		return fmt.Sprintf("Error marshaling JSON: %s", err)
	}
	return string(jsonData)
}

type RouterResult struct {
	Vdc            string   `json:"vdc"`
	ProducerConsul string   `json:"producerConsul"`
	Domains        *Domains `json:"domains"`
}

type Domains struct {
	PortalQueryDomain    string `json:"portalQueryDomain"`
	PortalBosunDomain    string `json:"portalBosunDomain"`
	AlarmQueryDomain     string `json:"alarmQueryDomain"`
	ArgosQueryDomain     string `json:"argosQueryDomain"`
	ArgosBosunDomain     string `json:"argosBosunDomain"`
	ConfigSvcDomain      string `json:"configSvcDomain"`
	ProducerProxyPublic  string `json:"producerProxyPublic"`
	ProducerProxyPrivate string `json:"producerProxyPrivate"`
}

type ConsulNodeResponse struct {
	Code   int                 `json:"code"`
	Msg    string              `json:"msg"`
	Result []*ConsulNodeResult `json:"result"`
}

func (c ConsulNodeResponse) String() string {
	jsonData, err := json.Marshal(c)
	if err != nil {
		return fmt.Sprintf("Error marshaling JSON: %s", err)
	}
	return string(jsonData)
}

type ConsulNodeResult struct {
	Host string            `json:"Host"`
	Port int               `json:"Port"`
	Tags map[string]string `json:"Tags"`
}

func (c *ConsulNodeResult) toNode() (string, error) {
	if c == nil {
		return "", fmt.Errorf("nil result")
	}
	host := c.Host
	ip := net.ParseIP(host)
	if ip == nil {
		return "", fmt.Errorf("invalid ip: %v", host)
	}

	if isIPV6(c.Host) {
		host = "[" + host + "]"
	}

	return fmt.Sprintf("%s:%d", host, c.Port), nil
}

type TenantIntervalResponse struct {
	Code   int              `json:"code"`
	Msg    string           `json:"msg"`
	Result []IntervalResult `json:"result"`
}

func (c TenantIntervalResponse) String() string {
	jsonData, err := json.Marshal(c)
	if err != nil {
		return fmt.Sprintf("Error marshaling JSON: %s", err)
	}
	return string(jsonData)
}

type IntervalResult struct {
	Tenant        string `json:"tenant"`
	IntervalInSec int    `json:"intervalInSec"`
}
