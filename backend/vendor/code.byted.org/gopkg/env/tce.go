package env

import (
	"os"
)

var (
	podName        string
	imageVersion   string
	cloudEnv       string
	inVolcanoCloud bool
	isHostNetwork  bool
)

func init() {
	podName = os.Getenv("MY_POD_NAME")
	if podName == "" {
		podName = "-"
	}
	imageVersion = os.Getenv("CURRENT_VERSION")
	if imageVersion == "" {
		imageVersion = "-"
	}
	cloudEnv = os.Getenv("PAAS_CLOUD_ENV")
	if cloudEnv == "" {
		cloudEnv = "-"
	}
	if cloudEnv == "VOLCANO" {
		inVolcanoCloud = true
	}
	if ihn := os.Getenv("IS_HOST_NETWORK"); ihn == "1" {
		isHostNetwork = true
	}
}

func InTCE() bool {
	return inTCE
}

func PodName() string {
	return podName
}

func ImageVersion() string {
	return imageVersion
}

func CloudEnv() string {
	return cloudEnv
}

func InVolcanoCloud() bool {
	return inVolcanoCloud
}

// IsHostNetwork 网络模式是否为 host 或 autohost 模式
func IsHostNetwork() bool {
	return isHostNetwork
}
