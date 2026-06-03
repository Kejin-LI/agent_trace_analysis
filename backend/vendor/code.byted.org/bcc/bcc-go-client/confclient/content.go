package confclient

import (
	"context"
	"fmt"
	"sync"

	"code.byted.org/bcc/bcc-go-client/coreclient/model"
	internalerror "code.byted.org/bcc/bcc-go-client/internal/error"
	"code.byted.org/bcc/bcc-go-client/logger"
	"code.byted.org/bcc/conf_engine/confcontent"
	"code.byted.org/bcc/conf_engine/confcontent/grayrule"
	"code.byted.org/bcc/conf_engine/jsonconf/expr"
	cmodel "code.byted.org/bcc/conf_engine/model"
	exprservice "code.byted.org/bcc/conf_engine/service/expr"
	"code.byted.org/bcc/tools"
	"code.byted.org/bcc/tools/json"

	lru "github.com/hashicorp/golang-lru"
	"github.com/pkg/errors"
)

const (
	lruCacheSize = 30
)

type cacheItem struct {
	// json dynamic config
	fullExprList []*expr.Conf
	grayExprList []*expr.Conf
	fullCache    *lru.Cache // cel path -> {model, publish_time}
	grayCache    *lru.Cache // cel path -> {model, publish_time}

	// static config
	fullModel interface{} // full model
	grayModel interface{} // gray model

	mu sync.RWMutex
}

// fixme 目前使用方式会并发修改cache，但是cache有锁，但是还是未来场景如果并发写其他字段会有并发问题
type content struct {
	confcontent.Content
	cache       *cacheItem `json:"-"` // compile or deserialization result of conf
	baseStrOnce sync.Once
	baseStr     string
	fullBaseStr string
	updateID    int64
	isAuthFail  bool
}
type lruCacheItem struct {
	Model       interface{}
	PublishTime int64
}

func newContent(item model.Item) (c *content, err error) {
	c = &content{
		Content: confcontent.Content{
			PublishTime: -1,
			Version:     confcontent.ConfVersionInit, //-1表示紧急配置，为了远离-1，选用-9
		},
		cache:    &cacheItem{},
		updateID: item.UpdateID(),
	}
	if len(item.Value()) == 0 && len(item.Values()) > 0 {
		// 多文件协议的内容整合
		err = c.assemblContent(item.Values())
	} else {
		err = c.unmarshal(item)
	}
	return c, err
}

func newAuthFailContent() *content {
	return &content{
		isAuthFail: true,
	}
}

// AssemblContent 整合配置层信息
func (c *content) assemblContent(contents []*model.Content) error {
	cs := make(map[string][]byte)
	for _, one := range contents {
		cs[one.Desc] = one.Value
	}

	if b, exist := cs[confcontent.ConfDescMeta]; !exist {
		return fmt.Errorf("no meta content block")
	} else if err := tools.JsonUnmarshal(b, c); err != nil {
		return err
	}

	if b, exist := cs[confcontent.ConfDescFullBase]; !exist {
		return fmt.Errorf("no fullBase content block")
	} else {
		c.FullBase = b
	}

	// 流量灰度版本赋值
	if b, exist := cs[confcontent.ConfDescBase]; exist {
		c.Base = b
	}

	// 流量灰度规则赋值
	if rule, exist := cs[confcontent.ConfDescGrayRule]; exist {
		gr := &grayrule.GrayRule{}
		err := tools.JsonUnmarshal(rule, gr)
		if err != nil {
			return fmt.Errorf("parse grayRule fail: err[%v] value:[%v]", err, rule)

		}
		err = gr.Compile()
		if err != nil {
			return fmt.Errorf("compile grayRule fail: err[%v] value:[%v]", err, rule)
		}
		c.GrayRule = gr
	}

	return nil
}

func (c *content) unmarshal(item model.Item) (err error) {

	if item.Serialization().IsJsonSerialization() {
		err = tools.JsonUnmarshal(item.Value(), c)
	} else if item.Serialization().IsMsgpackSerialization() {
		err = tools.MsgPackUnmarshal(item.Value(), c)
	} else {
		err = fmt.Errorf("unsupport serialization[%v]", item.Serialization())
	}

	return
}
func (c *content) clone() *content {
	newCt := &content{
		Content:  c.Content,
		updateID: c.updateID,
		cache:    &cacheItem{},
	}
	err := newCt.processDynamicJson()
	if err != nil {
		logger.Error("clone content fail: err[%v] should not happen", err)
	}
	return newCt
}

// if config type is dynamic config, compile and cache
func (c *content) processDynamicJson() error {
	if c.ConfType != confcontent.ConfTypeDynamicJson {
		return nil
	}

	if c.checkVersion(c.Version) == nil { // 创建配置的首次流量灰度时，会出现version为-1的情况，不需要Compile线上版本内容
		cListOrigin, err := exprservice.Compile(c.FullBase)
		if err != nil {
			logger.Error("Compile base dynamic json fail: err[%v] , version:[%v]", err, c.Version)
			return internalerror.NewError(errConfigInternalError, "compile base dynamic json fail: err[%v] , version:[%v]", err, c.Version)
		}
		c.cache.fullExprList = cListOrigin
	}

	if c.GrayRule.IsGray() && c.checkVersion(c.GrayVersion) == nil { // 删除配置的灰度过程不需要Compile灰度内容
		cList, err := exprservice.Compile(c.Base)
		if err != nil {
			logger.Error("Compile fullbase dynamic json fail: err[%v] version:[%v]", err, c.Version)
			return internalerror.NewError(errConfigInternalError, "compile fullbase dynamic json fail: err[%v] version:[%v]", err, c.Version)
		}
		c.cache.grayExprList = cList
	}

	// TODO: use sync.Pool
	var ferr, gerr error
	c.cache.fullCache, ferr = lru.New(lruCacheSize)
	c.cache.grayCache, gerr = lru.New(lruCacheSize)
	if ferr != nil || gerr != nil {
		logger.Error("fail to new lru cache: ferr:%v, gerr:%v", ferr, gerr)
		return internalerror.NewError(errConfigInternalError, "fail to new lru cache: ferr:%v, gerr:%v", ferr, gerr)

	}
	return nil
}
func (c *content) getBytes(ctx context.Context, confOpt *confOption) (ret []byte, err error) {

	switch c.ConfType {
	case confcontent.ConfTypeRegular:
		ret, err = c.getBytesOfStaticConf(ctx, confOpt.scope)
	case confcontent.ConfTypeDynamicJson:
		ret, err = c.getBytesOfCelJson(ctx, confOpt.scope)
	default:
		return nil, errors.Wrap(errConfigUnknownType, fmt.Sprintf("get bytes fail unknown conf type[%v]", c.ConfType))
	}
	return
}
func (c *content) getString(ctx context.Context, confOpt *confOption) (ret string, err error) {
	switch c.ConfType {
	case confcontent.ConfTypeRegular:
		ret, err = c.getStringOfStaticConf(ctx, confOpt.scope)
	case confcontent.ConfTypeDynamicJson:
		ret, err = c.getStringOfCelJson(ctx, confOpt.scope)
	default:
		return "", errors.Wrap(errConfigUnknownType, fmt.Sprintf("get bytes fail unknown conf type[%v]", c.ConfType))
	}
	return
}
func (c *content) getBytesOfStaticConf(ctx context.Context, scope map[string]interface{}) ([]byte, error) {
	hitGray, err := c.hitGrayRuleAndCheckVersion(ctx, scope)
	if err != nil {
		return nil, err
	}
	if hitGray {
		return c.Base, nil
	}

	return c.FullBase, nil
}
func (c *content) getBytesOfCelJson(ctx context.Context, scope map[string]interface{}) ([]byte, error) {
	valStr, celPathIndex, hitGray, err := c.runDynamicJson(ctx, scope)
	if err != nil {
		logger.CtxError(ctx, "run bytecode of cel-json conf fail: err[%v]", err)
		return nil, internalerror.NewError(err, "run bytecode of cel-json conf fail: err[%v] hitGray:%v scope:%+v", err, hitGray, scope)
	}
	// no match conditions
	if len(celPathIndex) == 0 {
		logger.CtxError(ctx, "scope don't match any rule of the config, scope:%+v", scope)
		return nil, errors.Wrap(errDynamicConfigNotMatch, fmt.Sprintf("scope:[%+v] don't match config tree", scope))
	}
	//fixme gc风险？
	return tools.String2Bytes(valStr), nil
}
func (c *content) getStringOfStaticConf(ctx context.Context, scope map[string]interface{}) (string, error) {
	hitGray, err := c.hitGrayRuleAndCheckVersion(ctx, scope)
	if err != nil {
		return "", err
	}
	c.initBaseStr()
	if hitGray {
		return c.baseStr, nil
	}

	return c.fullBaseStr, nil
}
func (c *content) getStringOfCelJson(ctx context.Context, scope map[string]interface{}) (string, error) {
	valStr, celPathIndex, hitGray, err := c.runDynamicJson(ctx, scope)
	if err != nil {
		logger.CtxError(ctx, "run bytecode of cel-json conf fail: err[%v] hitGray:%v scope:%+v", err, hitGray, scope)
		return "", internalerror.NewError(err, "run bytecode of cel-json conf fail: err[%v] hitGray:%v scope:%+v", err, hitGray, scope)
	}
	// no match conditions
	if len(celPathIndex) == 0 {
		logger.CtxError(ctx, "scope don't match any rule of the config, scope:%+v", scope)
		return "", errors.Wrap(errDynamicConfigNotMatch, fmt.Sprintf("scope:[%+v] don't match config tree", scope))
	}
	return valStr, nil
}
func (c *content) getBase() ([]byte, error) {
	if c.ConfType == confcontent.ConfTypeRegular {
		return c.FullBase, nil
	}

	//对于json动态配置，返回兜底配置。兜底配置是数组的第一个元素
	if c.cache == nil || len(c.cache.fullExprList) == 0 {
		return []byte{}, internalerror.NewError(errConfigInternalError, "dynamic json config full list is empty")
	}

	var defVal string
	if err := json.JsonUnmarshal([]byte(c.cache.fullExprList[0].Conf.Txt), &defVal); err != nil {
		return []byte{}, internalerror.NewError(errConfigInternalError, "unmarshal dynamic config base config fail:%v", err)
	}

	return []byte(defVal), nil
}

func (c *content) initBaseStr() {
	c.baseStrOnce.Do(func() {
		c.baseStr = string(c.Base)
		c.fullBaseStr = string(c.FullBase)
	})
}

// 判断线上或者流量灰度版本是否是正常版本
func (c *content) checkVersion(version int64) error {
	if version >= 0 {
		return nil
	}

	if version == confcontent.ConfVersionEmpty {
		return internalerror.NewError(internalerror.ErrNotExist, "namespace=%v path=%v name=%v ", c.NsKey, c.Path, c.Name)
	}
	if version == confcontent.ConfVersionInit {
		return internalerror.NewError(errConfigNotAvailable, "namespace=%v path=%v name=%v init not published version not available", c.NsKey, c.Path, c.Name)
	}
	return nil
}

func (c *content) getModel(ctx context.Context, p parser, confOpt *confOption) (ret interface{}, err error) {

	switch c.ConfType {
	case confcontent.ConfTypeRegular:
		ret, err = c.getModelOfStaticConf(ctx, p, confOpt.scope)
	case confcontent.ConfTypeDynamicJson:
		ret, err = c.getModelOfDynamicConf(ctx, p, confOpt.scope)
	default:
		return nil, errConfigUnknownType
	}
	return
}
func (c *content) getModelOfDynamicConf(ctx context.Context, p parser, scope map[string]interface{}) (interface{}, error) {
	valStr, celPathIndex, hitGray, err := c.runDynamicJson(ctx, scope)
	if err != nil {
		logger.CtxError(ctx, "run bytecode of cel-json conf fail: err[%v] scope:%v hitGray:%v", err, scope, hitGray)
		return nil, internalerror.NewError(errConfigInternalError, "run bytecode of cel-json conf fail: err[%v] scope:%v hitGray:%v", err, scope, hitGray)
	}
	// no match conditions
	if len(celPathIndex) == 0 {
		logger.CtxError(ctx, "scope don't match any rule of the config, scope:%+v", scope)
		return nil, errors.Wrap(errDynamicConfigNotMatch, "get model of dynamic conf fail. not found any rule match scope")
	}

	var item interface{}
	var realItem *lruCacheItem
	pathID := celPath(celPathIndex)
	var exist bool
	if hitGray {
		if item, exist = c.cache.grayCache.Get(pathID); !exist {
			item = nil
		}
	} else {
		if item, exist = c.cache.fullCache.Get(pathID); !exist {
			item = nil
		}
	}
	if item != nil {
		lruItem, ok := item.(*lruCacheItem)
		if !ok {
			return nil, errors.Wrap(errConfigInternalError, "cast type to lru cache item fail. should not happen!")
		}
		realItem = lruItem
	}

	// not exist or expire
	if item == nil || realItem == nil || realItem.PublishTime < c.PublishTime {

		ret, err := p(tools.String2Bytes(valStr))
		if err != nil {
			logger.Error("Parse model fail: err[%v]", err)
			return nil, internalerror.NewError(errModelParse, "parse model fail:%v", err)
		}
		if hitGray {
			c.cache.grayCache.Add(pathID, &lruCacheItem{
				Model:       ret,
				PublishTime: c.PublishTime,
			})
		} else {
			c.cache.fullCache.Add(pathID, &lruCacheItem{
				Model:       ret,
				PublishTime: c.PublishTime,
			})
		}

		return ret, nil
	} else {
		logger.CtxDebug(ctx, "Hit cel model cache")
	}

	return realItem.Model, nil
}
func (c *content) getModelOfStaticConf(ctx context.Context, parser parser, scope map[string]interface{}) (interface{}, error) {

	hitGray, err := c.hitGrayRuleAndCheckVersion(ctx, scope)
	if err != nil {
		return nil, err
	}

	//TODO：除了考虑cache是否为空，还需要考虑model的类型是否有变动过。
	if hitGray {
		if c.cache.grayModel == nil {
			c.cache.mu.Lock()
			defer c.cache.mu.Unlock()

			//前面的hitGrayRule已经确认当前version对应的Base是合法的。
			if c.cache.grayModel, err = parser(c.Base); err != nil {
				logger.Error("parser base model fail: err[%v]", err)
				return nil, internalerror.NewError(errModelParse, "parse base model fail. value:%v", string(c.Base))
			}

		}
		return c.cache.grayModel, nil
	}
	if c.cache.fullModel == nil {
		c.cache.mu.Lock()
		defer c.cache.mu.Unlock()

		//在流量灰度发布期间，content.Base的内容就是灰度发布的配置。
		var err error
		//前面的hitGrayRule已经确认当前version对应的FullBase是合法的。
		if c.cache.fullModel, err = parser(c.FullBase); err != nil {
			logger.Error("parse fullbase model fail: err[%v]", err)
			return nil, internalerror.NewError(errModelParse, "parse fullbase model fail, value:%v", string(c.FullBase))
		}
	}

	return c.cache.fullModel, nil
}

func (c *content) runDynamicJson(ctx context.Context, scope map[string]interface{}) (conf string, path []int, hitGray bool, err error) {
	var celList []*expr.Conf
	hitGray, err = c.hitGrayRuleAndCheckVersion(ctx, scope)
	if err != nil {
		return
	}
	if hitGray {
		celList = c.cache.grayExprList
	} else {
		celList = c.cache.fullExprList
	}
	// fill default values
	curScope := cmodel.FillVar(scope, c.VarMap)

	ret, path, err := exprservice.RunItem(ctx, celList, curScope)
	return ret, path, hitGray, err
}

// 判断配置是否命中灰度、配置当前的线上或者灰度版本是否正常（首次创建配置、删除配置的流量灰度中，version会是-1，这时候应该返回not exist错误）
func (c *content) hitGrayRuleAndCheckVersion(ctx context.Context, scope map[string]interface{}) (bool, error) {
	hitGray := hitGrayRule(ctx, c.GrayRule, scope)
	version := c.Version
	if hitGray {
		version = c.GrayVersion
	}
	if err := c.checkVersion(version); err != nil {
		return hitGray, err
	}
	return hitGray, nil
}
