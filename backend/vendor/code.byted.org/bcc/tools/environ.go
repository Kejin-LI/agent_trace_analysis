package tools

import (
	"os"
	"strings"
)

var tceHostEnv string

func init() {
	tceHostEnv = os.Getenv("TCE_HOST_ENV")
}
func GetTceHostEnv() string {
	return tceHostEnv
}

//环境变量
func EnvironAll() map[string]string {
	environ := os.Environ()
	r := make(map[string]string, len(environ))
	for _, v := range environ {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) == 2 {
			r[parts[0]] = parts[1]
		}
	}
	return r
}

//只获取server端必须得一些环境变量
func NecessaryEnviron() map[string]string {
	allEnv := EnvironAll()

	spEnv := map[string]bool{
		"FTF_ENV":           true,
		"IS_FAAS_ENV":       true,
		"IS_TCE_DOCKER_ENV": true,
		"IS_HOST_NETWORK":   true,
		"BYTED_HOST_IPV6":   true,
		"BYTED_HOST_IP":     true,
		"MY_HOST_IP":        true,
		"MY_POD_IP":         true,
		"SERVICE_ENV":       true,
	}

	nEnv := make(map[string]string)
	for envName, envValue := range allEnv {
		if strings.HasPrefix(envName, "TCE_") || spEnv[envName] {
			nEnv[envName] = envValue
		}
	}

	return nEnv
}

//环境变量
func EnvironGet(key string, def string) string {
	r := os.Getenv(key)
	if r == "" {
		return def
	}
	return r
}
