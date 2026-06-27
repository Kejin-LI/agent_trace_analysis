package env

import (
	"os"
	"strconv"
	"strings"
)

var tceServicePort = ""
var tceDebugPort = ""

var portMap = map[string]string{}

const (
	MaxPort = 65535
	MinPort = 0
)

func init() {
	parseTCEPorts()
}

func TCEServicePort() string {
	return tceServicePort
}

func TCEDebugPort() string {
	return tceDebugPort
}

func isValidPort(portStr string) bool {
	port, err := strconv.Atoi(portStr)
	return err == nil && port >= MinPort && port <= MaxPort
}

// TCEServicePortRegistered 返回 TCE_PRIMARY_PORT 对应的访问端口
func TCEServicePortRegistered() string {
	return TCEPortRegistered(TCEServicePort())
}

// TCEDebugPortRegistered 返回 TCE_DEBUG_PORT 对应的访问端口
func TCEDebugPortRegistered() string {
	return TCEPortRegistered(TCEDebugPort())
}

// TCEPortRegistered 返回服务端口对应的访问端口
func TCEPortRegistered(svr string) string {
	return portMap[svr]
}

// https://bytedance.feishu.cn/wiki/wikcnR7f17phbl2h7ZVnj4KgRfg
// TCE 平台上用户注册所有端口的列表，通过空格分隔，按照顺序依次为 PORT0 PORT1 ... PORTN
func parseTCEPorts() {
	servicePortStr := os.Getenv("TCE_PRIMARY_PORT")
	if isValidPort(servicePortStr) {
		tceServicePort = servicePortStr
	}
	debugPortStr := os.Getenv("TCE_DEBUG_PORT")
	if isValidPort(debugPortStr) {
		tceDebugPort = debugPortStr
	}

	raw := os.Getenv("TCE_PORTS")
	if raw == "" {
		return
	}
	raw = strings.TrimSpace(raw)
	portList := strings.Split(raw, " ")
	for i, port := range portList {
		if !isValidPort(port) {
			continue
		}
		// TCE 平台往 Consul 注册的端口值，该变量下，PORT0会注册为主端口，其他的PORT1、2、... N，会注册到 Consul 的 Tag 里，如果有映射，这是经过端口映射后的值。
		hostPort := os.Getenv("PORT" + strconv.Itoa(i))
		if !isValidPort(hostPort) {
			continue
		}
		portMap[port] = hostPort
	}
}
