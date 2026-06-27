package tccclient

import (
	"fmt"
	"strings"
)

type clientType string

const (
	clientTypePath clientType = "PATH"
	clientTypeKeys clientType = "KEYS"

	defaultClientType   = clientTypePath
	defaultWatchPath    = "/default"
	defaultBlockingInit = true

	rootDir      = "/"
	dirSeparator = "/"
)

type (
	// clientV3Option 修改字段时，需要修改 genUniqName 函数；保证 clientManager 中 clientV3 的唯一性
	clientV3Option struct {
		bccClientType     clientType // Client 类型: PATH 或 KEYS
		watchPath         string     // 监听的目录
		watchKeys         []WatchKey // 监听的配置列表
		watchSubDirectory bool       // 是否开启递归监听
		withNewCoreClient bool       // 是否允许重复监听
		blockingInit      bool       // 是否开启阻塞式初始化

		// 实时回调函数:
		// 1. 当监听目录时，目录下任意 key 发生变更都会触发回调
		// 2. 当监听多个配置时，任意配置发生变更都会触发回调
		realtimeCallback RealtimeCallback
	}

	ClientV3Option func(o *clientV3Option)
)

func (o *clientV3Option) genUniqName() string {
	return strings.Join([]string{
		"bccClientType", fmt.Sprint(o.bccClientType),
		"watchPath", o.watchPath,
		"watchKeys", fmt.Sprint(o.watchKeys),
		"watchSubDirectory", fmt.Sprint(o.watchSubDirectory),
		"withNewCoreClient", fmt.Sprint(o.withNewCoreClient),
		"blockingInit", fmt.Sprint(o.blockingInit),
		"realtimeCallback", fmt.Sprint(o.realtimeCallback),
	}, ":")
}

func (o *clientV3Option) check() error {
	if o.bccClientType != clientTypePath && o.bccClientType != clientTypeKeys {
		return fmt.Errorf("invalid client type: [%v]", o.bccClientType)
	}

	if o.bccClientType == clientTypePath {
		if o.watchPath == "" || o.watchPath == rootDir {
			return fmt.Errorf("can`t watch empty directoy or root directory(`/`): [%v], please use WithPath() to set a valid path. e.g. `/default`", o.watchPath)
		}
		if !strings.HasPrefix(o.watchPath, dirSeparator) {
			return fmt.Errorf("the parameter of WithPath() must start with `/`")
		}
		if strings.HasSuffix(o.watchPath, dirSeparator) {
			return fmt.Errorf("the parameter of WithPath() can`t end with `/`")
		}
	}

	if o.bccClientType == clientTypeKeys {
		if len(o.watchKeys) == 0 {
			return fmt.Errorf("can`t watch empty keys, please set at least one key by WithKeys()")
		}
	}

	return nil
}

// 监听某个目录下的全部配置，path 为目录名，e.g. "/default", "/my", 不能为 "" 或 "/"
func WithPath(path string) ClientV3Option {
	return func(o *clientV3Option) {
		o.watchPath = path
		o.bccClientType = clientTypePath
	}
}

// 当使用 WithKeys() 指定监听的 key 时，即使配置被删除，SDK 本地缓存依然生效；
// 如不符合业务预期，请使用 WithPath() 方法
func WithKeys(keys []WatchKey) ClientV3Option {
	return func(o *clientV3Option) {
		o.watchKeys = keys
		o.bccClientType = clientTypeKeys
	}
}

func WithRecurse() ClientV3Option {
	return func(o *clientV3Option) {
		o.watchSubDirectory = true
	}
}

func WithNewCoreClient(new bool) ClientV3Option {
	return func(o *clientV3Option) {
		o.withNewCoreClient = new
	}
}

func WithBlockingInit(blockingInit bool) ClientV3Option {
	return func(o *clientV3Option) {
		o.blockingInit = blockingInit
	}
}

func WithRealtimeCallback(cb RealtimeCallback) ClientV3Option {
	return func(o *clientV3Option) {
		o.realtimeCallback = cb
	}
}
