package eventmgr

import (
	"fmt"
	"time"

	cmodel "code.byted.org/bcc/bcc-go-client/coreclient/model"
	"code.byted.org/bcc/bcc-go-client/internal/core/common"
	internalerror "code.byted.org/bcc/bcc-go-client/internal/error"
	"code.byted.org/bcc/bcc-go-client/internal/util"
	"code.byted.org/bcc/bcc-go-client/logger"
	"code.byted.org/bcc/tools"
)

// 管理配置的状态、下载、回调、更新
type confHub struct {
	name        string
	keeper      *ConfKeeper
	waiterMgr   *watchWaiterMgr
	callbackMgr *callbackMgr
	fileCache   *util.FileCache
	dumper      common.Dumper
}

func newConfHub(name string, dumper common.Dumper) *confHub {
	return &confHub{
		name:        name,
		keeper:      newConfKeeper(),
		waiterMgr:   newWatchWaiterMgr(),
		callbackMgr: newCallbackMgr(),
		fileCache:   util.NewFileCache(),
		dumper:      dumper,
	}
}

func (c *confHub) AddWatchPath(task *common.WatchPathTask) common.StreamWatchPathTask {
	streamTask := common.StreamWatchPathTask{}
	waiter := newPathWaiterByWatchTask(task)
	item, err := c.keeper.AddWatchPath(task)
	if err != nil {
		waiter.replyError(err)
		return streamTask
	}

	c.waiterMgr.addPathWaiter(waiter)
	streamTask.CltDir = item.getStreamPathInfo()
	return streamTask
}

func (c *confHub) AddWatchKey(task *common.WatchKeysTask) *common.StreamWatchKeyTask {
	streamTask := common.NewStreamWatchKeyTask()
	waiter := newKeyWaiterByWatchTask(task)
	for _, key := range task.Keys() {
		item, err := c.keeper.AddWatchKey(key, task.Option(), task.Callback())
		if err != nil {
			waiter.replyKeyErrorAndDelete(key, err)
			continue
		}
		streamTask.Items[key] = item.genCltItem()
	}

	if waiter.checkFastTerminate() { //快速结束了，就不需要进行真正的监听了
		streamTask = common.NewStreamWatchKeyTask() //外部根据streamTask是否有key判断是否需要进行真正的监听。所以这里直接情况即可
	} else {
		c.waiterMgr.addKeyWaiter(waiter)
	}

	return streamTask
}

func (c *confHub) isAllowUpdateKey(svrItem *common.ServerItem) (*KeyInfo, bool) {
	item := c.keeper.GetKey(svrItem.Key())
	if item == nil {
		logger.Warn(" onUpdateKey miss, keyInfo:%v", svrItem.KeyInfo())
		return nil, false
	}
	if svrItem.IsAuthFail() {
		c.handleItemAuthFail(svrItem, "")
		return nil, false
	}
	if svrItem.IsItemErr() {
		c.handleItemErr(item, svrItem, "")
		return nil, false
	}

	flag := item.AllowUpdate(svrItem)
	return item, flag
}

func (c *confHub) isAllowUpdatePath(path string, svrItem *common.ServerItem) (*KeyInfo, bool) {
	dir := c.keeper.GetPathInfo(path)
	if dir == nil { //不应该出现，可能取消监听
		logger.Warn("onUpdatePath miss path=%v, keyInfo:%v", path, svrItem.KeyInfo())
		return nil, false
	}

	item := dir.GetItem(svrItem.Key())
	if item == nil {
		//fetch client不命中灰度也会重复下发，不需要处理
		if svrItem.IsDelete() {
			logger.Debug(" onUpdatePath miss key=%v", svrItem.Key())
			return nil, false
		}
		//首次下发，或者由于鉴权失败导致没有记录，拉渠道有可能会多次下发
		if svrItem.IsAuthFail() {
			c.handleItemAuthFail(svrItem, path)
			return nil, false
		}
		// 由于网络等问题，push_svr/pull_svr并没有从存储引擎读取到配置。一般情况下，只会在watch阶段，svr才会返回这个信息。后面配置更新时svr无法读取，直接忽略
		if svrItem.IsItemErr() {
			itemTmp := NewKeyInfoFromPath(svrItem.Key(), dir, dir.Opt)
			c.handleItemErr(itemTmp, svrItem, path)
			return nil, false
		}

		item = dir.ItemFirstUpdate(svrItem)
		if item == nil {
			logger.Error("onUpdatePath invalid path=%v, keyInfo=%v", path, svrItem.KeyInfo())
			return nil, false
		}
	}

	flag := item.AllowUpdate(svrItem)
	return item, flag
}

func (c *confHub) stopLastUpdateAction(item *KeyInfo) {
	c.callbackMgr.delCallbackRetry(item)

}

func (c *confHub) handleChangeEvent(item *KeyInfo, svrItem *common.ServerItem) (reportItem *KeyInfo, needReport bool) {
	// 配置不存在
	if svrItem.IsDelete() {
		c.handleItemDelete(item, svrItem)
		return item, true
	}
	// 配置鉴权失败
	if svrItem.IsAuthFail() {
		c.handleItemAuthFail(svrItem, item.Path())
		return item, true
	}

	// 正常更新，回调
	newItem := c.preProcessInlineData(item, svrItem)
	needReport = c.handleItemUpdate(newItem)
	if needReport {
		return newItem, true
	}
	return nil, false

}

func (c *confHub) handleItemAuthFail(svrItem *common.ServerItem, path string) {
	msg := fmt.Sprintf("key=%v version=%v updateId=%v", svrItem.Key(), svrItem.Version(), svrItem.UpdateID())
	if path == "" {
		logger.Warn("%v Msg: %v", internalerror.ErrAuthFailedMsg, msg)
		c.waiterMgr.replyKey(svrItem.Key(), common.GetResultError(common.DOWNLOAD_RESULT_AUTH_FAILED))
		return
	} else {
		logger.Warn("%v Msg: %v path=%v", internalerror.ErrAuthFailedMsg, msg, path)
		//考虑这种情况：一开始某个key存在并且是大文件，在大文件还没下发完, push_svr还在下发目录的其他key时，业务方将这个大文件key给删除了。
		//前面key还存在时，就已经加入了该keyName了。现在需要减去该keyName。

		//目录监听，鉴权错误不返回error，后续状态变化会下发更新
		c.callbackMgr.doPathAuthFailCallback(c.keeper.GetPathInfo(path), svrItem)
	}
}

// 一般是push_svr/pull_svr无法从存储引擎读取到配置造成的错误。svr只会在watch阶段下发该错误给SDK。此时，SDK直接吐回错误给调用方即可
func (c *confHub) handleItemErr(item *KeyInfo, svrItem *common.ServerItem, path string) {
	msg := fmt.Sprintf("key=%v version=%v updateId=%v failMsg:%v", svrItem.Key(), svrItem.Version(), svrItem.UpdateID(), svrItem.FailMsg())
	if path == "" {
		//c.keeper.CancelWatchKey(item.Name) //这里不能取消监听。参考https://bytedance.larkoffice.com/docx/JL23d783LoWug4xn7Sjcu1DWnNh
		logger.Error("%v Msg: %v", internalerror.ErrUnknown, msg)
		c.waiterMgr.replyKey(svrItem.Key(), common.GetResultError(common.DOWNLOAD_RESULT_ITEM_ERR, svrItem.FailMsg()))
	} else {
		//目录时，会在finish时取消监听
		logger.Error("%v Msg: %v path=%v", internalerror.ErrUnknown, msg, path)
		c.waiterMgr.replyPathKey(item, common.GetResultError(common.DOWNLOAD_RESULT_ITEM_ERR, svrItem.FailMsg()))
	}
}

func (c *confHub) handleItemDelete(item *KeyInfo, svrItem *common.ServerItem) {
	msg := fmt.Sprintf("key=%v version=%v updateId=%v", item.Key(), svrItem.Version(), svrItem.UpdateID())
	if item.isItem() {
		if item.Opt.EnableEmpty {
			logger.Info("watchKey wait %v", msg)
			c.waiterMgr.replyKey(item.Name, common.GetResultError(common.DOWNLOAD_RESULT_EMPTY))
		} else {
			c.keeper.CancelWatchKey(item.Name)
			logger.Error("watchKey delete %v", msg)
			c.waiterMgr.replyKey(item.Name, common.GetResultError(common.DOWNLOAD_RESULT_INVALID))
		}
	} else {
		logger.Info("watchPath delete %v", msg)
		item.deletePathItem()
		c.callbackMgr.doPathDeleteCallback(item)

		//考虑这种情况：一开始某个key存在并且是大文件，在大文件还没下发完, push_svr还在下发目录的其他key时，业务方将这个大文件key给删除了。
		//前面key还存在时，就已经加入了该keyName了。现在需要减去该keyName。
		item.Dir.ItemFinish(item)
		c.waiterMgr.replyPathKey(item, nil) //不认为是有错的
	}

}

func (c *confHub) preProcessInlineData(item *KeyInfo, svrItem *common.ServerItem) *KeyInfo {
	item.downRecord.inlineOk()
	if svrItem.IsMultiContent() {
		values, err := svrItem.GetDecompressValues()
		if err != nil {
			logger.Error("decompress key:[%v] value error:%v value:%v", item.Key(), err, svrItem.Value())
			//错误会体现在返回的KeyInfo中
			item.valueError(common.DOWNLOAD_RESULT_DECOMPRESS, err)
		} else {
			item.setValues(values)
		}
	}
	val, err := svrItem.GetDecompressValue()
	if err != nil {
		logger.Error("decompress key:[%v] value error:%v value:%v", item.Key(), err, svrItem.Value())
		//错误会体现在返回的KeyInfo中
		item.valueError(common.DOWNLOAD_RESULT_DECOMPRESS, err)
	} else {
		item.valueUpdate(val)
	}

	backupFile := item.WriteDisk(svrItem, c.fileCache)
	newItem := item.NewItem(svrItem, backupFile)

	return newItem
}

func (c *confHub) dumpItem(ts time.Time, item *KeyInfo, val []byte, vals []*cmodel.Content) {
	if c.dumper == nil {
		return
	}

	it := common.DumpItem{
		DownloadTime:   ts,
		IsSuccCallback: item.isCallbackSuccess(),
		Keyname:        item.Key(),
		UpdateID:       item.UpdateID(),
		Val:            val,
		Vals:           vals,
		BigFilepath:    item.BackupFile(),
		Serializer:     item.Serializer,
	}

	c.dumper.StorageItem(it)
}

// 使用新的keyInfo进行回调和更新缓存
func (c *confHub) handleItemUpdate(item *KeyInfo) bool {
	now := time.Now()
	val, vals := item.Value(), item.Values() //这里先取出来是因为回调函数中会将item的这两个值置空

	cbResult := callbackResult{needReply: true, needReport: true}
	c.emitConfUpdateLatencySLA(item)
	if !item.empty() {
		cbResult = c.callbackMgr.doCallback(item)
	}

	c.dumpItem(now, item, val, vals)

	err := item.getError()
	//callback success
	if item.isCallbackSuccess() {
		err = nil
		item.deleteOldBackupFile(c.fileCache)
		if item.isItem() {
			c.keyCallbackSuccess(item)
		} else {
			c.pathCallbackSuccess(item)
		}
		util.EmitWatch(item.Key(), int(item.Version()), item.source().String())
	} else {
		util.EmitErrorWatch(item.Key(), int(item.Version()), item.source().String(), item.result().String())
	}

	if item.isCallbackSuccess() || cbResult.needReply {
		if item.isItem() {
			c.waiterMgr.replyKey(item.Name, err)
			if item.Opt.DisableListen {
				c.CancelKeys([]string{item.Name}, insideCancel)
			}
		} else {
			c.waiterMgr.replyPathKey(item, err)
		}
	}

	return cbResult.needReport
	//todo 处理回调重试中，下发多个tos版本的情况
}

// todoCore 回调成功才将内容更新到cache中？
func (c *confHub) keyCallbackSuccess(item *KeyInfo) {
	logger.Info("core watchKey add key=%v version=%v updateId=%v size=%v source=%v",
		item.Name, item.Ver, item.UpdateID(), item.ValueSize, item.source())
	c.keeper.KeyUpdate(item)
}

func (c *confHub) pathCallbackSuccess(item *KeyInfo) {
	logger.Info("core watchPath add path=%v key=%v version=%v updateId=%v size=%v source=%v",
		item.Path(), item.Name, item.Ver, item.UpdateID(), item.ValueSize, item.source())

	c.keeper.PathUpdate(item)
	item.pathCallbackSuccess()
}

func (c *confHub) GetAllSendTask() (*common.StreamWatchKeyTask, []*common.StreamWatchPathTask) {
	keyTask := c.keeper.GetAllStreamWatchKeyTask()
	pathTaskList := c.keeper.GetAllStreamWatchPathTask()
	return keyTask, pathTaskList

}

func (c *confHub) CancelKeys(keys []string, source cancelType) []string {
	notifyKeys := make([]string, 0, 0)
	for _, key := range keys {
		exist := c.keeper.CancelKey(key)
		if !exist {
			logger.Info("cancelKey miss key=%v", key)
			continue
		}

		notifyKeys = append(notifyKeys, key)
		logger.Info("core cancelKey ok key=%v caller=%v", key, source)
	}

	return notifyKeys
}

func (c *confHub) CancelPath(path string, source cancelType) (cancelKeys []string) {
	pathInfo := c.keeper.GetPath(path)
	if pathInfo == nil {
		logger.Info("cancelPath miss path=%v", path)
		return
	}
	for _, item := range pathInfo.Items {
		cancelKeys = append(cancelKeys, item.Name)
	}

	c.keeper.CancelPath(path)
	logger.Info("core cancelPath ok path=%v caller=%v", path, source)

	return
}

func (c *confHub) FinishPath(task common.FinishPathMsg) {
	path := task.Path
	pathInfo := c.keeper.GetPath(path)
	if pathInfo == nil {
		logger.Warn("onFinishPath skip path=%v", path)
		return
	}
	pathInfo.pathFinish(task)
	if task.FailMsg != "" || pathInfo.Opt.DisableListen {
		//这里不能CancelPath。参考https://bytedance.larkoffice.com/docx/JL23d783LoWug4xn7Sjcu1DWnNh
		//c.CancelPath(path, insideCancel)
	}

	if task.Code != common.PathFinishSucc {
		logger.Warn("onFinishPath fail. code[%v], msg[%v]", task.Code, task.FailMsg)
		if task.Code == common.PathFinishDbErr {
			c.waiterMgr.replyPathError(path, internalerror.NewError(internalerror.ErrItemError, task.FailMsg))
		} else if task.Code == common.PathFinishInvalidPath {
			c.waiterMgr.replyPathError(path, internalerror.NewError(internalerror.ErrInvalidParam, task.FailMsg))
		} else {
			c.waiterMgr.replyPathError(path, internalerror.NewError(internalerror.ErrUnknown, task.FailMsg))
		}
	} else {
		c.waiterMgr.replyPathFinish(pathInfo)
	}

	util.EmitPathLatency(path, int(task.Total), pathInfo.WatchTime)
}

// 处理大文件下载完成
func (c *confHub) finishDownload(msg *common.FinishloaderMsg, keyInfo *KeyInfo) (reportInfo *KeyInfo, needReport bool) {
	result := msg.Result
	svrItem := result.SvrItem

	if result.Result != common.DOWNLOAD_RESULT_OK {
		// 上报下载失败的错误状态
		return keyInfo.NewItemWitResult(result), true
	}

	//被修改，结果直接丢弃 //要么改动太频繁，要么服务重启后刚好更新版本
	if keyInfo.isOlderSvrItem(svrItem) {
		newUpdateId := keyInfo.UpdateID()
		if keyInfo.nextItem != nil {
			newUpdateId = keyInfo.nextItem.UpdateID()
		}
		logger.Info("onFinishLoader skip key=%v updateId=%v newUpdateId=%v", keyInfo.Name, svrItem.UpdateID(), newUpdateId)
		return
	}
	keyInfo.finishDownload(result)

	newKeyInfo := keyInfo.NewItemWithValue(svrItem, result.Value, result.BackupFile)
	needReport = c.handleItemUpdate(newKeyInfo)
	reportInfo = newKeyInfo

	return
}

func (c *confHub) OnTimer(idx int64) {
	logger.Debug("timer:%v", idx)
	c.waiterMgr.processTimeout()
	//todo callback retry

}

func (c *confHub) clean() {
	c.waiterMgr.close()
	c.dumper.Stop()
}

func (c *confHub) emitConfUpdateLatencySLA(item *KeyInfo) {
	if item == nil {
		return
	}
	//首次监听的，不是配置更新引起的下发,不计算
	if item.isItem() && c.waiterMgr.getKeyWaiter(item.Name) != nil {
		return
	}
	if !item.isItem() && !item.Dir.IsItemsFinish() {
		return
	}
	updateType := func(isValid bool) string {
		if isValid {
			return "delete"
		}
		return "update"
	}
	cost := (time.Now().UnixNano() - item.KeyUpdateTime) / 1e6 //毫秒
	if cost < 0 {                                              //一般是由于两台实例的时钟误差导致的。
		cost = 0
	}
	util.EmitConfUpdateLatency(item.Name, c.name, updateType(item.Valid), int(item.ValueSize), int(cost))

}

func (c *confHub) initPathSnapshot(snapshots [][]byte, pathCallback cmodel.PathCallback) (paths []string) {
	for _, k := range snapshots {
		p := NewPathInfoWithSnapshot(k, pathCallback)
		paths = append(paths, p.Path)
		c.keeper.addPathInfo(p)
		logger.Debug("init path snapshot path:[%v] keys:%v", p.Path, tools.ToJsonStringer(p.Items))

	}

	return
}

func (c *confHub) initKeySnapshot(snapshots [][]byte, keyCallback cmodel.KeyCallback) (keys []string) {
	for _, k := range snapshots {
		keyInfo := NewKeyInfoWithSnapshot(k, keyCallback)
		keys = append(keys, keyInfo.Key())
		c.keeper.KeyUpdate(keyInfo)
	}

	return
}
