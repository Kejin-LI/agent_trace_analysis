package fic_client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"code.byted.org/gopkg/env"

	"code.byted.org/middleware/fic_client/model"
)

const (
	collectPath  = "http://%s/v1/collect"
	queryPathFmt = "http://%s/v1/query?status=1&psm=%s"

	disableEnvKey = "DISABLE_FVC_CLIENT" // not typo, for compatibility
)

var (
	disabled bool
	domain   = "fic.byted.org"

	httpCli = http.Client{Timeout: time.Second * 5}

	globalInfo              *collectInfo
	hasDelayedReportTask    = int32(0)
	delayInterval           = time.Second * 15 // 聚合上报延迟
	needReport              bool
	blockedComponentEnvKeys = []string{"SIDECAR_NAME", "LEGO_PLUGIN_DIR"} // If one of the env in this list has value, then no need to report
)

// path -> name
var whitelistMods = map[string]string{
	"trace-contrib/kitex-go": "kitex-go",
	"middleware/netpoll":     "netpoll",
}

func init() {
	globalInfo = newCollectInfo(env.PSM(), env.Cluster(), env.IDC())
	isBytedCloud := env.IsProduct() || env.IsBoe()
	isBlockedComponent := checkBlockedComponentByEnv()
	needReport = checkNeedReport(domain, isBytedCloud, isBlockedComponent, globalInfo)

	if os.Getenv(disableEnvKey) != "" {
		disabled = true
	}
	buildInfo, ok := debug.ReadBuildInfo()
	if ok {
		for _, mod := range buildInfo.Deps {
			for k, v := range whitelistMods {
				if strings.Contains(mod.Path, k) {
					Collect(v, mod.Version, nil)
				}
			}
		}
	}
}

func checkBlockedComponentByEnv() bool {
	for _, key := range blockedComponentEnvKeys {
		if os.Getenv(key) != "" {
			return true
		}
	}
	return false
}

func checkNeedReport(domain string, isBytedCloud bool, isBlockedComponent bool, info *collectInfo) bool {
	if domain == "" {
		// if domain is not set, return
		return false
	}
	if !isBytedCloud {
		// logs.Info("not prod environment, skip collect version info")
		return false
	}
	if info.psm == env.PSMUnknown || info.cluster == "" || info.idc == env.UnknownIDC {
		// logs.Info("not prod environment, skip collect version info")
		return false
	}
	if isBlockedComponent {
		// do not collect info for specified components
		return false
	}
	return true
}

func Collect(name, version string, data map[string]interface{}) {
	if !needReport {
		return
	}

	if !globalInfo.AddFramework(name, version, data) {
		return
	}

	delayTask(&hasDelayedReportTask, delayInterval, func() {
		report(globalInfo.GetModelData())
	})
}

func delayTask(hasDelayedTask *int32, interval time.Duration, task func()) {
	if atomic.CompareAndSwapInt32(hasDelayedTask, 0, 1) {
		time.AfterFunc(interval, func() {
			atomic.StoreInt32(hasDelayedTask, 0)
			task()
		})
	}
}

func report(data *model.Data) {
	url := fmt.Sprintf(collectPath, domain)
	for i := 0; i < 3; i++ { // 重试三次
		resp, err := postJson(url, data)
		if err == nil {
			if resp != nil {
				resp.Body.Close()
			}
			break
		}
		// logs.Warn("framework version collect failed: %v", err)
		time.Sleep(5 * time.Second)
	}
}

// GetPSMVersionInfo is the wrap func to query framework version info of psm.
func GetPSMVersionInfo(psm string, params map[string]string) ([]map[string]interface{}, error) {
	if psm == env.PSMUnknown || psm == "" {
		return nil, fmt.Errorf("psm is invalid, psm=%s", psm)
	}
	url := fmt.Sprintf(queryPathFmt, domain, psm)
	for k, v := range params {
		if k != "" && v != "" {
			url += "&" + k + "=" + v
		}
	}
	httpResp, err := httpCli.Get(url)
	if err != nil {
		return nil, fmt.Errorf("http request failed, url=%s, %w", url, err)
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("version query failed, url=%s, ", url)
	}

	var body []byte
	body, err = ioutil.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}
	var resp []map[string]interface{}
	if err = json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

var SetExtra = globalInfo.SetExtra

func postJson(url string, data interface{}) (*http.Response, error) {
	if disabled {
		// logs.Info("framework version collector disabled")
		return nil, nil
	}
	b, err := json.Marshal(data)
	if err != nil {
		// logs.Warn("marshal json failed: %v", err)
		return nil, err
	}
	resp, err := httpCli.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		// logs.Warn("http post failed: %v", err)
	}
	return resp, err
}
