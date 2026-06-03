## 平台

[动态配置中心](https://cloud.bytedance.net/tcc/namespace/)

## 文档

[动态配置中心介绍](https://cloud.bytedance.net/docs/tcc/docs/646447998e0ca902369dc10e/64647a529a1f99021ed3c665?x-resource-account=public)

[平台功能指南](https://cloud.bytedance.net/docs/tcc/docs/646447998e0ca902369dc10e/64648389a9ea7d021d358efa?x-resource-account=public)

## 动态配置获取方法

### 如何安装

```bash
go get code.byted.org/gopkg/tccclient/v3
```

### 基础用法

```go
package main

import (
	"context"
	"fmt"
	"time"

	tccclientV3 "code.byted.org/gopkg/tccclient/v3"
)

var (
	clientV3 *tccclientV3.ClientV3

	watchPath = "/default" // 监听的目录，不填则默认为 /default 目录
	myConf    = "key1"     // 读取的配置名
)

// clientV3 为全局变量，只在服务启动时初始化一次
func initClientV3() error {
	var err error
	clientV3, err = tccclientV3.NewClientV3("tcc.sdk.demo",
		tccclientV3.WithPath(watchPath))
	if err != nil {
		return err
	}

	return nil
}

func main() {
	if err := initClientV3(); err != nil {
		panic(err)
	}

	for range time.NewTicker(10 * time.Second).C {
		ctx := context.Background() // 如果使用了框架，应该用框架的ctx，禁止传nil
		value, err := clientV3.Get(ctx, watchPath, myConf)
		fmt.Println(value, err)

        // 遍历全部配置
		keyList, err := clientV3.KeyList(ctx)
		if err != nil {
			fmt.Println(err)
		}
		for index, key := range keyList {
			v, err := clientV3.Get(ctx, key.Directory, key.ConfName)
			if err != nil {
				fmt.Printf("index: %d, key: %s, err: %v\n", index, key, err)
				continue
			}
			fmt.Printf("index: %d, key: %s, value: %s\n", index, key, v)
		}
	}
}
```

### 使用解析缓存 Getter API

大部分场景下，获取配置之后需要Unmarshal来获得golang对象，使用Getter API可以**安全**，**便捷**的获取解析+缓存的值对象。

#### 示例

比如现在我们有个如下配置，存在tcc的`hello_key`中：

```json
{
  "def": {
    "ggg": {
      "name": "Toby",
      "year": 25,
      "like": ["janus", "thrift", "runtime-static"]
    }
  }
}
```

那我们可以通过如下代码来获取对象

```go
var (
    once sync.Once
    client *tccclientV3.ClientV3
)

func TccClient() *tccclientV3.ClientV3 {
    once.Do(func(){
        var err error
        client, err = tccclientV3.NewClientV3("tcc.sdk.demo", tccclientV3.WithPath("/default"))
        if err != nil {
            panic(err)
        }
    })
    return client
}

type User struct {
    Name string   `json:"name"`
    Year int      `json:"year"`
    Like []string `json:"like"`
}
type Conf struct {
    Def struct {
        Ggg User`json:"ggg"`
    } `json:"def"`
}
// 初始化 Getter 传入的目录需要在 NewClientV3 时的 WithPath 中指定，否则会读不到配置
var GetConf = TccClient().NewGetter("/default", "hello_key", json.Unmarshal, Conf{})

func Biz(ctx context.Context) {
    inf, err := GetConf(ctx)   // 错误场景会fallback到上次可用值，不需要过于在意错误
    if err != nil {
        // 最好还是打印一下错误
        // 如果为 config not found error，业务需要决策是否继续使用缓存值
        logs.CtxWarn(ctx, "get conf failed %#v", err)
    }
    defConf, _ := inf.(Conf) // 类型能对应上，不会有问题
}
```

这里不用太过于在乎`GetConf`的`error`返回，只要这个业务逻辑不是很重要，因为会使用最近一次合法值，具体的取值顺序，从前往后为：

- 上次error为空时的返回值
- 如果NewGetter中的第三参数为非指针，则返回该对象
- 返回第三参数对应的零值

如果你完全无法接受首次获取失败的零值，你可以如下处理（其他情况下都有上次可用值作为缓存返回，或者给定的零值）：

```go
func Biz(ctx context.Context) {
    inf, err := GetConf(ctx)
    if err, ok := err.(ErrFallbackDefault); ok {
        // 如果你连零值都无法接受，panic是唯一的做法
        panic(err)
    }
    defConf, _ := inf.(Conf)
}
```

如果你可以接受一个fallback值，那可以在代码中指定，例如：

```go
var GetConf = TccClient().NewGetter("/default", "not_exist", json.Unmarshal,
    Conf{Def:{GGG:{"Toby", 25, {"janus","thrift","runtime-static"}}}})
func Biz(ctx context.Context) {
    inf, err := GetConf(ctx)
    val, _ := inf.(Conf)
    // 这里会取到你所填入的fallback值
    if val.Def.GGG.Name == "Toby" && err != nil {
        fmt.Println("fallbacked!")
    }
}
```

这里不推荐第三参数填入指针对象，例如`&Conf{}`，如果有对于返回对象的改动则会直接影响缓存中的对象。

> [【事故复盘】6.24抖音feed严重事故](https://bytedance.feishu.cn/wiki/wikcnZIvpRYudu1J1ZAUW9ud7Kg) 变更隐式共享的指针导致服务停止响应  
> [20220729抖音pack平台故障影响收集](https://bytedance.feishu.cn/docx/doxcnKcqNqd3wWJFSqwqQbscxTe) 并发读写配置中的Map导致panic

如果使用`Conf{}`则你获取的每次都会是个新的浅拷贝，更**安全**。在这条链路上，最贵的是反序列化过程，**安全**地使用反序列化的缓存并没有失去什么。**我们强烈建议你把任何的TCC返回作为只读对象来理解。**

#### SubKey的提取

如上个例子所示，你所获取的对象可能非常的巨大，可是您要获得的只是里面的`User`对象。那么你可以使用`WithSubKey`额外参数来简化提取和类型声明：

```go
// ...
var GetConf = TccClient().NewGetter("/default", "hello_key", json.Unmarshal, User{}, tccclientV3.WithSubKey("def", "ggg"))

func Biz(ctx context.Context) {
    inf, _ := GetConf(ctx)   // 错误场景会fallback到上次可用值，不需要过于在意错误
    defConf, _ := inf.(User) // 类型能对应上，不会有问题
}
```

如果你的配置文件并非TCC官方支持的`json`,`yaml`或者`xml`格式的话，你可以额外指定`WithTag`来支持您的反序列化实现：

```go
//...
var GetConf = TccClient().NewGetter("/default", "hello_key", toml.Unmarshal, User{},
    tccclientV3.WithSubKey("def", "ggg"), tccclientV3.WithTag("toml"))
```

### 不需要Unmarshal的场景

如果字段不涉及unmarshal， 比如只是`string` 或者`[]byte`结果，且没有json语义，可以用
`DummyUnmarshal`实现：

```go
//...
var GetConf = TccClient().NewGetter("/default", "hello_key", tccclientV3.DummyUnmarshal, "")
```

#### 其他类型的获取

您可以指定任意的值类型作为容器中的类型，比如：

- 指针（强烈不推荐）

```go
var GetConf = TccClient().NewGetter("/default", "hello_key", json.Unmarshal, &User{}, tccclientV3.WithSubKey("def", "ggg"))
func Biz(ctx context.Context) {                                  ↑
    inf, _ := GetConf(ctx)                                       |
    defConf, _ := inf.(*User) <--------------------------------- 这两个类型要对应
}
```

- 数组

```go
var GetConf = TccClient().NewGetter("/default", "hello_key", json.Unmarshal, []string{}, tccclientV3.WithSubKey("def", "ggg", "like"))
func Biz(ctx context.Context) {                                   ↑
    inf, _ := GetConf(ctx)                                        |
    defConf, _ := inf.([]string) <------------------------------- 这两个类型要对应
}
```

- 字符串

```go
var GetConf = TccClient().NewGetter("/default", "hello_key", json.Unmarshal, "", tccclientV3.WithSubKey("def", "ggg", "name"))
func Biz(ctx context.Context) {                                  ↑
    inf, _ := GetConf(ctx)                                       |
    defConf, _ := inf.(string) <-------------------------------- 这两个类型要对应
}
```

- 数字

```go
var GetConf = TccClient().NewGetter("/default", "hello_key", json.Unmarshal, int(0), tccclientV3.WithSubKey("def", "ggg", "year"))
func Biz(ctx context.Context) {                                  ↑
    inf, _ := GetConf(ctx)                                       |
    defConf, _ := inf.(int) <-------------------------------- 这两个类型要对应
}
```

- 字符串指针（强烈不推荐）

```go
var GetConf = TccClient().NewGetter("/default", "hello_key", json.Unmarshal, (*string)(nil), tccclientV3.WithSubKey("def", "ggg", "name"))
func Biz(ctx context.Context) {                                   ↑
    inf, _ := GetConf(ctx)                                        |
    defConf, _ := inf.(*string) <-------------------------------- 这两个类型要对应
}
```

- 任何你能想到的值类型

#### 关于`NewCastGetter`

NewCastGetter 创建了一个值容器，每次调用Getter会获取最新版本的对象；您需要传入一个unmarshal的实现，比如json或者yaml的;
您同时需要实现一个Caster函数，对于Unmarshal之后的对象进行类型转换和处理。这里需要注意的是，这个容器将不再返回您传入的类型，而是您Caster中返回的类型；可以参考 NewCastGetter 的实现代码和注释了解更多细节。
推荐优先使用 Getter, 更直接的代码表述你的逻辑，更直接的代码总不会有问题。

### 注册实时回调

#### 场景&作用

如果需要监听某个目录下的变更，并在目录下的配置变更后做一些复杂操作，例如更新本地的一些配置或做一些本地数据处理操作，示例逻辑如下：

```go
  for {
       value = tccclientV3.get(ctx, key)
       if value updated {
            // do something
       }
       time.sleep(interval)
   }
```

这种场景下可以使用实时回调函数 RealtimeCallback, 将回调函数注册给 clientV3, 则监听的目录下任意配置发生变更时都可以选择触发回调函数。

#### 使用

1. 实现一个 RealtimeCallback, 用来对更新后的配置项做处理。RealtimeCallback 提供了以下方法可供使用:

```go
type RealtimeCallback func(item RealtimeCallbackItem) error

type RealtimeCallbackItem interface {
	Namespace() string                              // TCC 服务名
	Directory() string                              // 服务目录
	ConfName() string                               // 配置名
	ClientType() string                             // client 类型: KEYS 或 PATH
	Value() (string, error)                         // 获取配置 value
	Version() (int64, error)                        // 配置的版本号
	PublishTime() (int64, error)                    // 配置的发布时间
}
```

2. 在初始化时，将需要回调的函数注册给 clientV3，可以根据 ConfName() 为不同的配置定制不同的回调逻辑

```go
// clientV3 为全局变量，只在服务启动时初始化一次
func initClientV3() error {
	var err error
	clientV3, err = tccclientV3.NewClientV3("tcc.sdk.demo",
		tccclientV3.WithPath(watchPath),
		tccclientV3.WithRealtimeCallback(callbackByPath()))
	if err != nil {
		return err
	}

	return nil
}

// 监听目录时，目录下任意配置更新都会触发回调
// 可以根据item.ConfName()，过滤无关配置
func callbackByPath() tccclientV3.RealtimeCallback {
	return func(item tccclientV3.RealtimeCallbackItem) error {
		namespace := item.Namespace()
		directory := item.Directory()
		confName := item.ConfName()
		clientType := item.ClientType()
		logs.Info("clientType: %v, namespace: %v directory: %v confName: %v has updated! ",
			clientType, namespace, directory, confName)

		value, err := item.Value()
		if err != nil {
			logs.Error("get item.Value err %v", err)
			return errors.Wrap(err, "item.Value")
		}

		// 只感知当前配置的变更，并触发回调
		if directory == watchPath && confName == myConf {
			data := &MyStruct{}
			if err := json.Unmarshal([]byte(value), data); err != nil {
				logs.Errorf("parse dir=%v confName=%v failed err=%v", directory, confName, err)
				return err
			}
			logs.Info("update data: %v", data)
		}

		// TODO: 处理其他配置的回调
		return nil
	}
}
```
