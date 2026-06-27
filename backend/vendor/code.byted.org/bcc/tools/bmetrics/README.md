
## 说明

metrics组件

[code.byted.org/gopkg/metrics](https://code.byted.org/gopkg/metrics) 二次封装

- 初始化，模块内定义默认client
- 结果转换
- metrics默认tags https://code.byted.org/gopkg/metrics/blob/master/metrics.go#L136
  -	env_type=tce
  -	pod_name
  - _psm
  - deploy_stage=os.Getenv("TCE_STAGE")
  - cluster
  - host_v6

文档（go基础监控、thrift监控）  https://bytedance.feishu.cn/wiki/wikcnAqL7HeBJl72YfEVdAZv44b#JIc0t5
打点规范 https://site.bytedance.net/docs/2080/2717/29398/


## 示例
```go
package main

import (
    "code.byted.org/gopkg/env"
    "code.byted.org/toutiao/easygo/byted/bmetrics"
)

func main() {
    //default client
    bmetrics.EmitCounter("count1", 11)
    bmetrics.EmitStore("store1", 22)
    bmetrics.EmitTimer("timer1", 33, bmetrics.Tag("t1", "33"))
    
    //special client
    c := bmetrics.NewMetricsClient("XX.XX.XX")
    c.EmitCounter("count2", 11)
    c.EmitStore("store1", 22)
    c.EmitTimer("timer1", 33, bmetrics.Tag("t1", "33"), bmetrics.Tag("t2", 34))
}

```


## todo
 - 接入v3 https://code.byted.org/gopkg/metrics/tree/master/v3 
 

