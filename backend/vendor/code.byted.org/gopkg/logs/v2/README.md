# Go Log SDK V2

Next generation log SDK, which takes blazing fast performance and friendly API.

## Feature

- At least half of the overhead reducing
- Human-friendly and scalable chaining API
- Partial compatibility promise

## Installation

`go get code.byted.org/gopkg/logs/v2`

## Design Document

[Go Log SDK 的性能与可拓展性优化](https://bytedance.feishu.cn/docs/doccniHZOBwOx8kQDUkhgEaz4Kd#pJKjiQ)

## Benchmark

[gopkg/logs/v2 性能测试报告](https://bytedance.feishu.cn/docs/doccnXNthjmrlJkw8GUStzCkhoh#)

## Compatibility

V2 provides a compatible logger, and it supports a part of API in V1:
- logger.Ctx(Debug/Info/...)
- logger.Debug/Info/...
- logger.Ctx(Debug/Info/...)KVs
- logger.Ctx(Debug/Info/...)sf
- CtxAddKVs

You are able to initialize a compatible logger and replace the v1 logger if you used the methods above.
Please mind that the formatted result of these methods is not totally equal to the v1,
check the log content after replacing if your alerting relies on it.

## Use Case

[example](https://code.byted.org/gopkg/logs/blob/master/v2/example/main.go)

## API Introduction in Chinese
[API Introduction](https://bytedance.feishu.cn/docs/doccnBsN8rd3rnHt3MxZJ6V7ABc#)

## API Document

[Go Doc](https://codebase.byted.org/godoc/code.byted.org/gopkg/logs/v2)

## Develop
Before you send out a new MR, make sure you already pass the tests with the following
commnd.
```bash
v2 $ go test -v -race -coverpkg=./... -cover -coverprofile=c.out ./...
```
