package confclient

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	internalerror "code.byted.org/bcc/bcc-go-client/internal/error"
	"code.byted.org/bcc/bcc-go-client/logger"
	"code.byted.org/bcc/conf_engine/confcontent"
	"code.byted.org/bcc/tools"
	"code.byted.org/gopkg/logs"

	"github.com/pkg/errors"
)

type PathAction int

const (
	KeyAddOrUpdate PathAction = 0 // new or update config within path
	KeyDelete      PathAction = 1 // soft/hard delete config
)

type (
	KeyCallback func(client ConfClient, bcKey string) error
	// 目录监听情况下，鉴权失败不会回调，Get对应key会返回authFail error，但是会保持监听，当鉴权状态变成通过后，会进行回调
	PathCallback func(client ConfClient, bcKey string, action PathAction) error
)

type (
	Unmarshaler func([]byte, interface{}) error
	Getter      func(ctx context.Context, opts ...ConfOption) (interface{}, error)
	BizCaster   func(interface{}) interface{}
)

// ConfClient 配置相关的client
type ConfClient interface {
	// GetBytes Outside API：return raw data
	// param@bcKey: path+confName
	// param@opts: conf level options
	GetBytes(ctx context.Context, bcKey string, opts ...ConfOption) ([]byte, error)
	Get(ctx context.Context, bcKey string, opts ...ConfOption) (string, error)
	GetWithUpdateID(ctx context.Context, bcKey string, opts ...ConfOption) (string, int64, error)
	GetAllConf() (map[string][]byte, error)
	/** 相关元信息接口 **/

	// GetPublishTime  Outside API：return configure publishTime
	// param@bcKey:  path+confName
	GetPublishTime(bcKey string) (int64, error)

	// GetVersion Outside API：return configure version
	// param@bcKey: path+confName
	GetVersion(bcKey string) (int64, error)
	// GetSchemaName Outside API：return configure schema name
	// param@bcKey:  , path+confName
	GetSchemaName(bcKey string) (string, error)
	// GetBcKeys Return all bcKeys in current client
	GetBCKeys() []string
	// Close stop watch. once client closed. Invoke other method will be undefined behavior
	Close()
	// CancelWatchKeys bcKey=pathath+confName, Cancel watch the specified keys, the update of this keys will not be callback
	CancelWatchKeys(bcKeys []string)
	// CancelWatchPath Cancel watch the specified path recursively, the update of this path and its sub path will not be callback
	CancelWatchPath(path string)
	// GetModel cache by default. only cache the type firs time input. can use WithDeepCopy to get the model not cached
	GetModel(ctx context.Context, bcKey string, opts ...ConfOption) (interface{}, error)
	NewGetter(bcKey string, unmarshaler Unmarshaler, ins interface{}) Getter
	NewCastGetter(bcKey string, unmarshaler Unmarshaler, ins interface{}, caster BizCaster) Getter
	IsFlowGrayStatus(bcKey string) (bool, error) //in flow gray status
	IsDynamicConfig(bcKey string) (bool, error)
}

func NewClient(ctx context.Context, namespace string, bcKey string, opts ...ClientOption) (ConfClient, error) {
	return NewClientByKeys(ctx, namespace, []string{bcKey}, opts...)
}
func NewClientByKeys(ctx context.Context, namespace string, bcKeys []string, opts ...ClientOption) (ConfClient, error) {
	logger.CtxInfo(ctx, "NewClientByKeys namespace[%v], bcKeys[%+v]", namespace, bcKeys)
	if namespace == "" || len(bcKeys) == 0 {
		return nil, errors.New("Namespace/ bcKeys cannot be empty string")
	}

	// check key validity
	for _, key := range bcKeys {
		if err := checkKeyValidity(key); err != nil {
			return nil, err
		}
	}

	c, err := newClient(ctx, namespace, opts)
	if err != nil {
		return nil, err
	}

	err = c.watchKeys(bcKeys)
	if err != nil {
		if nErr, ok := err.(internalerror.Error); ok {
			if nErr.IsAuthFail() {
				return nil, errors.Wrap(internalerror.ErrAuthFailed, err.Error())
			}
		}
		return nil, err
	}

	loopMetricsAddClient(c)

	return c, nil
}
func NewClientByPath(ctx context.Context, namespace string, path string, opts ...ClientOption) (ConfClient, error) {
	logger.CtxInfo(ctx, "NewClientByPath namespace[%v] path[%v]", namespace, path)
	if path[0] != '/' {
		return nil, errors.New("path must start with /")
	}
	path = strings.TrimSuffix(path, "/")
	c, err := newClient(ctx, namespace, opts)
	if err != nil {
		return nil, err
	}

	err = c.watchPath(path)
	if err != nil {
		if nErr, ok := err.(internalerror.Error); ok {
			if nErr.IsNotExist() {
				return nil, errors.Wrap(nErr, fmt.Sprintf("namespace=%v path=%v", namespace, path))
			}
		}
		return nil, err
	}

	loopMetricsAddClient(c)

	return c, nil

}

func (c *client) Get(ctx context.Context, bcKey string, opts ...ConfOption) (ret string, err error) {
	defer func() {
		c.recordGet(bcKey, err)
	}()
	ct, err := c.getConfContent(bcKey)
	if err != nil {
		return "", err
	}

	confOpt := newConfOption(opts...)

	ret, err = ct.getString(ctx, confOpt)

	if err != nil {
		return "", errors.Wrap(err, fmt.Sprintf("bcKey[%v] get bytes fail", bcKey))
	}

	if err != nil {
		return "", errors.Wrap(err, "get fail")
	}
	return ret, nil
}

func (c *client) GetWithUpdateID(ctx context.Context, bcKey string, opts ...ConfOption) (ret string, updateID int64, err error) {
	defer func() {
		c.recordGet(bcKey, err)
	}()
	ct, err := c.getConfContent(bcKey)
	if err != nil {
		return "", 0, err
	}

	confOpt := newConfOption(opts...)

	ret, err = ct.getString(ctx, confOpt)

	if err != nil {
		return "", 0, errors.Wrap(err, fmt.Sprintf("bcKey[%v] get bytes fail", bcKey))
	}

	if err != nil {
		return "", 0, errors.Wrap(err, "get fail")
	}
	return ret, ct.updateID, nil
}
func (c *client) GetBytes(ctx context.Context, bcKey string, opts ...ConfOption) (ret []byte, err error) {
	defer func() {
		c.recordGet(bcKey, err)
	}()
	ct, err := c.getConfContent(bcKey)
	if err != nil {
		return nil, err
	}

	confOpt := newConfOption(opts...)

	ret, err = ct.getBytes(ctx, confOpt)

	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("bcKey[%v] get bytes fail", bcKey))
	}

	if confOpt.deepcopy {
		return tools.DeepCopy(ret).([]byte), nil
	}

	return ret, nil
}

func (c *client) IsFlowGrayStatus(bcKey string) (bool, error) {
	if ct, err := c.getConfContent(bcKey); err != nil {
		return false, err
	} else {
		return ct.GrayRule.IsGray(), nil
	}
}

func (c *client) IsDynamicConfig(bcKey string) (bool, error) {
	if ct, err := c.getConfContent(bcKey); err != nil {
		return false, err
	} else {
		return ct.ConfType == confcontent.ConfTypeDynamicJson, nil
	}
}

func (c *client) GetModel(ctx context.Context, bcKey string, opts ...ConfOption) (ret interface{}, err error) {
	defer func() {
		c.recordGet(bcKey, err)
	}()
	ct, err := c.getConfContent(bcKey)
	if err != nil {
		return nil, err
	}

	confOpt := newConfOption(opts...)
	confModel := confOpt.model
	if confModel == nil && ct.SchemaName != "" {
		confModel = c.option.modelMap[ct.SchemaName]
	}

	if confModel == nil {
		return nil, errors.Wrap(errMissModel, fmt.Sprintf("bcKey[%v] not found model input can not use get Model method", bcKey))
	}

	ret, err = ct.getModel(ctx, genModelParser(c.namespace, bcKey, confModel), confOpt)
	if err != nil {
		return nil, internalerror.NewError(err, "namespace=%v full_key=%v confModel=%#v", c.namespace, bcKey, confModel)
	}

	if confOpt.deepcopy {
		return tools.DeepCopy(ret), nil
	}

	return ret, nil
}

func (c *client) GetAllConf() (map[string][]byte, error) {
	var err error
	snapShot := make(map[string][]byte, 0)
	c.confs.Range(func(k, v interface{}) bool {
		key, ok := k.(string)
		if !ok {
			logs.CtxWarn(c.ctx, "key [%v] is not type of string", k)
			return true
		}

		confData, ok := v.(*content)
		if !ok {
			logs.CtxWarn(c.ctx, "value [%+v] is not type of *content", v)
			return true
		}

		// 是正常版本才进行配置获取，避免首次创建配置的流量灰度过程中获取到空值
		if confData.checkVersion(confData.Version) == nil {
			snapShot[key], err = confData.getBase()
			if err != nil {
				// 终止迭代遍历，返回错误
				return false
			}
		}
		return true
	})
	return snapShot, err
}

func (c *client) GetPublishTime(bcKey string) (int64, error) {
	if ct, err := c.getConfContent(bcKey); err != nil {
		return 0, err
	} else if ct.PublishTime == -1 {
		return 0, errors.Wrap(errConfigNotAvailable, fmt.Sprintf("bcKey[%v] init not published publish time not available", bcKey))
	} else {
		return ct.PublishTime, nil
	}
}

func (c *client) GetVersion(bcKey string) (int64, error) {
	if ct, err := c.getConfContent(bcKey); err != nil {
		return 0, err
	} else if ct.Version == confcontent.ConfVersionInit {
		return 0, errors.Wrap(errConfigNotAvailable, fmt.Sprintf("bcKey[%v] init not published version not available", bcKey))
	} else {
		return ct.Version, nil
	}
}

func (c *client) GetSchemaName(bcKey string) (string, error) {
	if ct, err := c.getConfContent(bcKey); err != nil {
		return "", err
	} else {
		return ct.SchemaName, nil
	}
}

func (c *client) GetBCKeys() []string {
	var bcKeys []string
	c.confs.Range(func(key, value interface{}) bool {
		strKey, ok := key.(string)
		if !ok {
			logs.CtxWarn(c.ctx, "key [%v] is not type of string", key)
			return true
		}

		bcKeys = append(bcKeys, strKey)
		return true
	})

	return bcKeys
}

func (c *client) Close() {
	if c.state == clientClosed {
		return
	}
	c.stateLock.Lock()
	defer c.stateLock.Unlock()
	if c.state == clientClosed {
		return
	}
	c.coreClient.CancelKeys(c.getWatchKeys())
	if path := c.getWatchPath(); path != "" {
		c.coreClient.CancelPath(path)
	}
	c.state = clientClosed
}

func (c *client) CancelWatchKeys(BCKeys []string) {
	var storayKeys []string
	for _, k := range BCKeys {
		storayKeys = append(storayKeys, c.getStorageKey(k))
	}
	c.coreClient.CancelKeys(storayKeys)
}

func (c *client) CancelWatchPath(path string) {
	c.coreClient.CancelPath(c.getStoragePath(path))
}

func (c *client) NewGetter(bcKey string, unmarshaler Unmarshaler, ins interface{}) Getter {
	return c.NewCastGetter(bcKey, unmarshaler, ins, nil)
}

func (c *client) NewCastGetter(bcKey string, unmarshaler Unmarshaler, ins interface{}, caster BizCaster) Getter {
	if caster == nil {
		caster = func(v interface{}) interface{} {
			return v
		}
	}
	typ := reflect.TypeOf(ins)
	// get the zero value in by the given type
	// {*type, nil} in pointer type
	// {type, zeros} in concrete type
	zeroVal := reflect.Zero(typ).Interface()
	isPtr := typ.Kind() == reflect.Ptr
	if isPtr {
		typ = typ.Elem()
	}

	resultCaster := func(val interface{}, err error) (interface{}, error) {
		// not unmarshal-able kinds, we panic
		switch typ.Kind() {
		case reflect.Interface, reflect.Chan, reflect.Func:
			logger.Fatal("kind: " + typ.Kind().String() + " is not unmarshal of type: " + typ.Name())
			return caster(zeroVal), errors.New("kind: " + typ.Kind().String() + " is not unmarshal of type: " + typ.Name())
		default:
			// explicit do nothing
		}
		if err != nil || val == nil {
			return caster(zeroVal), err
		}
		return val, err
	}
	valParser := func(value []byte) (interface{}, error) {
		val := reflect.New(typ)
		err := unmarshaler(value, val.Interface())
		if err != nil {
			return nil, err
		}
		if !isPtr {
			// if given is not a pointer type, we need return a non-pointer interface
			// to meet the expectation from user.
			val = val.Elem()
		}
		return caster(val.Interface()), nil
	}
	type Proxy struct {
		sync.RWMutex
		ct *content
	}

	cacheProxy := &Proxy{}

	getterFunc := func(ctx context.Context, opts ...ConfOption) (interface{}, error) {
		var err error
		var ct *content
		{
			// CRITICAL REGION
			// make a short copy to simplify the code

			cacheProxy.RLock()
			ct = cacheProxy.ct
			cacheProxy.RUnlock()
		}

		confOpt := newConfOption(opts...)
		latestContent, err := c.getConfContent(bcKey)
		if err != nil {
			// useful cache, but we get latest value failed
			if ct != nil {
				ret, err := ct.getModel(ctx, valParser, confOpt)
				return resultCaster(ret, err)
			}
			// makes zero value useful
			return resultCaster(nil, err)
		}
		var val interface{}

		if ct != nil && latestContent.updateID == ct.updateID {
			return resultCaster(ct.getModel(ctx, valParser, confOpt))
		}

		{
			cacheProxy.Lock()
			cacheProxy.ct = latestContent.clone()
			ct = cacheProxy.ct
			cacheProxy.Unlock()
		}

		val, err = ct.getModel(ctx, valParser, confOpt)
		return resultCaster(val, err)
	}
	return getterFunc
}

func genModelParser(ns, bcKey string, model interface{}) parser {
	return func(contentBytes []byte) (interface{}, error) {

		var newModel interface{}
		if reflect.TypeOf(model).Kind() == reflect.Ptr {
			newModel = reflect.New(reflect.TypeOf(model).Elem()).Interface()
		} else {
			newModel = reflect.New(reflect.TypeOf(model)).Interface()
		}
		err := json.Unmarshal(contentBytes, &newModel)
		if err != nil {
			emitModelUnmarshalError(ns, bcKey)
			logger.Error("ns[%v] bcKey[%v] model unmarshal fail, err:%v", ns, bcKey, err)
		}
		return newModel, err
	}
}

func checkKeyValidity(bcKey string) error {
	if bcKey == "" {
		return errors.New("key cannot be empty string")
	}
	if bcKey[0] != '/' || bcKey[len(bcKey)-1] == '/' {
		return fmt.Errorf("key:[%v] must start with / and cannot end with /", bcKey)
	}
	return nil
}
