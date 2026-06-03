# ctxvalues

[![](https://badge.byted.org/view-count/gl-gopkg-ctxvalues)]()
[![](https://badge.byted.org/ci/passed/gopkg/ctxvalues)]()
[![](https://badge.byted.org/ci/coverage/gopkg/ctxvalues)]()
[![](https://badge.byted.org/go/depended-count/code.byted.org/gopkg/ctxvalues)]()
[![](https://badge.byted.org/go/version/byte-gitlab/gopkg/ctxvalues)]()
[![](https://badge.byted.org/go/doc/gopkg/ctxvalues)](https://godoc.byted.org/pkg/code.byted.org/gopkg/ctxvalues/)

package ctxvalues is reader and writer for context.Context

ctxvalues 是一个针对 context.Context 内变量进行读/写的包

## how to install / 如何安装

```shell script
go get code.byted.org/gopkg/ctxvalues
```

## how to use / 如何使用

### import / 导入包依赖

```go
import "code.byted.org/gopkg/ctxvalues"
```

### logid

- get logid

```go
logid, _ := ctxvalues.LogID(ctx)
```

- set logid / 设置 logid

```go
ctx = ctxvalues.SetLogID(ctx, logid)
```

### stress_tag / 压测标签

- get stress_tag / 获取压测标签

```go
stressTag, _ := ctxvalues.StressTag(ctx)
```

- get is_enable stress / 获取是否启用压测

```go
isEnable, _ := ctxvalues.IsEnableStress(ctx)
```

### method / RPC 方法

```go
method, _ := ctxvalues.Method(ctx)
```

### env / 运行环境

```go
env, _ := ctxvalues.Env(ctx)
```

## ChangeLog / 更新日志

- v0.6.0 2024.01.15
  - add: add detaches from the cancellation and error handling
- v0.5.0 2023.05.06
  - add: add IsEnableStress getter
- v0.4.0 2021.02.03
  - add: add EnvDefault getter
- v0.3.0 2021.02.03
  - add: add Env getter
- v0.2.0 2020.12.03
  - add: add LogIDDefault to get logid or default val `-`
  - opt: LogID return (val, ok), if ok == false, then val must be empty string
- v0.1.1 2020.12.03
  - fix: fix panic when value is nil *string
  - opt: LogID return default value: `-`
- v0.1.0 2020.11.30
  - add: add StressTag getter
  - add: add Method getter
- v0.0.1 2020.08.20
  - add: add LogID getter and setter
