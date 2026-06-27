package util

import (
	"code.byted.org/gopkg/env"
	"os"
)

// env.Cluster() 函数会在 SERVICE_CLUSTER 为空时返回 default，因此无法直接使用 env.Cluster() 判断集群
// SERVICE_CLUSTER 由 loader 导入，是个进程内环境变量，checker 工具无法获取到
// TCE_CLUSTER 由上层平台导入，checker 工具可以获取到
// TCE_CLUSTER 是容器中自带的环境变量，TCE FAAS 等都注入了该环境变量
// TCE 环境变量 https://bytedance.feishu.cn/wiki/wikcnR7f17phbl2h7ZVnj4KgRfg
func GetCluster() string {
	cluster := os.Getenv("TCE_CLUSTER")
	if cluster == "" {
		cluster = env.Cluster()
	}
	return cluster
}
