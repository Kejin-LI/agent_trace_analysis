package confclient

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"code.byted.org/bcc/bcc-go-client/coreclient"
	"code.byted.org/bcc/bcc-go-client/coreclient/debug"
	"code.byted.org/bcc/bcc-go-client/coreclient/model"
	internalerror "code.byted.org/bcc/bcc-go-client/internal/error"
	"code.byted.org/bcc/bcc-go-client/logger"
	"code.byted.org/bcc/conf_engine/confcontent/grayrule"
	"code.byted.org/bcc/tools"
	"code.byted.org/gopkg/logs"
)

var _ ConfClient = (*client)(nil)

// 将用于内部工具，如 tcc_config_checker 所需的调试信息和对外暴露的 ConfClient 隔
// 离开，但仍然保留内部工具获取内部细节的能力
var _ debug.DebuggerContainer = (*client)(nil)

type client struct {
	watchInfo
	stateInfo
	*emitCache
	namespace  string
	option     clientOption
	coreClient model.Client
	confs      sync.Map // confs map, byteconf key -> confContent
	ctx        context.Context
}

var (
	getCoreOnce          sync.Once
	defaultCoreClient    model.Client
	defaultCoreClientErr error
)

const (
	defaultClientName = "bcc"
)

type parser func(value []byte) (interface{}, error)

// defaultCoreClient不能加option的。因为会被多次WatchKey/WatchPath，它们对于底层coreClient的option设置并不一定一致。
// 但由于共享一个defaultCoreClient，所以一次设置就会影响后面的WatchKey。
// 所以最保险的方法就是不允许对defaultCoreClient做option设置，以此保证行为一致
func getDefaultCoreClient() (model.Client, error) {
	getCoreOnce.Do(func() {
		defaultCoreClient, defaultCoreClientErr = coreclient.NewClient(defaultClientName)
	})
	return defaultCoreClient, defaultCoreClientErr
}

func newClient(ctx context.Context, namespace string, opts []ClientOption) (c *client, err error) {
	clientOpt := newClientOption(opts...)

	if err = clientOpt.isValid(); err != nil {
		logger.CtxFatal(ctx, "%v", err)
		return nil, err
	}

	c = &client{
		namespace: namespace,
		option:    clientOpt,
		ctx:       ctx,
		emitCache: &emitCache{
			namespace:  namespace,
			identifier: clientOpt.identifier,
		},
	}

	if clientOpt.newCoreClient {
		c.coreClient, err = coreclient.NewClient("") // core will create uniq name
		emitUseNewCoreClient(namespace)
	} else if clientOpt.newCoreClientName != "" {
		coreClientOpt := transferConfClientOptionToCoreClientOption(clientOpt)
		c.coreClient, err = coreclient.NewClient(clientOpt.newCoreClientName, coreClientOpt...)
		emitUseNewCoreClient(namespace)
	} else {
		c.coreClient, err = getDefaultCoreClient()
	}
	if err != nil {
		c = nil
	}

	return
}

func (c *client) Name() string {
	return c.coreClient.Name()
}

func (c *client) getCoreClientWatchOptions() []model.WatchOption {
	var watchOptions []model.WatchOption
	clientOpt := c.option
	if clientOpt.initTimeout != 0 {
		watchOptions = append(watchOptions, model.WithWatchTimeout(clientOpt.initTimeout))
	}
	if clientOpt.disableListen {
		watchOptions = append(watchOptions, model.WithWatchDisableListen())
	}
	if clientOpt.enableEmpty {
		watchOptions = append(watchOptions, model.WithWatchEmpty())
	}
	return watchOptions
}

func (c *client) watchKeys(keys []string) error {
	for _, key := range keys {
		emitConfWatchNum(c.namespace, key, "bcKey")
	}
	options := c.getCoreClientWatchOptions()
	cb := func(item model.Item) error {
		defer item.Clear()

		// TODO: need timeout control？
		bcKey := c.parseBCKey(item.Key())
		err := c.parse(bcKey, item)

		if err != nil {
			logger.Error("Parse conf fail: err[%v]", err)
			return err
		}

		if c.option.keyCallback != nil {

			err = c.option.keyCallback(c, bcKey)
			if err != nil {
				emitCallbackError(c.namespace, bcKey, keyCallbackError+err.Error())
				logger.Error("Exec key callback fail: err[%v]", err)
				return err
			}
		}
		return nil
	}

	var storageKeys []string
	for _, key := range keys {
		storageKeys = append(storageKeys, c.getStorageKey(key))
	}
	c.addWatchKeys(storageKeys)
	if err := c.coreClient.WatchKeys(storageKeys, cb, options...); err != nil {
		logger.Error("core client watch keys fail: namespace:%v keys[%v] err[%v]", c.namespace, keys, err)
		emitWatchError(c.namespace, "key", errorType(err), 1)
		//TODO：现在默认取消监听。需要多观察有没有错误
		//其他的错误，应该取消监听。 因为业务方碰过Watch返回error，大概率会重试。如果不取消监听，那么就会碰到重复监听的错误了。这就掩盖了真正的错误。
		//如果有一个key重复监听了。其他的key其实也不会加入了监听列表中。此时不需要cancel其他的key
		if !IsReuseError(err) {
			c.coreClient.CancelKeys(storageKeys)
		}
		return err
	}

	logger.Info("watch keys success: namespace:%v keys[%v]", c.namespace, keys)
	return nil
}
func (c *client) watchPath(path string) error {
	emitConfWatchNum(c.namespace, path, "bcPath")

	cb := func(item model.Item, status model.PathCallbackStatus) error {
		defer item.Clear()
		logger.Debug("watch path callback: namespace:%v path:%v key:%v status:%v", c.namespace, path, item.Key(), status)
		bcKey := c.parseBCKey(item.Key())
		if status == model.PathAtuhFail {
			c.confs.Store(bcKey, newAuthFailContent())
			return nil
		}

		if status == model.PathUpdate {
			err := c.parse(bcKey, item)
			if err != nil {
				logger.Error("Parse conf fail: err[%v]", err)
				return err
			}
		}
		if status == model.PathDelete {
			// Fetch the last content in callback
			defer c.confs.Delete(bcKey)
		}

		logger.Debug("Path action: key[%s] status[%v]", bcKey, status)
		if c.option.pathCallback != nil {
			var err error

			if status == model.PathUpdate {
				err = c.option.pathCallback(c, bcKey, KeyAddOrUpdate)
			} else {
				// TODO: need timeout control？
				// Not check err for path delete callback
				//nolint errcheck
				c.option.pathCallback(c, bcKey, KeyDelete)
			}
			if err != nil {
				emitCallbackError(c.namespace, bcKey, pathCallbackError+err.Error())
				logger.CtxError(c.ctx, "Exec path callback fail: err[%v]", err)
				return err
			}
		}
		return nil
	}

	storagePath := c.getStoragePath(path)
	c.addWatchPath(storagePath)
	if err := c.coreClient.WatchPath(storagePath, cb, c.getCoreClientWatchOptions()...); err != nil {
		logger.CtxError(c.ctx, "watch path fail: namespace:[%v] path[%s] err[%v]", c.namespace, path, err)
		emitWatchError(c.namespace, "path", errorType(err), 1)
		// Reuse 错误不应该取消之前的监听
		//其他的错误，应该取消监听。 因为业务方碰过Watch返回error，大概率会重试。如果不取消监听，那么就会碰到重复监听的错误了。这就掩盖了真正的错误。
		if !IsReuseError(err) {
			//由于一次只能监听一个目录，所以不用像watchKey那样的判断
			logger.CtxWarn(c.ctx, "cancel path[%v]", storagePath)
			c.coreClient.CancelPath(storagePath)
		}
		return err
	}

	logger.Info("watch path success: namespace:[%v] path[%v]", c.namespace, path)
	return nil
}

func (c *client) parse(bcKey string, item model.Item) (err error) {
	value := item.Value()
	rule := item.SDKGrayRule()

	defer func() {
		if err != nil {
			emitConfParseError(c.namespace, bcKey, err.Error())
		}
	}()
	ct, err := newContent(item)
	if err != nil {
		logger.CtxError(c.ctx, "parse serialization[%v] conf fail. : bcKey[%v] err[%v] value:[%v]", item.Serialization(), bcKey, err, string(value))
		return internalerror.NewError(errConfigInternalError, "parse conf fail: bcKey[%v] err[%v] value:[%v]", bcKey, err, string(value))
	}
	// flow gray rule compile
	if rule != "" {
		gr := &grayrule.GrayRule{}
		err = tools.JsonUnmarshal(rule, gr)
		if err != nil {
			logger.CtxError(c.ctx, "parse grayRule fail: bcKey[%v] err[%v] value:[%v]", bcKey, err, rule)
			return internalerror.NewError(errConfigInternalError, "parse grayRule fail: bcKey[%v] err[%v] value:[%v]", bcKey, err, rule)
		}
		err = gr.Compile()
		if err != nil {
			logger.CtxError(c.ctx, "compile grayRule fail: bcKey[%v] err[%v] value:[%v]", bcKey, err, rule)
			return internalerror.NewError(errConfigInternalError, "compile grayRule fail: bcKey[%v] err[%v] value:[%v]", bcKey, err, rule)
		}
		ct.GrayRule = gr
	}
	err = ct.processDynamicJson()
	if err != nil {
		logger.CtxError(c.ctx, "process dynamic json fail: bcKey[%v] err[%v] value:[%v]", bcKey, err, string(value))
		return internalerror.NewError(errConfigInternalError, "process dynamic json fail: bcKey[%v] err[%v] value:[%v]", bcKey, err, string(value))
	}

	emitConfUpdate(c.namespace, bcKey, ct.Version)
	c.confs.Store(bcKey, ct)
	return nil
}

func (c *client) getConfContent(bcKey string) (*content, error) {
	//todo emitSlaRequestThroughput(c.namespace)
	if item, ok := c.confs.Load(bcKey); ok {
		res := item.(*content)
		if res.isAuthFail {
			return nil, internalerror.NewError(internalerror.ErrAuthFailed, "namespace=%v full_key=%v", c.namespace, bcKey)
		}
		return res, nil
	}
	return nil, internalerror.NewError(internalerror.ErrNotExist, "namespace=%v full_key=%v", c.namespace, bcKey)
}
func (c *client) getAllKeyVersion() map[string]int64 {
	keyVersion := make(map[string]int64, 0)

	c.confs.Range(func(key, value interface{}) bool {
		k, ok := key.(string)
		if !ok {
			logs.CtxWarn(c.ctx, "key [%v] is not type of string", key)
			return true
		}

		v, ok := value.(*content)
		if !ok {
			logs.CtxWarn(c.ctx, "value [%+v] is not type of content", value)
			return true
		}

		keyVersion[k] = v.Version
		return true
	})

	return keyVersion
}

func (c *client) parseBCKey(key string) string {
	return strings.TrimPrefix(key, c.namespace)
}
func (c *client) getStorageKey(bcKey string) string {
	bcKey = strings.Trim(bcKey, "/")
	return fmt.Sprintf("%s/%s", c.namespace, bcKey)
}

func (c *client) getStoragePath(path string) string {
	path = strings.Trim(path, "/")
	if path == "" {
		return fmt.Sprintf("%s/", c.namespace)
	}
	return fmt.Sprintf("%s/%s/", c.namespace, path)
}

// Debugger implements debug.DebuggerContainer.
func (c *client) Debugger() debug.Debugger {
	debugger, ok := c.coreClient.(debug.Debugger)
	if !ok {
		logger.Error("core client not implement debug.Debugger")
		return nil
	}
	return debugger
}

func celPath(pathIndex []int) string {
	ret := "celpath"
	for _, each := range pathIndex {
		ret = fmt.Sprintf("%v_%v", ret, each)
	}
	return ret
}

func hitGrayRule(ctx context.Context, grayRule *grayrule.GrayRule, abKeyMap map[string]interface{}) bool {
	if !grayRule.IsGray() || !grayRule.IsCompiled() {
		return false
	}
	// 这里不需要填充默认的流量参数, 防止默认填充的流量参数命中白名单, 对齐ByteConf
	// 比如白名单为did in [0, 1, 2, 3] or uid in [4, 5, 6, 7], scope: map['uid'] = 8
	// 此时如果scope 填充默认did = 0参数, 则会命中灰度
	hit, err := grayRule.Hit(abKeyMap)
	if err != nil {
		logger.CtxWarn(ctx, "grayRule.Hit fail: abKey[%+v] err[%v]", abKeyMap, err)
		return false
	}
	return hit
}
