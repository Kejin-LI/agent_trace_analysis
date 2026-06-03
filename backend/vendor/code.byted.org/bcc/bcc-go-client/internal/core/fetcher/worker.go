package fetcher

import (
	"time"

	"code.byted.org/bcc/bcc-go-client/internal/core/common"
	"code.byted.org/bcc/bcc-go-client/internal/util"
	"code.byted.org/bcc/bcc-go-client/logger"
)

type worker struct {
	closeChan  chan int
	pullClient *client
	taskChan   chan fetchRequest
	mgr        common.MsgHandler
}
type fetchOptions struct {
	timeout     time.Duration
	psm         string
	addr        string
	cluster     string
	SdkLang     string
	SdkPath     string
	Tags        map[string]string
	DisableAuth bool
}

func newWorker(mgr common.MsgHandler, opts fetchOptions) *worker {

	return &worker{
		closeChan:  make(chan int, 1),
		pullClient: newFetchClient(opts),
		taskChan:   make(chan fetchRequest, 15),
		mgr:        mgr,
	}
}

func (w *worker) run() {
	go w.work()
}
func (w *worker) AddPullConfTask(msg fetchRequest) {
	w.taskChan <- msg
}

// 独立协程，只负责拉取，不负责修改
func (w *worker) work() {
	for {
		select {
		case <-w.closeChan:
			logger.Debug("exit")
			return
		case msg := <-w.taskChan:
			w.pullConf(msg)

		}
	}
}
func (w *worker) pullConf(msg fetchRequest) {
	pathMsg, keyMsg, finishPathMsg, updateIntervalMsg, err := w.pullClient.fetch(msg)
	if err != nil {
		util.EmitFetchError(1)
		logger.Warn("fetch path:%v keys:%v fail:%v", msg.pathList, msg.keyList, err)
		return
	}
	for _, p := range pathMsg {
		w.mgr.AddMsg(p)
	}
	for _, p := range finishPathMsg {
		w.mgr.AddMsg(p)
	}
	for _, k := range keyMsg {
		w.mgr.AddMsg(k)
	}
	if updateIntervalMsg != nil {
		w.mgr.AddMsg(updateIntervalMsg)
	}
}
func (w *worker) close() {
	w.closeChan <- 1
}
