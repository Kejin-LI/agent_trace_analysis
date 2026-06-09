# BytedGORM

GORM Dialector for ByteDance

## Usage

```go
import (
  "code.byted.org/gorm/bytedgorm"
  "gorm.io/gorm"
)

// 初始化
DB, err := gorm.Open(
  // 默认使用 inf.lidar.data_write 作为 db
  bytedgorm.MySQL("inf.lidar.data" /*psm*/, "data" /*dbname*/).With(func(conf *bytedgorm.DBConfig) {
    // 通过 conf 选项可修改数据库连接的配置信息
    conf.ReadTimeout = 2*time.Second
  // WithReadReplicas 开启读写分离, 将使用inf.lidar.data_write 做为写db, inf.lidar.data_read 做为读db, 不开启时将使用 inf.lidar.data_write 进行读写
  }).WithReadReplicas(),
  // 配置连接池的信息
  bytedgorm.ConnPool{MaxIdleConns: 200, MaxOpenConns: 200},
  // 使用订制的 logs 做为 logger
  bytedgorm.Logger{LogLevel: logger.Error, IgnoreRecordNotFoundError: true},
  // 允许压测模式，在使用压测平台进行压力测试时，会使用影子表进行查询等
  bytedgorm.WithStressTestSupport(),
  // 使用单数作为表名
  bytedgorm.WithSingularTable(),
  // 默认通过 logger 打印 db stats 状态，支持通过WithStatsWithOption配置metrics等高级特性，详细参考stats_test.go
  bytedgorm.WithStats(),
)

// 检查错误
if err != nil {
  panic(err)
}

// 使用 GORM API 进行操作
DB.WithContext(ctx).First(&user, 1)
```

### WithDefaults

`bytedgorm.WithDefaults()` 选项等同于:

```go
bytedgorm.ConnPool{
    ConnMaxIdleTime: 300 * time.Second,
    ConnMaxLifetime: 300 * time.Second,
    MaxIdleConns:    100,
    MaxOpenConns:    200,
}
bytedgorm.Logger{LogLevel: logger.Error}
bytedgorm.WithStressTestSupport()
```

在使用下面的配置时：

```go
DB, err := gorm.Open(bytedgorm.MySQL("inf.lidar.data", "data").WithReadReplicas(), bytedgorm.WithDefaults())
```

将会产生如下效果:

* 本配置将使用读写分离，所有的写操作会使用 `inf.lidar.data_write` 的 `data` 库，读操作将使用 `inf.lidar.data_read` 的 `data` 库, 如不需要读写分离，可以去掉后面的 `.WithReadReplicas` 选项
* 默认的 connection pool 的配置为 max idle time 300s, max lifetime 为 300s, max idle conns 100, max open conns 200
* 使用 logs 打印 log, log level 为 error
* 也会开启 stress test 模式，此时在使用压测平台压测时，读请求会使用影子表查询

### WithStatsWithOption

`bytedgorm.WithStatsWithOption` 对db stats记录log和metrics，传入参数：

```go
type StatsOption struct {
	Duration          time.Duration
	MetricsClient      metrics.Client // option 1: 用户提供MetricsClient，用于需要复用MetricsClient场景.
	WithDefaultMetric bool           // option 2: 使用默认创建Metric，prefix为`inf.gorm`, metricName为`conn`.
}
```

如果`StatsOption`.`MetricsClient`，或者`StatsOption`.`WithDefaultMetric`非零值，则会记录以下metrics（`prefix`默认为"inf.gorm", `metricName`默认为"conn"）：

- ${prefix}.${metricName}.max: 最大连接数
- ${prefix}.${metricName}.open: 打开连接数
- ${prefix}.${metricName}.idle: 空闲连接数
- ${prefix}.${metricName}.inuse: 使用连接数
- ${prefix}.${metricName}.wait_count: 等待连接数
- ${prefix}.${metricName}.wait_duration: 等待总时长（单位是微秒)
- ${prefix}.${metricName}.max_idle_closed: maxIdleConn关闭连接
- ${prefix}.${metricName}.max_idle_time_closed: maxIdleTime关闭连接
- ${prefix}.${metricName}.max_life_time_closed: maxLifetime关闭连接

以上metric类型为Store。tag为`db`, `addr`(对应读写库的psm)和[metric v4默认tag](https://bytedance.feishu.cn/wiki/wikcnwa2OwEBS6eYVYBu8aoBvSh#doxcncUq2OMuSksm2eu9Sv8rMXg)，包括不限于`dc`, `_psm`, `_pod_name`等。



