package helper

import (
	"code.byted.org/gopkg/metrics"
	"github.com/sirupsen/logrus"
)

const (
	VERSION                = "1.0.15"
	LANGUAGE               = "golang"
	GetTokenFail           = "get_token_error"
	GetTokenSucceed        = "get_token"
	GetTokenFromDPSAgent   = "get_token_from_dps_agent"
	GetTokenFromString     = "get_token_from_string"
	GetTokenFromAgentFail  = "get_token_from_agent_fail"
	FetchTokenFromPathFail = "fetch_token_path_error"
)

var metricCli *metrics.MetricsClientV2

func initMetricCli() {
	metricCli = metrics.NewDefaultMetricsClientV2("security.zti.jwt_helper", true)
}

func EmitCounter(name string, tags map[string]string) {
	metricsTags := mapToTags(tags)
	if err := metricCli.EmitCounter(name, 1, *metricsTags...); err != nil {
		logrus.Warn("security.zti.jwt_helper metrics EmitCounter error: ", err.Error())
	}
}

func mapToTags(tagMap map[string]string) *[]metrics.T {
	if tagMap == nil {
		tagMap = make(map[string]string)
	}
	tagMap["ver"] = VERSION
	tagMap["language"] = LANGUAGE
	tags := make([]metrics.T, 0, len(tagMap))
	for k, v := range tagMap {
		tags = append(tags, metrics.T{Name: k, Value: v})
	}
	return &tags
}
