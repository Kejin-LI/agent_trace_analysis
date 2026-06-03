package confclient

import (
	"fmt"
	"strings"
	"sync"

	"code.byted.org/bcc/bcc-go-client"
	"code.byted.org/bcc/bcc-go-client/logger"
	metrics "code.byted.org/bcc/tools/bmetrics/v3"
	m "code.byted.org/gopkg/metrics/v3"
)

var gTCCMetrics *tccMetrics
var tccMetricOnce sync.Once

type tccMetrics struct {
	metricsMap map[string]*tccMetricsWithSdkVersion // key: [identifier:namespace] such as tcc:bytedance.bcc.server, 避免同一个项目中既有 tcc client 又有 bcc client 导致 metrics 混用
	mu         sync.RWMutex
}

type tccMetricsWithSdkVersion struct {
	cli           *metrics.MetricsClient
	tccSdkVersion string
}

func addTCCClientMetrics(namespace, identifier, tccSdkVersion string) {
	nsIdentifierKey := genNsIdentifierKey(namespace, identifier)
	tccMetricOnce.Do(func() {
		gTCCMetrics = &tccMetrics{
			metricsMap: make(map[string]*tccMetricsWithSdkVersion),
		}
	})
	// 双重检查减少并发锁
	gTCCMetrics.mu.RLock()
	m := gTCCMetrics.metricsMap[nsIdentifierKey]
	gTCCMetrics.mu.RUnlock()
	if m != nil {
		return
	}
	gTCCMetrics.mu.Lock()
	defer gTCCMetrics.mu.Unlock()
	if gTCCMetrics.metricsMap[nsIdentifierKey] != nil {
		return
	}
	gTCCMetrics.metricsMap[nsIdentifierKey] = &tccMetricsWithSdkVersion{
		cli: metrics.NewMetricsClient(
			// tcc.client.$psm.get_config.success
			metrics.WithPrefix("tcc"),
			metrics.WithMetrics(map[string][]string{
				fmt.Sprintf("client.%s.get_config", namespace): {"identifier", "confspace", "path", "config_key", "version", "bcc_version", "api_version", "language", "code", "get_type"},
			}),
		),
		tccSdkVersion: tccSdkVersion,
	}
}

func (t *tccMetrics) emit(identifier string, namespace string, path, keyName string, status emitStatus, cnt int64) {
	if cnt <= 0 {
		return
	}
	t.mu.RLock()
	tccMCliWithSdkVersion := t.metricsMap[genNsIdentifierKey(namespace, identifier)]
	t.mu.RUnlock()
	if tccMCliWithSdkVersion == nil || tccMCliWithSdkVersion.cli == nil {
		logger.Error("should not happen, tcc metrics not init namespace:%v, identifier:%v", namespace, identifier)
		return
	}
	logger.Debug("emit tcc metrics namespace:%v, path:%v, keyName:%v, status:%v, cnt:%v", namespace, path, keyName, status, cnt)

	mClient, tccSdkVersion := tccMCliWithSdkVersion.cli, tccMCliWithSdkVersion.tccSdkVersion

	code := "0"
	switch status {
	case emitStatusErr:
		code = "1"
	case emitStatusNotFound:
		status = emitStatusSucc
		code = "100"
	}

	getConfSpace := func(path string) string {
		//兼容tcc sdk的逻辑，只有一层目录才会打 confspace tag
		if path == "/" {
			return "-"
		}
		splitPath := strings.Split(path, "/")
		if len(splitPath) != 2 {
			return "-"
		}
		return splitPath[1]
	}

	ts := []m.T{
		{Name: "identifier", Value: identifier},
		{Name: "confspace", Value: getConfSpace(path)},
		{Name: "path", Value: path},
		{Name: "config_key", Value: keyName},
		{Name: "version", Value: tccSdkVersion},
		{Name: "bcc_version", Value: bcc.SDKVersion()},
		{Name: "api_version", Value: "v3"},
		{Name: "language", Value: "go"},
		{Name: "code", Value: code},
		{Name: "get_type", Value: "cache"},
	}
	mClient.EmitCounterWithSuffix(fmt.Sprintf("client.%s.get_config", namespace), string(status), int(cnt), ts...)
}

func genNsIdentifierKey(namespace, identifier string) string {
	return fmt.Sprintf("%s:%s", identifier, namespace)
}
