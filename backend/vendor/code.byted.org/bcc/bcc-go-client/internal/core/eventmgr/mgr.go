package eventmgr

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	clientmodel "code.byted.org/bcc/bcc-go-client/coreclient/model"
	"code.byted.org/bcc/bcc-go-client/internal/core/common"
	internalerror "code.byted.org/bcc/bcc-go-client/internal/error"
	"code.byted.org/bcc/bcc-go-client/logger"
)

// 主事件循环，负责接收配置监听，处理配置变更和发送配置监听
// mgr只依赖接口，common包，不依赖具体实现
type EventMgr struct {
	name       string
	conn       common.Connection
	option     *clientmodel.SdkOptions
	hub        *confHub
	closeChan  chan int
	closeWg    sync.WaitGroup
	closeMu    sync.Mutex
	msgChan    chan interface{}
	taskChan   chan interface{}
	state      *mgrState
	downloader common.Loader
	fetcher    common.Fetcher
	stat       *stat
}

func (e *EventMgr) GetOption() *clientmodel.SdkOptions {
	return e.option
}
func (e *EventMgr) GetName() string {
	return e.name
}
func NewEventMgr(name string, transportBuilder common.TransportBuilder, fetcherBuilder common.FetcherBuilder, loader common.DownloaderBuilder, dumper common.DumpBuilder, option *clientmodel.SdkOptions) *EventMgr {

	e := &EventMgr{
		name:      name,
		option:    option,
		hub:       newConfHub(name, dumper.Build(name, option)),
		closeChan: make(chan int, 1),
		closeWg:   sync.WaitGroup{},
		msgChan:   make(chan interface{}, 100),
		taskChan:  make(chan interface{}, 100),
		state:     &mgrState{},
		stat:      newStat(),
	}
	e.closeWg.Add(1)

	e.conn = transportBuilder.Build(e, name, option)
	e.fetcher = fetcherBuilder.Build(e, option)
	e.downloader = loader.Build(e, option)
	logger.Debug("begin snapshot")
	e.initSnapshot()
	logger.Debug("snapshot finish")

	go e.loop()
	return e
}

// 基于快照进行初始化，作为sidecar模式启动并且重新启动
func (e *EventMgr) initSnapshot() {
	if e.option.Snapshot == nil || e.option.Snapshot.KeyCallback == nil || e.option.Snapshot.PathCallback == nil {
		return
	}

	watchPaths := e.hub.initPathSnapshot(e.option.Snapshot.SnapPaths, e.option.Snapshot.PathCallback)
	watchKeys := e.hub.initKeySnapshot(e.option.Snapshot.SnapKeys, e.option.Snapshot.KeyCallback)

	//这里不需要给推渠道发起watch命令。因为上面两行会将watch的内容写入到hub中。后面在处理OnRegister的回包时，会从hub中获取到带watch的path和key，然后发送给pushSvr

	e.fetcher.OnWatch(common.NewOnWatchMsg("", watchKeys))
	for _, path := range watchPaths {
		e.fetcher.OnWatch(common.NewOnWatchMsg(path, nil))
	}
}

func (e *EventMgr) loop() {
	running := true
	ticker := time.NewTicker(time.Second * 1)
	defer ticker.Stop()
	idx := int64(0)
	//避免channel被置nil，导致panic
	msgChan := e.msgChan
	for running {
		select {
		case msg := <-msgChan:
			logger.Debug("event mgr recv msg_type:%v msg:%v", reflect.TypeOf(msg), msg)
			e.dispatchMsg(msg)

		case task := <-e.taskChan:
			logger.Debug("event mgr recv task type:%v task:%v", reflect.TypeOf(task), task)
			e.dispatchTask(task)

		case <-e.closeChan:
			logger.Debug("event mgr recv close")
			running = false
		case <-ticker.C:
			logger.Trace("event mgr recv ticker")
			e.onTimer(idx)
			idx += 1
		}
	}
	logger.Debug("event mgr loop clean resource")
	e.clean()
	e.closeWg.Done()

}
func (e *EventMgr) AddMsg(msg interface{}) {
	e.msgChan <- msg
}

func (e *EventMgr) AddTask(task interface{}) {
	e.taskChan <- task
}

func (e *EventMgr) onTimer(idx int64) {

	e.hub.OnTimer(idx)

	fetchInterval := int64(e.option.PullChannelOptions.Interval.Seconds())
	if !e.option.PullChannelOptions.Disable && idx%fetchInterval == 0 {
		e.fetcher.NotifyFetchUpdate()
	}

	//每30秒，打点成功回调key的带宽等数据
	if idx%30 == 10 {
		e.stat.emit()
	}
}

// [output] conn->mgr
// 并发安全
func (e *EventMgr) AddWatchKeysTask(task *common.WatchKeysTask) error {
	logger.Debug("add watch keys:%v", task.Keys())
	if e.state.IsClosed() {
		return internalerror.NewError(internalerror.ErrClose, `add watch key "%v" failed, event mgr is closed`, task.Keys())
	}
	e.AddTask(task)
	return nil
}

func (e *EventMgr) onWatchKey(task *common.WatchKeysTask) {
	logger.Debug("on watch keys:%v", task.Keys())
	streamTask := e.hub.AddWatchKey(task)
	if len(streamTask.Items) == 0 {
		return
	}
	e.fetcher.OnWatch(&common.OnWatchMsg{
		Keys: streamTask.Keys(),
	})
	e.conn.AddTask(streamTask)
}

// [output] conn->mgr
// 并发安全
func (e *EventMgr) AddWatchPathTask(task *common.WatchPathTask) error {
	if e.state.IsClosed() {
		return internalerror.NewError(internalerror.ErrClose, `add watch path "%s" failed, event mgr is closed`, task.Path())
	}
	e.AddTask(task)
	return nil
}

func (e *EventMgr) onWatchPath(task *common.WatchPathTask) {
	sTask := e.hub.AddWatchPath(task)
	if sTask.CltDir == nil {
		return
	}
	e.fetcher.OnWatch(&common.OnWatchMsg{
		Path: task.Path(),
	})
	e.conn.AddTask(&sTask)
}

func (e *EventMgr) AddCancelKeysTask(task *common.CancelKeysTask) {
	if e.state.IsClosed() {
		logger.CtxError(task.Ctx(), "add cancel keys :%v err:%v", task.Keys(), internalerror.ErrClose)
		task.Done()
		return
	}
	e.AddTask(task)
}

func (e *EventMgr) onCancelKeys(task *common.CancelKeysTask) {
	defer task.Done()
	notifyKeys := e.hub.CancelKeys(task.Keys(), outsideCancel)
	e.downloader.StopKeys(notifyKeys)

	t := common.StreamCancelKeyTask{Keys: notifyKeys}
	for _, key := range notifyKeys {
		e.fetcher.OnUpdate(&common.OnUpdateKeyMsg{
			Key:      key,
			UpdateId: 0,
			Delete:   true,
		})
	}

	e.conn.AddTask(&t)
}

func (e *EventMgr) AddCancelPathTask(task *common.CancelPathTask) {
	if e.state.IsClosed() {
		logger.CtxError(task.Ctx(), "add cancel path:%v err:%v", task.Path(), internalerror.ErrClose)
		task.Done()
		return
	}
	e.AddTask(task)
}

func (e *EventMgr) onCancelPath(task *common.CancelPathTask) {
	defer task.Done()
	cancelKeys := e.hub.CancelPath(task.Path(), outsideCancel)
	e.downloader.StopKeys(cancelKeys)

	e.fetcher.OnUpdate(&common.OnUpdatePathMsg{
		Path:   task.Path(),
		Delete: true,
	})

	t := common.StreamCancelPathTask{Path: task.Path()}
	e.conn.AddTask(&t)
}

// 收到注册包时，进行发送监听的信息
func (e *EventMgr) onRegister(msg common.RegisterMsg) {
	if err := e.downloader.Init(msg); err != nil {
		logger.Error("downloader init err %v", err)
	}
	//获取所有监听key和path
	keyTask, pathTask := e.hub.GetAllSendTask()

	if len(keyTask.Keys()) > 0 {
		//发送监听key和path
		e.conn.AddTask(keyTask)
	}

	for _, t := range pathTask {
		e.conn.AddTask(t)
	}
	logger.Notice("register ok items=%v dir=%v pingInv=%v", len(keyTask.Items), len(pathTask), msg.PingInterval)
}

func (e *EventMgr) onUpdateKey(task common.UpdateKeyMsg) {

	e.fetcher.OnUpdate(&common.OnUpdateKeyMsg{
		Key:      task.KeyItem.Key(),
		UpdateId: task.KeyItem.UpdateID(),
		Delete:   false,
	})

	keyItem := task.KeyItem
	logger.Trace("onUpdateKey begin key=%v version=%v updateId=%v valid=%v len=%v", keyItem.Key(), keyItem.Version(), keyItem.Valid(), len(keyItem.Value()))

	item, allow := e.hub.isAllowUpdateKey(keyItem)
	if !allow {
		return
	}

	e.handleUpdate(item, keyItem)
}

// 对于watchPath的鉴权失败处理，首次和非首次的监听都不会回调，仅仅是打日志提醒
func (e *EventMgr) onUpdatePath(task common.UpdatePathMsg) {
	logger.Debug("onUpdatePath begin path=%v key=%v version=%v", task.Path, task.KeyItem.Key(), task.KeyItem.Version())

	e.fetcher.OnUpdate(&common.OnUpdatePathMsg{
		Path:   task.Path,
		Delete: false,
	})

	item, allow := e.hub.isAllowUpdatePath(task.Path, task.KeyItem)
	if !allow {
		return
	}
	e.handleUpdate(item, task.KeyItem)
}

func (e *EventMgr) handleUpdate(item *KeyInfo, svrItem *common.ServerItem) {

	e.downloader.Stop(item.Key()) //无论是update还是delete事件，都是需要停止上个版本的下载操作
	e.hub.stopLastUpdateAction(item)
	var needReport bool
	var keyInfo *KeyInfo
	if !item.needCallback(svrItem) {
		item.updateMeta(svrItem)
		needReport = true
		keyInfo = item
	} else {
		item.downRecord.onItemUpdate(svrItem)

		if svrItem.NeedDownload() {
			item.nextItem = svrItem
			e.downloader.AddDownloadTask(&common.DownloadTask{SvrItem: svrItem})
			item.OnAddDownloadTask()
			return
		}

		keyInfo, needReport = e.hub.handleChangeEvent(item, svrItem)
		e.stat.update(keyInfo)
	}

	//上报server
	if needReport {
		if keyInfo.isItem() {
			e.sendReportKey(keyInfo)
		} else {
			e.sendReportPath(keyInfo)
		}
	}
}
func (e *EventMgr) onFinishDownload(msg *common.FinishloaderMsg) {
	result := msg.Result
	svrItem := result.SvrItem

	// 下载完成后的回调处理，不能只从key中获取到的keyInfo，需要区分key/path的监听
	keyInfos := e.hub.keeper.GetKeyInfo(svrItem.Key())
	if len(keyInfos) == 0 {
		logger.Info("miss key, keyInfo:%v", svrItem.KeyInfo())
		return
	}
	var needReport bool
	for _, keyInfo := range keyInfos {
		keyInfo, needReport = e.hub.finishDownload(msg, keyInfo)
		if needReport {
			if keyInfo.isItem() {
				e.sendReportKey(keyInfo)
			} else {
				e.sendReportPath(keyInfo)
			}
		}
		if keyInfo != nil {
			e.stat.update(keyInfo)
		}
	}
}

func (e *EventMgr) onFinishPath(task common.FinishPathMsg) {
	logger.Info("onFinishPath begin path=%v firstTotal=%v msg=%v", task.Path, task.Total, task.FailMsg)
	e.hub.FinishPath(task)
}

func (e *EventMgr) sendReportKey(item *KeyInfo) {
	task := &common.StreamReportKeyTask{Item: item.genCltItem()}
	e.conn.AddTask(task)
}

func (e *EventMgr) sendReportPath(item *KeyInfo) {
	task := &common.StreamReportPathTask{
		Item: item.genCltItem(),
		Path: item.Dir.Path,
	}
	e.conn.AddTask(task)
}

// 并发安全
func (e *EventMgr) Close() {
	if !e.state.Close() {
		return
	}
	e.closeMu.Lock()
	if e.closeChan != nil {
		close(e.closeChan)
	}
	e.closeMu.Unlock()
	e.closeWg.Wait()
}

func (e *EventMgr) clean() {
	e.hub.clean()
	logger.Debug("finish confhub clean")
	e.conn.Close()
	logger.Debug("finish conn close")
	e.fetcher.Close()
	logger.Debug("finish fetcher close")
	e.downloader.Close()
	logger.Debug("finish downloader close")

	//清理未读消息
	e.processUnreadTask()
	logger.Debug("finish processUnreadTask")

	//避免内存泄漏
	e.conn = nil
	e.hub = nil
	e.downloader = nil
	e.msgChan = nil
	e.taskChan = nil
}

func (e *EventMgr) dispatchMsg(msg interface{}) {
	switch m := msg.(type) {
	case *common.FinishloaderMsg:
		e.onFinishDownload(m)
	case *common.UpdateKeyMsg:
		e.onUpdateKey(*m)
	case *common.UpdatePathMsg:
		e.onUpdatePath(*m)
	case *common.FinishPathMsg:
		e.onFinishPath(*m)
	case *common.RegisterMsg:
		e.onRegister(*m)
	case *common.UpdateIntervalMsg:
		e.updateInterval(*m)
	default:
		logger.Warn("not support msg:%T", msg)
	}
}

func (e *EventMgr) dispatchTask(task interface{}) {
	switch t := task.(type) {
	case *common.CancelKeysTask:
		e.onCancelKeys(t)
	case *common.CancelPathTask:
		e.onCancelPath(t)
	case *common.WatchKeysTask:
		e.onWatchKey(t)
	case *common.WatchPathTask:
		e.onWatchPath(t)
	default:
		logger.Warn("not support task:%T", task)
	}
}

func (e *EventMgr) processUnreadTask() {
	err := internalerror.NewMultiError()
	logger.Debug("begin processUnreadTask")
	for {
		select {
		case t := <-e.taskChan:
			logger.Debug("processUnreadTask task=%v", t)
			switch task := t.(type) {
			case *common.WatchKeysTask:
				err.Add(fmt.Sprintf("%v", task.Keys()), internalerror.NewError(internalerror.ErrClose, "close"))
				task.Done(err)
			case *common.WatchPathTask:
				err.Add(fmt.Sprintf("%v", task.Path()), internalerror.NewError(internalerror.ErrClose, "close"))
				task.Done(err)
			case *common.CancelKeysTask:
				task.Done()
			case *common.CancelPathTask:
				task.Done()
			}
		case <-time.After(time.Millisecond):
			return
		}

	}

}

func (e *EventMgr) updateInterval(msg common.UpdateIntervalMsg) {
	if time.Duration(msg.Interval)*time.Second <= e.option.PullChannelOptions.Interval {
		return
	}
	oldInterval := e.option.PullChannelOptions.Interval
	e.option.PullChannelOptions.Interval = time.Duration(msg.Interval) * time.Second
	logger.Debug("get pull server update interval msg, old interval:%+v, new interval:%+v", oldInterval, msg.Interval)
}
