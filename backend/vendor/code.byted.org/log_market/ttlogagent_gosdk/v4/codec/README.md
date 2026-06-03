# ByteLog Codec For Log 2.0

## Installation

`go get -u code.byted.org/log_market/ttlogagent_gosdk/v4/codec`

## Design Document
[StreamLog 2.0 Go SDK Codec文档](https://bytedance.feishu.cn/docs/doccnZZMDIzRZJxUcksveWDg9Fd)


## ByteLog Batch
LogBatch是LogSDK往Log Agent发送的数据。
接口有两个实现：ByteLogBatch和FlatByteLogBatch。 在设计上两者可以混用，FlatByteLogBatch性能稍好。

ByteLogBatch是常规的结构化数据结构
https://bytedance.feishu.cn/wiki/wikcndunVA0UgfBUTCi4qMWMqUc#ULmQd7
```go
type ByteLogBatch struct {
    header     *SDKHeader
    logMessage *ByteLogMessage
    sdkHeaderBuf []byte
}
```
而FlatByteLogBatch则是为了追求性能性能，直接将数据以bytes的方式放在内存中，牺牲了可读性，但是编码速度更快。
具体例子如下：\
[bytelog codec example](https://code.byted.org/log_market/ttlogagent_gosdk/master/v4/codec/example/example.go)


### ByteLog Message
```go
type ByteLogMessage struct {
	byteLogHeader *ByteLogHeader
	pattern       *ByteLogPattern
	commonHeaders *CommonHeaders
	dataPacks     []*DataPack

	firstTimeStamp uint64
	lastTimeStamp  uint64
	currSize       int

	flags             uint32
	uuid              uuid.UUID
	seqId             uint64
	uuidBuf           []byte
	logHeadersAreaEnd int
	byteLogHeadersBuf []byte
	commonHeadersBuf  []byte
}
```
ByteLog Message是Agent之后链路传输所用的数据，是ByteLog Batch相比少了SDK Header。 
现在已经单独抽象出一个模块来使用。它同样有一个替代品，FlatByteLogMessage。



### CommonHeaders
CommonHeader是一个Batch内所有日志共享的属性，比如ip,cluster, podname等等。

### DataPack
每个DataPack对应一个Log，存储每条日志的独特部分，比如Level，Msg，LogID等等。 每个DataPack由两部分组成，LogHeader和LogContent。
LogHeader内存储了一些短属性，比如level，timestamp等等，长度不能超过0xFF。LogContent内存储了原文，和用户自定义长属性。

DataPack应该都用NewDataPack创建，由Codec包负责管理生命周期，请谨慎手动回收。


## Benchmark
[Go Log 2.0 SDK/Codec性能测试](https://bytedance.feishu.cn/docs/doccnKTDeE63W20S11Ewh1fvPFf)


