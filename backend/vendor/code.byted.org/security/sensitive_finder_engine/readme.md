> ## ❗V1版本已经不再维护，新用户请直接使用[V2版本](https://code.byted.org/security/sensitive_finder_engine/tree/master/v2) <br> V1 is no longer maintained, For new users, please use [V2](https://code.byted.org/security/sensitive_finder_engine/tree/master/v2)

脱敏 SDK
说明可以查看： [https://bytedance.feishu.cn/docs/doccn5TiLGdslZVV1LV4fJ5teeN](https://bytedance.feishu.cn/docs/doccn5TiLGdslZVV1LV4fJ5teeN)

Python 版本脱敏 SDK：[https://code.byted.org/security/sensitive_finder](https://code.byted.org/security/sensitive_finder)

规则引擎说明可以查看：[https://bytedance.feishu.cn/docs/doccnG4EvKMnRVl7K8IA5vTpo7g](https://bytedance.feishu.cn/docs/doccnG4EvKMnRVl7K8IA5vTpo7g)

Python 版本规则引擎
SDK：[https://code.byted.org/security/secdata_rule_engine](https://code.byted.org/security/secdata_rule_engine)

接口脱敏查看：[https://bytedance.feishu.cn/docs/doccnrvdLWZx409LEA1ku8D76NX](https://bytedance.feishu.cn/docs/doccnrvdLWZx409LEA1ku8D76NX)

可以关注 "日志脱敏" 、 "接口脱敏" 部分（当前页面 ctrl-f 进行搜索）。

## 安装

```
go get code.byted.org/security/sensitive_finder_engine
```

## 测试

进入仓库目录下，运行 `go test`。也可以查看相关单元测试文件了解如何使用。

## 数据脱敏

### 检测与脱敏 - 针对key-value或ascii编码等数据

首先创建 `Finder` ，之后调用相关函数进行脱敏。针对的数据类型为字符串值类型，如手机号的字符串。（只传 value 不传 key 暂时不支持）

- func (f *Finder) FindSensitive(value string) *FinderResult ：寻找数据中的敏感数据类型，参数为数据。
- func (f *Finder) MaskSensitive(value string) string ：对数据进行脱敏，参数为数据。
- func (f *Finder) FindSensitiveWithKey(key, value string) *FinderResult ：寻找数据中的敏感数据类型，参数为数据和数据的键名。
- func (f *Finder) MaskSensitiveWithKey(key, value string) string ：对数据进行脱敏，参数为数据和数据的键名。

```
package main

import (
	"code.byted.org/security/sensitive_finder_engine"
	"fmt"
	"log"
)

func main() {
	f, err := sensitive_finder_engine.NewFinder()
	if err != nil {
		log.Fatal(err.Error())
	}

	valueList := map[string][]string{
		"": {
			"123123199010100010",
			"13312341234",
			"P1234567",
			"EM1234123",
			"010-4012312",
			"jintianlu@qq.com",
			"abcdefg",
		},
	}
	for k, vl := range valueList {
		for _, v := range vl {
			fmt.Println(f.MaskSensitiveWithKey(k, v))
		}
	}
}
```

### 检测与脱敏 - 针对中文文本段落

首先创建 `ChineseFinder` ，之后调用相关函数进行脱敏。针对的数据类型为中文文本段落，如一句话。

- func (f *ChineseFinder) FindSensitive(value string) []FinderResult ：寻找数据中的敏感信息，并返回检测结果
- func (f *ChineseFinder) MaskSensitive(value string) string ：对数据进行脱敏，返回脱敏后的结果

```
package main

import (
	"code.byted.org/security/sensitive_finder_engine"
	"fmt"
	"log"
)

func main() {
	f, err := sensitive_finder_engine.NewChineseFinder()
	if err != nil {
		log.Fatal(err.Error())
	}

    valueList := []string{
        "我来到了北京，这是我的手机号，请保存好：13312341234",
        "晚上我们去吃海底捞吧，我的身份证号是210321199010100011，我家在海淀区一号楼3单元",
        "尹紫晓你还好吗，一会去海淀区的医院吧",
        "我的邮箱是 jintianlu@qq.com",
        "我家住在北京市海淀区北三环西路43号中航广场员工小邮局。",
        "我于2018年1月1日获得1000万美金奖励。",
        "海淀区学院路冶金小区兜底楼抖动单元",
        "海淀区知春路太阳园小区迭代楼迭代给广告",
        "010-222233334444",
    }
    for _, vl := range valueList {
        fmt.Println(f.MaskSensitive(vl))
    }
}
```

### 检测与脱敏 - 针对流式文本

首先创建 `StreamTextFinder` ，之后调用相关函数进行脱敏。针对的数据类型为流式文本，如服务日志。

- func (s StreamTextFinder) Match(data []byte) []string ：寻找文本中的敏感信息，并返回检测结果
- func (s StreamTextFinder) MatchAndMask(data string) string ：对文本进行脱敏，返回脱敏后的结果

脱敏只针对有记录的数据安全地图标签，如果需要自定义，使用函数 `SetTagIDToKind`，参数是 标签id 和 对应的类型：

```
sensitive_finder_engine.SetTagIDToKind(map[int]string{1: "unknown"})
```

如果需要在系统规则基础上自定义规则（指定敏感类型和正则表达式）：

```
f, err := sensitive_finder_engine.NewStreamTextFinder()
if err != nil {
    panic(err)
}
err = f.SetCustomRules([]sensitive_finder_engine.StreamTextFindCustomRule{
    {
        Kind: sensitive_finder_engine.SensitiveKindMobilePhoneNumber,
        Rule: "(?i)(virtual_number|caller_number|callee_number)s?\\\\?[\"']?\\s{0,5}[:=]?\\s{0,5}(map\\[)?\\[?u?\\\\?[\"']?([1](([3][0-9])|([4][5-9])|([5][0-3,5-9])|([6][5,6])|([7][0-8])|([8][0-9])|([9][1,8,9]))[0-9]{8}|0\\d{2,3}-?\\d{7,8})",
    },
})
if err != nil {
    logs.Error(err.Error())
}
```

### 直接脱敏

如果已知数据的敏感类型，不需要进行检测，直接执行脱敏即可。

- func MaskDataWithKind(data, kind string) string ：第一个参数是需要脱敏的数据。第二个参数是数据的类型。

如：

```
sensitive_finder_engine.MaskDataWithKind("jintianlu@qq.com", sensitive_finder_engine.SensitiveKindEmailAddress)
```

## 规则引擎

首先创建 `RuleEngine` :

```
e, err := sensitive_finder_engine.NewRuleEngine(sensitive_finder_engine.PublicToken)
```

如果需要使用自己的 token ，参考 [token获得](https://bytedance.feishu.cn/space/doc/doccnWaKXSqUS2uwROXJVYcnPhh#PJ3REe) 。

```
token = "secdata_token"
e, err := sensitive_finder_engine.NewRuleEngine(token)
```

创建好 `RuleEngine` 即可进行数据的匹配：

- func (e RuleEngine) Match(data Element) []RuleDocker

如：

```
e, err := sensitive_finder_engine.NewRuleEngine(sensitive_finder_engine.PublicToken)
if err != nil {
    log.Fatal(err.Error())
}

record := sensitive_finder_engine.Element{
    Key:         "email",
    Value:       []string{"jintianlu@qq.com"},
    Description: "",
    Type:        "string",
}
results := e.Match(record)
fmt.Printf("match email records: %+v\n", results)
```

### 日志脱敏

日志脱敏是业务常见场景，提供两个processor可以被直接安装在logger上面进行日志脱敏

添加processor，脱敏一般敏感信息，对应上面的`Finder` （日志脱敏使用这个就可以了）

```
logs.DefaultLogger().AddProcessor(MaskSensitiveLogsProcessor())

// 下面这个开销更小
logs.DefaultLogger().AddProcessor(sensitive_finder_engine.SecdataEngineSensitiveLogsProcessor())
```

如果只想脱敏某种类型的数据：

```
logs.DefaultLogger().AddProcessor(MaskSensitiveLogsProcessorWithKind([]string{
    sensitive_finder_engine.SensitiveKindMobilePhoneNumber,
    sensitive_finder_engine.SensitiveKindTelephoneNumber,
}))

logs.DefaultLogger().AddProcessor(SecdataEngineSensitiveLogsProcessorWithKind([]string{
    sensitive_finder_engine.SensitiveKindMobilePhoneNumber,
    sensitive_finder_engine.SensitiveKindTelephoneNumber,
}))
```

-__注意：对于 kite 框架，要放在 kite.init 后面、kite.run 前面，tmq框架可能同理

#### 抽样脱敏

对于部分服务，可能只有部分情况才打印敏感数据，对打印的全量日志进行脱敏会产生较大的资源浪费。因此可以记录打印敏感日志的位置，只在需要脱敏时候才运行脱敏逻辑。可以参考 https://bytedance.feishu.cn/docs/doccnFaQ6dp4PdAyWIeGqpAmR1f#
。

需要使用该功能要运行如下语句：

```
sensitive_finder_engine.SetUseLogRateLimit(true)
```

部分参数设置：

```
// 大于多少比例认为需要脱敏
func SetSensitiveRate(rate float64) {
	sensitiveRate = rate
}

// 数量达到多少时计算比例
func SetMaskCount(count int) {
	maskCount = count
}
```

#### 自定义脱敏规则

如果在系统规则基础上需要自定义规则（指定敏感类型和正则表达式）：

```
logs.DefaultLogger().AddProcessor(sensitive_finder_engine.MaskSensitiveLogsProcessorWithRules([]sensitive_finder_engine.StreamTextFindCustomRule{
    {
        Kind: sensitive_finder_engine.SensitiveKindMobilePhoneNumber,
        Rule: "(?i)(virtual_number|caller_number|callee_number)s?\\\\?[\"']?\\s{0,5}[:=]?\\s{0,5}(map\\[)?\\[?u?\\\\?[\"']?([1](([3][0-9])|([4][5-9])|([5][0-3,5-9])|([6][5,6])|([7][0-8])|([8][0-9])|([9][1,8,9]))[0-9]{8}|0\\d{2,3}-?\\d{7,8})",
    },
}))
```

几种例子

```
# 针对邮箱
logs.DefaultLogger().AddProcessor(sensitive_finder_engine.MaskSensitiveLogsProcessorWithRules([]sensitive_finder_engine.StreamTextFindCustomRule{
    {
        Kind: sensitive_finder_engine.SensitiveKindEmailAddress,
		Rule: "(?i)([A-Za-z0-9_\\-\\.])+\\@([A-Za-z0-9_\\-\\.])+\\.([A-Za-z]{2,8})",
    },
}))
# 针对用户 id 为手机号
logs.DefaultLogger().AddProcessor(sensitive_finder_engine.MaskSensitiveLogsProcessorWithRules([]sensitive_finder_engine.StreamTextFindCustomRule{
    {
        Kind: sensitive_finder_engine.SensitiveKindMobilePhoneNumber,
        Rule: "(?i)(value|user_id)s?\\\\?[\"']?\\s{0,5}[:=]?\\s{0,5}(map\\[)?\\[?u?\\\\?[\"']?([1](([3][0-9])|([4][5-9])|([5][0-3,5-9])|([6][5,6])|([7][0-8])|([8][0-9])|([9][1,8,9]))[0-9]{8}|0\\d{2,3}-?\\d{7,8})",
    },
}))
```

同样可以使用自定义finder，通常用户自定义规则或重用finder对象

```
logs.DefaultLogger().AddProcessor(MaskSensitiveLogsProcessorWithFinder(finder))
```

#### 脱敏开关

如果需要使用开关来控制是否需要脱敏

```
logs.DefaultLogger().AddProcessor(sensitive_finder_engine.MaskSensitiveLogsProcessorWithSwitch(func() bool {
    return 40 > rand.Intn(100)
}))
```

#### 自定义脱敏函数

```
package main

import (
	"code.byted.org/security/gokms"
	"code.byted.org/security/sensitive_finder_engine"
	"fmt"
)

var aesCipher *gokms.AesCipher

func init() {
	aesCipher, _ = gokms.NewAesCipher("1p6LvbLHSs8yKNUpTN8fgNrUJ9tCqtIr", 0, 0, 0, "p.s.m")
}

func CustomMasker(kind string) *sensitive_finder_engine.MaskFunc {
	// 判断类型
	//if kind == sensitive_finder_engine.SensitiveKindMobilePhoneNumber || kind == sensitive_finder_engine.SensitiveKindTelephoneNumber {
	//	//
	//}
	tmp := func(data string, position sensitive_finder_engine.MaskPosition) string {
		res, _ := aesCipher.Encrypt(data)
		return res
	}
	return (*sensitive_finder_engine.MaskFunc)(&tmp)
}

func main() {
	data := "logid: 11, req: email: jintianlu@qq.com vvvv"
	s, err := sensitive_finder_engine.NewStreamTextFinderWithCustomMasker(CustomMasker)
	if err != nil {
		panic(err.Error())
	}
	fmt.Println(s.MatchAndMask(data))
}
```

#### 不加载默认规则

```
rules, err := sensitive_finder_engine.NewStreamTextFinderWithKindAndRules([]string{		"custom",
	}, []sensitive_finder_engine.StreamTextFindCustomRule{
		{
			Kind: "custom",
			Rule: `(?i)(phone)["']?[:=]?\s?["']?\S{2,}`,
		},
	})
```

#### 处理中文敏感信息（很少使用）

添加processor，处理中文敏感信息，对应上面的`ChineseFinder`

```
logs.DefaultLogger().AddProcessor(MaskchineseSensitiveLogsProcessor())
```

### 接口脱敏

目前支持了 hertz 和 kite 框架，其他框架有需求可以联系 @jintianlu 。

使用方式参考：[https://bytedance.feishu.cn/docs/doccnrvdLWZx409LEA1ku8D76NX](https://bytedance.feishu.cn/docs/doccnrvdLWZx409LEA1ku8D76NX)

### benchmark

#### 日志脱敏

默认开启全部规则

```
goos: darwin
goarch: amd64
pkg: code.byted.org/security/sensitive_finder_engine/benchmarks
BenchmarkLogGeneration-8                         	  649170	      1826 ns/op
BenchmarkLogGenerationStreamText-8               	   61333	     18609 ns/op
BenchmarkLogGenerationTreeEngine-8               	  321674	      3729 ns/op
BenchmarkLogGenerationNotSensitive-8             	  750021	      1840 ns/op
BenchmarkLogGenerationNotSensitiveStreamText-8   	   68733	     18022 ns/op
BenchmarkLogGenerationNotSensitiveTreeEngine-8   	   55938	     20996 ns/op
BenchmarkLogGenerationSensitive-8                	  679249	      1564 ns/op
BenchmarkLogGenerationSensitiveStreamText-8      	    4077	    296569 ns/op
BenchmarkLogGenerationSensitiveTreeEngine-8      	   51603	     24419 ns/op
PASS
ok  	code.byted.org/security/sensitive_finder_engine/benchmarks	15.294s
```
