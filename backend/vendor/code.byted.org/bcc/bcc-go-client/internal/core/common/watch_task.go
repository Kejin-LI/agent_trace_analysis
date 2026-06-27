package common

import (
	"context"
	"sync"
	"time"

	"code.byted.org/bcc/bcc-go-client/coreclient/model"
	internalerror "code.byted.org/bcc/bcc-go-client/internal/error"
)

type WatchReplyResult struct {
	Err error //具体的错误类型有下面所列的
}

type WatchChannel struct {
	ResultChan chan WatchReplyResult
}

func newWatchChannel() WatchChannel {
	return WatchChannel{
		ResultChan: make(chan WatchReplyResult, 2),
	}
}

/*
 Watch Path task
*/

type WatchPathTask struct {
	ctx      context.Context
	callback model.PathCallback
	path     string
	opt      *model.WatchOptions
	channel  WatchChannel
	endTime  time.Time
}

func NewWatchPathTask(ctx context.Context, cb model.PathCallback, path string, opt *model.WatchOptions) *WatchPathTask {
	task := &WatchPathTask{
		ctx:      ctx,
		callback: cb,
		path:     path,
		opt:      opt,
		channel:  newWatchChannel(),
	}
	if opt.Timeout > 0 {
		task.endTime = time.Now().Add(opt.Timeout)
	}

	return task
}

func (w *WatchPathTask) Path() string {
	return w.path
}

func (w *WatchPathTask) GetResult() WatchReplyResult {
	return <-w.channel.ResultChan
}

func (w *WatchPathTask) Option() *model.WatchOptions {
	return w.opt
}

// 阻塞监听时，完成一个key的回调就会调用一次本函数。当所有的key都回调了，那么就可以结束阻塞了。
func (w *WatchPathTask) Done(res *internalerror.MultiError) {
	var err error
	if !res.Empty() {
		err = res
	}
	w.channel.ResultChan <- WatchReplyResult{Err: err}
}

func (w *WatchPathTask) Ctx() context.Context {
	return w.ctx
}

func (w *WatchPathTask) IsTimeout() bool {
	if !w.endTime.IsZero() && time.Now().After(w.endTime) {
		return true
	}
	return false
}

func (w *WatchPathTask) Callback() model.PathCallback {
	return w.callback
}

/*
 Watch Keys task
*/

type WatchKeysTask struct {
	ctx      context.Context //处理超时
	callback model.KeyCallback
	keys     []string
	opt      *model.WatchOptions
	channel  WatchChannel
	endTime  time.Time
}

// 业务方需要根据https://bytedance.feishu.cn/wiki/wikcn0aptaTgbTC55AdEJHin39f 介绍的方法获取监听的结果
func NewWatchKeysTask(ctx context.Context, cb model.KeyCallback, keys []string, opt *model.WatchOptions) *WatchKeysTask {
	task := &WatchKeysTask{
		ctx:      ctx,
		callback: cb,
		keys:     keys,
		opt:      opt,
		channel:  newWatchChannel(),
	}
	if opt.Timeout > 0 {
		task.endTime = time.Now().Add(opt.Timeout)
	}
	return task
}

func (w *WatchKeysTask) Keys() []string {
	return w.keys
}

func (w *WatchKeysTask) Option() *model.WatchOptions {
	return w.opt
}
func (w *WatchKeysTask) Callback() model.KeyCallback {
	return w.callback
}

func (w *WatchKeysTask) FastTerminateOption() bool {
	return w.opt.FastTerminate
}

func (w *WatchKeysTask) Ctx() context.Context {
	return w.ctx
}

// 阻塞监听时，完成一个key的回调就会调用一次本函数。当所有的key都回调了，那么就可以结束阻塞了。
func (w *WatchKeysTask) Done(res *internalerror.MultiError) {
	var err error
	if !res.Empty() {
		err = res
	}
	w.channel.ResultChan <- WatchReplyResult{Err: err}
}

func (w *WatchKeysTask) GetResult() WatchReplyResult {

	return <-w.channel.ResultChan
}

func (w *WatchKeysTask) IsTimeout() bool {
	if !w.endTime.IsZero() && time.Now().After(w.endTime) {
		return true
	}
	return false
}

/*
 Cancel keys task
*/

type CancelKeysTask struct {
	ctx  context.Context
	keys []string
	wg   sync.WaitGroup
}

func NewCancelKeysTask(ctx context.Context, keys []string) *CancelKeysTask {
	task := &CancelKeysTask{
		ctx:  ctx,
		keys: keys,
	}
	task.wg.Add(1)
	return task
}

func (t *CancelKeysTask) Done() {
	t.wg.Done()
}
func (t *CancelKeysTask) Wait() {
	t.wg.Wait()
}

func (t *CancelKeysTask) Keys() []string {
	return t.keys
}

func (t *CancelKeysTask) Ctx() context.Context {
	return t.ctx
}

/*
 Cancel Path task
*/

type CancelPathTask struct {
	ctx  context.Context
	path string
	wg   sync.WaitGroup
}

func NewCancelPathTask(ctx context.Context, path string) *CancelPathTask {
	task := &CancelPathTask{
		ctx:  ctx,
		path: path,
	}
	task.wg.Add(1)
	return task
}
func (t *CancelPathTask) Done() {
	t.wg.Done()
}
func (t *CancelPathTask) Path() string {
	return t.path
}
func (t *CancelPathTask) Wait() {
	t.wg.Wait()
}

func (t *CancelPathTask) Ctx() context.Context {
	return t.ctx
}
