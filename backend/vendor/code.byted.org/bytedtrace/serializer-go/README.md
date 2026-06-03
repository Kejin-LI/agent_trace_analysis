# 设计文档
https://bytedance.feishu.cn/docs/doccndEvBDduyL1SiwzUBHYrnBh
# Serializer使用示例

（1）注意，如果你想使用压缩，请确保已经初始化好序列化器。在你的代码中使用：
```
import (
	utils "code.byted.org/bytedtrace/bytedtrace-utils-go/compressor_impl"
	serializer "code.byted.org/bytedtrace/serializer-go/compress"
)

// 注册一个zstd压缩器到compress中
func RegisterCompressor(T) {
	// 注册一个zstd compressor
    serializer.RegisterCompressor(serializer.ZSTD_FAST, utils.NewFastZSTDCompressor(10240, 1))
}
```
（2）开始使用serializer

```
	var bigEndian = false
	opt := serializer.SOption{
		BigEndian: &bigEndian,
	}
	//生成一个serializer
	s := serializer.NewSerializer(serializer.FlowSerializerType, opt)
	var d byte = 0
	var b = true
	var i uint64 = 10
	var timestamp uint64 = 1585884055
	var source = "psm"
	var context = "context"
	var logId = "2020040212350000"

	headerOptions := &serializer.HeaderOptionFlow{
		Version:            &d,
		ProtoType:          &d,
		Compression:        &b,
		Reserved:           &d,
		UserId:             &i,
		ContentCompression: &b,
	}
	//serializer配置
	e := s.SetHeader(headerOptions)
	if e != nil {
		fmt.Println(e)
	}
	//设置 commonHeader
	e = s.SetCommonHeaders([]*serializer.KeyValue{
		{Key: "header_key0", Value: "value"},
		{Key: "header_key1", Value: 1},
	})
	if e != nil {
		fmt.Println(e)
	}
	//写入数据
	e = s.Feed(&serializer.DataPack{
		LogHeader: &serializer.LogHeader{
			ReserveHeader: &serializer.LogHeaderOptionFlow{
				Timestamp: &timestamp,
				Source:    &source,
				Context:   &context,
				LogId:     &logId,
			},
			CustomizeHeader: []*serializer.KeyValue{
				{Key: "customHeader0", Value: "customHeaderValue0"},
				{Key: "customHeader1", Value: false},
			},
		},
		LogContent: &serializer.LogContent{
			KeyValues: []*serializer.KeyValue{
				{Key: "content0", Value: false},
				{Key: "content1", Value: "value"},
				{Key: "content2", Value: 1001},
				{Key: "content3", Value: math.MaxInt32},
				{Key: "content4", Value: math.MaxFloat64},
			},
		},
	})
	if e != nil {
		fmt.Println(e)
	}
	e = s.Feed(&serializer.DataPack{
		LogHeader: &serializer.LogHeader{
			ReserveHeader: &serializer.LogHeaderOptionFlow{
				Timestamp: &timestamp,
				Source:    &source,
				Context:   &context,
				LogId:     &logId,
			},
			CustomizeHeader: []*serializer.KeyValue{
				{Key: "customHeader0_1", Value: "customHeaderValue0"},
				{Key: "customHeader1_1", Value: false},
			},
		},
		LogContent: &serializer.LogContent{
			KeyValues: []*serializer.KeyValue{
				{Key: "content0_1", Value: false},
				{Key: "content1_1", Value: "value"},
				{Key: "content2_1", Value: 1001},
				{Key: "content3_1", Value: math.MaxInt32},
				{Key: "content4_1", Value: math.MinInt16},
			},
		},
	})
	if e != nil {
		fmt.Println(e)
	}
	//获取编码后的数据
	bs, _ := s.Serialize()
	fmt.Print(bs)

	//解码测试
	ds := serializer.NewDeserializer(serializer.FlowSerializerType, false)
	if e != nil {
		fmt.Println(e)
	}

	e = ds.Read(bs)
	if e != nil {
		fmt.Println(e)
	}

	o, e := ds.GetOptions()
	fmt.Println(o)

	patterns, e := ds.GetPatterns()
	fmt.Println(patterns)

	commonHeaders, e := ds.GetCommonHeaders()
	fmt.Print(commonHeaders)

	logHeaders, e := ds.GetLogHeaders()
	fmt.Print(logHeaders)

	logContents, e := ds.GetLogContents()
	fmt.Print(logContents)
```
