package eventmgr

import (
	"context"
	"fmt"
	"time"

	"code.byted.org/bcc/bcc-go-client/internal/core/common"
	internalerror "code.byted.org/bcc/bcc-go-client/internal/error"
	"code.byted.org/bcc/bcc-go-client/internal/util"
	"code.byted.org/bcc/bcc-go-client/logger"
)

//负责处理watch任务首次监听的阻塞处理，当监听的key/path都返回后，释放业务方的阻塞。

type PathWaiter struct {
	path          string
	ctx           context.Context
	task          *common.WatchPathTask
	svrFinishFlag bool //svr是否已经下发过结束
	CreateTime    time.Time
	err           *internalerror.MultiError
}

func newPathWaiterByWatchTask(task *common.WatchPathTask) *PathWaiter {
	waiter := &PathWaiter{
		ctx:           task.Ctx(),
		task:          task,
		svrFinishFlag: false,
		CreateTime:    time.Now(),
		err:           internalerror.NewMultiError(),
		path:          task.Path(),
	}

	return waiter
}

func (w *PathWaiter) replyFinish() {
	w.task.Done(w.err)
}

func (w *PathWaiter) replyKeyError(key string, err internalerror.BaseError) {
	w.err.Add(key, err)
}

func (w *PathWaiter) replyError(err internalerror.BaseError) {
	w.err.Add(w.path, err)
	w.task.Done(w.err)
	util.EmitError(w.path, fmt.Sprintf("watchPath.%v", w.err))
}

func (w *PathWaiter) DelError(key string) {
	w.err.Del(key)
}

type keyWaiter struct {
	id        int64 //keyWaiterMap id
	ctx       context.Context
	task      *common.WatchKeysTask
	err       *internalerror.MultiError
	watchKeys map[string]bool //这里的key不包含reuse的 //保存没有回调完成的
}

func newKeyWaiterByWatchTask(task *common.WatchKeysTask) *keyWaiter {
	waiter := &keyWaiter{
		ctx:       task.Ctx(),
		task:      task,
		watchKeys: make(map[string]bool),
		err:       internalerror.NewMultiError(),
	}

	for _, key := range task.Keys() {
		waiter.watchKeys[key] = true
	}

	return waiter
}

func (w *keyWaiter) replyKeyError(key string, err internalerror.BaseError) {
	w.err.Add(key, err)
	util.EmitError(key, fmt.Sprintf("watchKey.%v", err))
}

func (w *keyWaiter) replyKeyErrorAndDelete(key string, err internalerror.BaseError) {
	w.replyKeyError(key, err)
	delete(w.watchKeys, key)
}

// 对剩余的key赋值error
func (w *keyWaiter) replyFinish(err internalerror.BaseError) {
	if err != nil {
		for key := range w.watchKeys {
			w.replyKeyError(key, err)
		}
	}
	logger.Debug("key wait finish:%v", err)
	w.task.Done(w.err)
}

func (w *keyWaiter) checkFastTerminate() bool {
	if len(w.watchKeys) == 0 || (w.task.FastTerminateOption() && len(w.watchKeys) != len(w.task.Keys())) {
		w.replyFinish(internalerror.NewError(internalerror.ErrUnstart, "fast terminate"))
		return true
	}
	return false
}

// 负责维护所有首次监听的任务。当watch任务中所有的key都处理(成功回调或者中途失败)，那么会将该watch任务删除。
// 其主要是通过通过watch_task结构体的函数通知业务方的。业务方可以通过https://bytedance.feishu.cn/wiki/wikcn0aptaTgbTC55AdEJHin39f介绍的
// 方法获取到waiter发出的通知。
// 对于一次性监听的形式，不应该由watchWaiterMgr做这个事情。而且由https://bytedance.feishu.cn/wiki/wikcn0aptaTgbTC55AdEJHin39f介绍的业务方
// 在结束阻塞后，主动cancel监听的key。
type watchWaiterMgr struct {
	pathTaskMap   map[string]*PathWaiter //首次监听,path 完成或者错误就删除
	keyWaiterMap  map[int64]*keyWaiter   //这里用map主要是为了好删除已经完成的waiter waiterId->waiter
	keyWatchCount int64
}

func newWatchWaiterMgr() *watchWaiterMgr {
	waiter := &watchWaiterMgr{
		pathTaskMap:   make(map[string]*PathWaiter),
		keyWaiterMap:  make(map[int64]*keyWaiter),
		keyWatchCount: 0,
	}

	return waiter
}

func (w *watchWaiterMgr) addKeyWaiter(waiter *keyWaiter) {
	if waiter == nil {
		return
	}

	w.keyWatchCount += 1
	waiter.id = w.keyWatchCount
	w.keyWaiterMap[w.keyWatchCount] = waiter
}

func (w *watchWaiterMgr) addPathWaiter(waiter *PathWaiter) {
	if waiter == nil {
		return
	}

	w.pathTaskMap[waiter.path] = waiter
}

//============================================================================================

func (w *watchWaiterMgr) replyKey(key string, err internalerror.BaseError) {
	waiter := w.getKeyWaiter(key)

	if waiter == nil { //没有找到
		return
	}
	if err != nil {
		waiter.replyKeyError(key, err)
	}

	delete(waiter.watchKeys, key)
	logger.Debug("reply key waiter key:%v err:%v waiter:%+v", key, err, waiter)
	if len(waiter.watchKeys) == 0 { //没有已经待处理的key，那么说明这个task可以结束了
		w.finishWatchKeyTask(waiter)
	}
}
func (w *watchWaiterMgr) getKeyWaiter(key string) *keyWaiter {
	for _, wt := range w.keyWaiterMap {
		if wt.watchKeys[key] {
			return wt
		}
	}
	return nil
}
func (w *watchWaiterMgr) replyPathKey(item *KeyInfo, err internalerror.BaseError) {
	waiter := w.pathTaskMap[item.Path()]
	if waiter == nil {
		return
	}

	if err != nil {
		waiter.replyKeyError(item.Key(), err)
	} else {
		waiter.DelError(item.Key())
	}
	item.Dir.ItemFinish(item)

	if waiter.svrFinishFlag && item.Dir.IsItemsFinish() {
		w.finishWatchPathTask(waiter)
	}
}

// path级别的错误
func (w *watchWaiterMgr) replyPathError(path string, err internalerror.BaseError) {
	waiter := w.pathTaskMap[path]
	if waiter == nil {
		return
	}
	waiter.replyError(err)
	delete(w.pathTaskMap, path)
}

func (w *watchWaiterMgr) replyPathFinish(pathInfo *PathInfo) {
	waiter := w.pathTaskMap[pathInfo.Path]
	if waiter == nil {
		return
	}

	waiter.svrFinishFlag = true

	//并不能收到push_svr下发的PathFinish就认为任务已经结束了。因为可能前面还有一个大文件的key还在下载中。
	if pathInfo.IsItemsFinish() {
		w.finishWatchPathTask(waiter)
	}
}

func (w *watchWaiterMgr) finishWatchKeyTask(waiter *keyWaiter) {
	waiter.replyFinish(nil)
	delete(w.keyWaiterMap, waiter.id)
}

func (w *watchWaiterMgr) finishWatchPathTask(waiter *PathWaiter) {

	waiter.replyFinish()
	delete(w.pathTaskMap, waiter.path)
}

func (w *watchWaiterMgr) processTimeout() {
	for path, waiter := range w.pathTaskMap {
		if waiter.task.IsTimeout() {
			delete(w.pathTaskMap, path)
			waiter.replyError(internalerror.NewError(internalerror.ErrTimeout, `watch path "%s" timeout`, path))
		}
	}

	for id, waiter := range w.keyWaiterMap {
		if waiter.task.IsTimeout() {
			delete(w.keyWaiterMap, id)
			waiter.replyFinish(internalerror.NewError(internalerror.ErrTimeout, `watch keys "%v" timeout`, id))
		}
	}
}

// 清理等待的task
func (w *watchWaiterMgr) close() {
	for _, waiter := range w.keyWaiterMap {
		waiter.replyFinish(internalerror.NewError(internalerror.ErrClose, "watch keys"))
	}
	for _, waiter := range w.pathTaskMap {
		waiter.replyError(internalerror.NewError(internalerror.ErrClose, "watch path"))
	}
}
