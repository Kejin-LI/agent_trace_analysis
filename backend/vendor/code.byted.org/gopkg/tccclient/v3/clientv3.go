package tccclient

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	bccsdk "code.byted.org/bcc/bcc-go-client/confclient"
	"code.byted.org/gopkg/logs"
	"github.com/pkg/errors"

	"code.byted.org/gopkg/tccclient/v3/common"
)

type WatchKey struct {
	Directory string // 目录名
	ConfName  string // 配置名
}

var (
	clientV3Manager map[string]*ClientV3
	clientV3Mu      sync.Mutex

	// options for init bccclient
	commonBccOptions = []bccsdk.ClientOption{
		bccsdk.WithClientWatchEmpty(true),
		bccsdk.WithClientIdentifier("tcc"),
		bccsdk.WithClientTccSdkVersion(common.ClientVersion),
	}
)

const (
	initBccTimeout   = 30 * time.Second
	minDelayDuration = 1 * time.Second
	maxDelayDuration = 30 * time.Second
)

func init() {
	clientV3Manager = make(map[string]*ClientV3)
}

type ClientV3 struct {
	// the name of TCC V3 service
	// TCC V3 服务名
	serviceName string
	// param of init ClientV3(), is determined when init ClientV3(). enum: 'PATH','KEYS'
	// 初始化ClientV3()时确定值，初始化BccClient的参数；可枚举值:'PATH'，'KEYS'
	bccClientType clientType
	// param of init ClientV3()，is determined when init ClientV3(). not null when ClientType='PATH'
	// 初始化ClientV3()时确定值，初始化BccClient的参数；ClientType='PATH'时，标识监听的目录
	watchPath string
	// param of init ClientV3()，is determined when init ClientV3(). not null when ClientType='KEYS'
	// 初始化ClientV3()时确定值，初始化BccClient的参数；ClientType='KEYS'时，标识监听的keys
	watchKeys []WatchKey
	// symbol identify if watch subDirectory for KeyList(),is determined when init ClientV3()
	// 初始化ClientV3()时确定值，标识是否监听子目录下的配置
	watchSubDirectory bool
	// withNewCoreClient: if reuse the same bcc core client
	// 标识: 是否复用底层的bcc client链接; 只有重复监听的场景才会用到，尽量避免业务使用
	withNewCoreClient bool
	// if use blocking initialization, get all values of the configurations when new client
	// 是否采用阻塞式初始化方法: 初始化时即拉取监听的全部key
	blockingInit bool
	// the callback function registered for bccClient
	// 为bccClient注册的实时回调函数
	realtimeCallback RealtimeCallback

	// ensure: init BccClient() only once
	// 控制: InitBccClient()只执行一次
	onceInitBcc sync.Once
	// ensure:Close bccCheckChannel only once
	// 控制: bccCheckChannel只关闭一次
	onceCloseChan sync.Once
	// instance of BccClient, init by lazy refresh and only once
	// BccClient的实例，采用lazy refresh的方式初始化且只初始化一次
	bccClient atomic.Value
	// symbol identify if BccClient is initialized，will be closed after initialization
	// 标识BccClient是否初始化完成，完成后关闭此Channel
	bccCheckChannel chan struct{}
	// error: storage the FatalError of bcc_sdk.NewClient
	// error类型: 存储bcc_sdk.NewClient初始化失败的FatalError
	bccClientFatalError atomic.Value
	// non-fatal error: can be handled, should modify input param and retry
	// error类型: 存储bcc_sdk.NewClient初始化失败的TemporaryError, 应修改参数或服务, 重试可恢复
	bccTemporaryError atomic.Value
}

func NewClientV3(serviceName string, opts ...ClientV3Option) (*ClientV3, error) {
	clientV3Mu.Lock()
	defer clientV3Mu.Unlock()

	if serviceName == "" {
		return nil, fmt.Errorf("empty serviceName")
	}

	// 默认监听 `/default` 目录
	oo := clientV3Option{
		bccClientType: defaultClientType,
		watchPath:     defaultWatchPath,
		blockingInit:  defaultBlockingInit,
	}
	for _, op := range opts {
		op(&oo)
	}
	if err := oo.check(); err != nil {
		return nil, err
	}

	clientName := fmt.Sprintf("service:%s:uniqName:%s", serviceName, oo.genUniqName())
	if client, ok := clientV3Manager[clientName]; ok && client != nil {
		return client, nil
	}

	client := &ClientV3{
		serviceName:       serviceName,
		bccClientType:     oo.bccClientType,
		watchPath:         oo.watchPath,
		watchKeys:         oo.watchKeys,
		watchSubDirectory: oo.watchSubDirectory,
		withNewCoreClient: oo.withNewCoreClient,
		blockingInit:      oo.blockingInit,
		realtimeCallback:  oo.realtimeCallback,

		bccCheckChannel: make(chan struct{}),
	}

	if err := client.refresh(); err != nil {
		return nil, err
	}

	clientV3Manager[clientName] = client
	return client, nil
}

func (c *ClientV3) Get(ctx context.Context, directory string, confName string) (string, error) {
	value, _, err := c.getWithBccClient(ctx, directory, confName)
	return value, err
}

func (c *ClientV3) KeyList(ctx context.Context) (keyList []WatchKey, err error) {
	switch c.bccClientType {
	case clientTypeKeys:
		return c.watchKeys, nil
	case clientTypePath:
		return c.getWatchKeyByPath(ctx)
	default:
		// should not reach here
		return nil, fmt.Errorf("invalid bccClientType: %v", c.bccClientType)
	}
}

func (c *ClientV3) getWatchKeyByPath(ctx context.Context) (keyList []WatchKey, err error) {
	bccclient, err := c.getBccClient(ctx)
	if err != nil {
		return nil, err
	}
	currentDirectoryKey := []WatchKey{}
	subDirectoryKey := []WatchKey{}
	bccKeys := bccclient.GetBCKeys()
	for _, key := range bccKeys {
		keyTrimPath := strings.TrimPrefix(key, c.watchPath+dirSeparator)
		isSubKey := strings.Contains(keyTrimPath, dirSeparator)
		if !isSubKey {
			currentDirectoryKey = append(currentDirectoryKey, WatchKey{
				Directory: c.watchPath,
				ConfName:  decodeConfName(keyTrimPath),
			})
		} else {
			if !c.watchSubDirectory {
				continue
			}
			ss := strings.Split(key, dirSeparator)
			// 一级目录下的配置: len(ss) == 3，不应该出现；防止异常情况导致 panic
			if len(ss) < 3 {
				return nil, fmt.Errorf("invalid bccKey: %s", key)
			}
			confName := ss[len(ss)-1]
			path := strings.TrimSuffix(key, dirSeparator+confName)
			subDirectoryKey = append(subDirectoryKey, WatchKey{
				Directory: path,
				ConfName:  decodeConfName(confName),
			})
		}
	}
	keyList = append(keyList, currentDirectoryKey...)
	if c.watchSubDirectory {
		keyList = append(keyList, subDirectoryKey...)
	}
	return keyList, nil
}

func (c *ClientV3) refresh() error {
	// 后台初始化 bccclient
	go func() {
		ctx := context.Background()
		_, _ = c.getBccClient(ctx)
	}()

	// 阻塞式初始化
	var initErr error
	if c.blockingInit {
		ctx := context.Background()
		for {
			_, err := c.getBccClient(ctx)
			if err == nil {
				logs.CtxInfo(ctx, "init bcc client success, service: %s path: %v keys: %v", c.serviceName, c.watchPath, c.watchKeys)
				break
			}
			if bccsdk.IsFatalError(err) {
				logs.CtxError(ctx, "init bcc client failed, fatal err: [%v]", err)
				initErr = err
				break
			}

			logs.CtxWarn(ctx, "init bcc client failed, temporary err: [%v]", err)
		}
	}

	return initErr
}

func (c *ClientV3) getBccClient(ctx context.Context) (bccsdk.ConfClient, error) {
	if c := c.bccClient.Load(); c != nil {
		return c.(bccsdk.ConfClient), nil
	}

	bccOptions := commonBccOptions
	if c.withNewCoreClient {
		bccOptions = append(bccOptions, bccsdk.WithClientNewCoreClient(true))
	}
	if oo := c.withRealtimeCallbackOption(ctx); oo != nil {
		bccOptions = append(bccOptions, oo)
	}

	c.onceInitBcc.Do(func() {
		f := func() {
			var (
				bccClient bccsdk.ConfClient
				err       error
			)
			delayTime := minDelayDuration
			for {
				if c.bccClientType == clientTypeKeys {
					bccKeys := []string{}
					for _, key := range c.watchKeys {
						bccKeys = append(bccKeys, c.genBccKey(key.Directory, key.ConfName))
					}
					bccClient, err = bccsdk.NewClientByKeys(ctx, c.serviceName, bccKeys, bccOptions...)
				} else {
					bccClient, err = bccsdk.NewClientByPath(ctx, c.serviceName, c.watchPath, bccOptions...)
				}
				if err != nil {
					logs.CtxWarn(ctx, "init bcc client failed, err: [%v]", err)
					if bccsdk.IsReuseError(err) {
						bccOptions = append(bccOptions, bccsdk.WithClientNewCoreClient(true))
						c.bccClientFatalError.Store(err)
						logs.CtxWarn(ctx, "start mode withClientNewCoreClient(true)")
						continue
					} else if bccsdk.IsFatalError(err) {
						c.bccClientFatalError.Store(err)
						c.closeBccCheckChan(ctx)
						break
					} else {
						c.bccTemporaryError.Store(err)
					}

					time.Sleep(delayTime)
					for ; delayTime < maxDelayDuration; delayTime = delayTime * 2 {
						if delayTime > maxDelayDuration {
							delayTime = maxDelayDuration
						}
					}
					continue
				}
				c.bccClient.Store(bccClient)
				c.closeBccCheckChan(ctx)
				break
			}
		}
		go f()
	})

	select {
	case <-c.bccCheckChannel:
		if client := c.bccClient.Load(); client != nil {
			return client.(bccsdk.ConfClient), nil
		} else {
			if fatalErr := c.bccClientFatalError.Load(); fatalErr != nil {
				return nil, fatalErr.(error)
			}
			return nil, fmt.Errorf("Internal Error: nil bccClientInitErr")
		}
	case <-time.After(initBccTimeout):
		if tErr := c.bccTemporaryError.Load(); tErr != nil {
			return nil, fmt.Errorf("init bcc timeout: %v, temporary err: %v", initBccTimeout, tErr.(error))
		}
		return nil, fmt.Errorf("init bccclient timeout: %v", initBccTimeout)
	}
}

func (c *ClientV3) closeBccCheckChan(ctx context.Context) {
	c.onceCloseChan.Do(func() {
		defer catchPanic(ctx)

		close(c.bccCheckChannel)
	})
}

// 区分 ConfigNotListenError 和 ConfigNotFoundError
func (c *ClientV3) isConfigNotListenErr(bccKey string, err error) bool {
	if c.bccClientType != clientTypeKeys {
		return false
	}

	if bccsdk.IsConfNotExistError(err) {
		for _, key := range c.watchKeys {
			// 在监听列表中
			if c.genBccKey(key.Directory, key.ConfName) == bccKey {
				return false
			}
		}

		return true
	}

	return false
}

func (c *ClientV3) getWithBccClient(ctx context.Context, directory string, confName string) (value string, versionCode string, err error) {
	bccClient, err := c.getBccClient(ctx)
	if err != nil {
		return "", "", err
	}

	bccKey := c.genBccKey(directory, confName)
	value, updateID, err := bccClient.GetWithUpdateID(ctx, bccKey)
	if err != nil {
		if c.isConfigNotListenErr(bccKey, err) {
			err = errors.Wrapf(configNotListenError, "namespace=%s full_key=%s", c.serviceName, bccKey)
		}
		return "", "", err
	}

	return value, strconv.FormatInt(updateID, 10), err
}

func (c *ClientV3) withRealtimeCallbackOption(ctx context.Context) bccsdk.ClientOption {
	if c.realtimeCallback != nil {
		if c.bccClientType == clientTypeKeys {
			cb := func(client bccsdk.ConfClient, bccKey string) error {
				item := c.getRealtimeCallbackItem(ctx, client, bccKey)
				return c.realtimeCallback(item)
			}
			return bccsdk.WithClientKeyCallback(cb)
		} else {
			cb := func(client bccsdk.ConfClient, bccKey string, action bccsdk.PathAction) error {
				item := c.getRealtimeCallbackItem(ctx, client, bccKey)
				return c.realtimeCallback(item)
			}
			return bccsdk.WithClientPathCallback(cb)
		}
	}
	return nil
}

func (c *ClientV3) getRealtimeCallbackItem(ctx context.Context, client bccsdk.ConfClient, bccKey string) RealtimeCallbackItem {
	item := newInternalItem(ctx, client, bccKey)
	item.namespace = c.serviceName
	item.clientType = c.bccClientType

	return &item
}

func (c *ClientV3) genBccKey(directory string, confName string) string {
	return directory + dirSeparator + encodeConfName(confName)
}
