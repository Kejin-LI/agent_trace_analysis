package util

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"time"

	"code.byted.org/bcc/bcc-go-client/logger"
	"code.byted.org/bcc/tools"
	"code.byted.org/gopkg/consul"
)

const OtherPORT2 = "PORT2"

func GetOneEndpoint(psm string, opts ...consul.LookupOptions) (consul.Endpoint, error) {
	eps, err := GetEndpoints(psm, opts...)
	if err != nil {
		return consul.Endpoint{}, err
	}
	if len(eps) == 0 {
		return consul.Endpoint{}, fmt.Errorf("consul endpoint empty, params:%v psm:%v", tools.ToJson(opts), psm)
	}
	rand.Seed(time.Now().UnixNano())
	ep := eps[rand.Intn(len(eps))]
	logger.Debug("got psm[%v] endpoint:%v", psm, ep)
	return ep, nil
}

func GetEndpoints(psm string, opts ...consul.LookupOptions) (consul.Endpoints, error) {
	eps, err := consul.LookupName(psm, opts...)
	if err != nil {
		logger.Error("consul.LookupName:[%v] error: %v, opts:%v", psm, err, opts)
		return consul.Endpoints{}, err
	}
	if len(eps) == 0 {
		logger.Error("consul.LookupName [%v] empty option: %v", psm, opts)
		return consul.Endpoints{}, errors.New("consul.LookupName empty")
	}
	envMap := map[string]bool{
		"prod":   true,
		"canary": true,
	}
	if customEnv := os.Getenv("BCC_SVR_ENV"); customEnv != "" {
		envMap = map[string]bool{
			customEnv: true,
		}
	}
	eps = eps.Filter(func(e consul.Endpoint) bool {
		return envMap[e.Env]
	})
	return eps, nil
}
func GetEndpointAddr(ep consul.Endpoint, portName string) string {
	host, _, _ := net.SplitHostPort(ep.Addr)
	port := ep.Tags[portName]
	return net.JoinHostPort(host, port)
}
