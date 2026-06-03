package eventmgr

import (
	"context"
	"time"

	"code.byted.org/bcc/bcc-go-client/coreclient/model"
	"code.byted.org/bcc/bcc-go-client/internal/core/common"
	"code.byted.org/bcc/bcc-go-client/internal/util"
	"code.byted.org/bcc/bcc-go-client/logger"
	"code.byted.org/bcc/tools"
	"code.byted.org/bcc/tools/utime"
)

type callbackMgr struct {
	callbackRetryList map[string]*callbackNode //callback重试列表
}
type callbackNode struct {
	cbKey         string
	nextRetryTime time.Time
	item          *KeyInfo
}
type callbackResult struct {
	needReport bool
	needReply  bool
}

func newCallbackMgr() *callbackMgr {
	return &callbackMgr{
		callbackRetryList: make(map[string]*callbackNode),
	}
}
func (c *callbackMgr) addCallbackRetry(item *KeyInfo, dur time.Duration) {
	//没有重试限制，业务方保证
	nextRetryTime := time.Now().Add(dur)
	cbKey := item.getCallbackKey()

	c.callbackRetryList[cbKey] = &callbackNode{
		cbKey:         cbKey,
		nextRetryTime: nextRetryTime,
		item:          item,
	}
	logger.Debug("retry key=%v failCount=%v next=%v", item.Name, item.downRecord.target.FailCount, utime.UtcToTimestr(int(nextRetryTime.Unix())))
}
func (c *callbackMgr) delCallbackRetry(item *KeyInfo) {
	if len(c.callbackRetryList) > 0 {
		delete(c.callbackRetryList, item.getCallbackKey())
	}
}

// 目录下配置被删除时回调
func (c *callbackMgr) doPathDeleteCallback(item *KeyInfo) {
	var e error
	defer tools.Recover(context.TODO(), "bcc.callback", &e)
	callback := item.Dir.CallBack
	timeNow := time.Now()
	e = callback(item, model.PathDelete)
	util.EmitCallbackLatency(item.Key(), timeNow)
	if e != nil { //回调失败只是打点，不做其他处理
		logger.Error("updateOne callback fail key=%v err=%v", item.Key(), e)
		util.EmitError(item.Key(), "callback.del")
	}
}
func (c *callbackMgr) doPathAuthFailCallback(pathInfo *PathInfo, svrItem *common.ServerItem) {
	var e error
	defer tools.Recover(context.TODO(), "bcc.callback", &e)
	timeNow := time.Now()
	e = pathInfo.CallBack(NewKeyInfoFromPath(svrItem.Key(), pathInfo, pathInfo.Opt), model.PathAtuhFail)
	util.EmitCallbackLatency(svrItem.Key(), timeNow)
	if e != nil { //回调失败只是打点，不做其他处理}
		logger.Error("updateOne callback fail key=%v status:=[%v] err=%v", svrItem.Key(), model.PathAtuhFail, e)
		util.EmitError(svrItem.Key(), "callback.del")
	}
}

// 目录或key变动时回调
func (c *callbackMgr) doCallback(item *KeyInfo) callbackResult {
	var callbackDur time.Duration
	needReport := true
	needReply := true
	cbFunc := func() (e error) {
		defer tools.Recover(context.TODO(), "bcc.callback", &e)
		if item.isItem() {
			return item.callback(item)
		} else {
			return item.Dir.CallBack(item, model.PathUpdate)
		}
	}
	timeNow := time.Now()
	err := cbFunc()
	util.EmitCallbackLatency(item.Key(), timeNow)
	if err != nil {
		var result common.DownloadItemResult
		if e, ok := err.(*model.CallbackRetryError); ok && e != nil { //回调失败，重试
			callbackDur = e.Dur
			if e.Err == nil { //业务方的callback异步执行，不希望被server认为是错误
				needReport = false
			}
			if e.Block { //业务方希望WatchXXX()阻塞等待
				needReply = false
			}
			result = common.DOWNLOAD_RESULT_CALLBACK_RETRY
		} else { //回调失败，不重试
			result = common.DOWNLOAD_RESULT_CALLBACK
		}
		item.callbackError(result, err)
	} else {
		item.successCallback()
	}
	//callback失败
	if item.NeedCallbackRetry() {
		c.addCallbackRetry(item, callbackDur)
	}
	if !item.isCallbackSuccess() {
		item.reportCallback(needReport, callbackDur)
	}

	return callbackResult{
		needReport: needReport,
		needReply:  needReply,
	}
}
