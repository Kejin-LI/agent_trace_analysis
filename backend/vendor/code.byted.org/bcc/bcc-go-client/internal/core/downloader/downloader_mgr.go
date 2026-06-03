package downloader

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"code.byted.org/bcc/bcc-go-client/coreclient/model"
	"code.byted.org/bcc/bcc-go-client/internal/core/common"
	"code.byted.org/bcc/bcc-go-client/internal/util"
	"code.byted.org/bcc/bcc-go-client/logger"
)

const (
	openState   uint32 = 0
	closedState uint32 = 1
)

type downloadTask struct {
	mu        sync.RWMutex //
	keyName   string
	SvrItem   *common.ServerItem
	fileSize  int64
	isCancel  atomic.Value
	failCount int64
	result    *common.LoaderResult
	opt       taskOption
}

type taskOption struct {
	disableMemory     bool
	disableBackupFile bool
}

func (d *downloadTask) isCanceled() bool {
	return d.isCancel.Load() == true
}

func (d *downloadTask) setCancel() {
	d.isCancel.Store(true)
}

func (d *downloadTask) getResult() *common.LoaderResult {
	return d.result
}

func (d *downloadTask) setResult(result *common.LoaderResult) {
	d.result = result
}

type stopTask struct {
	key string
}

//========================================================================================

type delayTask struct {
	task          *downloadTask
	nextRetryTime time.Time
}

type downloadControl struct {
	stopChan      chan bool
	bigFileChan   chan *downloadTask
	smallFileChan chan *downloadTask

	bigFileDownloader   *downloader
	smallFileDownloader *downloader

	finishDownloadChan chan *downloadTask
}

//========================================================================================

type taskControl struct {
	taskChan  chan bool
	taskMutex sync.Mutex
	taskQueue []*downloadTask
}

func (t *taskControl) addDownloadTask(task *downloadTask) {
	t.taskMutex.Lock()
	defer t.taskMutex.Unlock()

	t.taskQueue = append(t.taskQueue, task)
	//不将sendQueue定义成channel，而是将额外定义一个sendCh。主要是不想定义一个超长的sendQueue channel(太短的话，会阻塞主协程)。如果是slice的话，就可以动态增长了。
	//sendCh的作用只是用来通知协程有没有数据过来，长度为2。 基本不占用内存.
	if len(t.taskQueue) == 1 {
		t.taskChan <- true
	} else if len(t.taskQueue) > 1000 {
		logger.Error("taskQueue.100")
		if len(t.taskQueue) > 2000 {
			logger.Error("taskQueue.200")
		}
	}
}
func (t *taskControl) swapMsg() []*downloadTask {
	t.taskMutex.Lock()
	defer t.taskMutex.Unlock()

	mq := t.taskQueue
	t.taskQueue = nil

	return mq
}

//========================================================================================

type DownloaderMgr struct {
	mgr              common.MsgHandler
	closeChan        chan bool
	downloadTaskChan chan *common.DownloadTask
	stopTaskChan     chan *stopTask
	readyTaskMap     map[string]*downloadTask //已经传到bigFileChan 或者smallFileChan里面的任务
	delayTaskMap     map[string]*delayTask    //失败重试的等待任务
	state            uint32
	opt              *model.DownloadOption
	dc               downloadControl
	tc               *taskControl
}

func NewDownloaderMgr(handler common.MsgHandler, opt *model.DownloadOption) *DownloaderMgr {
	initBufferPool(opt.BufferSize)

	tc := &taskControl{
		taskChan: make(chan bool, 2),
	}
	mgr := &DownloaderMgr{
		mgr:              handler,
		closeChan:        make(chan bool, 1),
		downloadTaskChan: make(chan *common.DownloadTask, 10),
		stopTaskChan:     make(chan *stopTask, 10),
		opt:              opt,
		readyTaskMap:     make(map[string]*downloadTask),
		delayTaskMap:     make(map[string]*delayTask),
		dc: downloadControl{
			stopChan:            make(chan bool, 1),
			bigFileChan:         make(chan *downloadTask, 10),
			smallFileChan:       make(chan *downloadTask, 100),
			finishDownloadChan:  make(chan *downloadTask, 100),
			bigFileDownloader:   newDownloader(saveOption{timeout: opt.BigFileTimeout}),
			smallFileDownloader: newDownloader(saveOption{timeout: opt.SmallFileTimeout}),
		},
		tc: tc,
	}

	{
		//这里做并发控制
		for i := 0; i < opt.BigFileDownloadNum; i++ {
			go mgr.bigFileRun()
		}

		for i := 0; i < opt.SmallFileDownloadNum; i++ {
			go mgr.smallFileRun()
		}
	}

	// 另起协程进行消息写队列，避免主协程阻塞
	go mgr.tcRun()

	go mgr.run()

	return mgr
}

func (d *DownloaderMgr) tcRun() {
	for {
		select {
		case <-d.dc.stopChan:
			return
		case <-d.tc.taskChan:
			tasks := d.tc.swapMsg()
			for _, task := range tasks {
				if task.fileSize > d.opt.BigFileSize {
					d.dc.bigFileChan <- task
				} else {
					d.dc.smallFileChan <- task
				}
			}
		}
	}
}

// Init 握手拿到相关tos信息后，完成初始化
func (d *DownloaderMgr) Init(task common.RegisterMsg) error {
	// 预留给动态调整的下载参数
	return nil
}

func (d *DownloaderMgr) AddDownloadTask(task *common.DownloadTask) {
	d.downloadTaskChan <- task
}

// Stop 取消下载任务
func (d *DownloaderMgr) Stop(key string) {
	d.stopTaskChan <- &stopTask{key}
}

func (d *DownloaderMgr) StopKeys(keys []string) {
	for _, key := range keys {
		d.stopTaskChan <- &stopTask{key}
	}
}

func (d *DownloaderMgr) run() {
	timer := time.NewTicker(1 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-d.closeChan:
			//做一系列终止操作
			d.dc.stopChan <- true
			return
		case task := <-d.downloadTaskChan:
			d.handleDownloadTask(task)
		case task := <-d.stopTaskChan:
			d.handleStopTask(task)
		case task := <-d.dc.finishDownloadChan:
			d.handleFinishTask(task)
		case <-timer.C:
			d.handleDelayTask()
		}
	}
}

func (d *DownloaderMgr) handleDownloadTask(t *common.DownloadTask) {
	task := &downloadTask{
		keyName:  t.SvrItem.Key(),
		SvrItem:  t.SvrItem,
		fileSize: t.SvrItem.Size(),
		result: &common.LoaderResult{
			BeginTime: time.Now(),
			SvrItem:   t.SvrItem,
		},
		opt: taskOption{
			disableMemory:     t.Opt.DisableMemory || t.Opt.BigFileDisableMemory,
			disableBackupFile: t.Opt.DisableBackupFile,
		},
	}

	d.dispatchDownloadTask(task)
}

func (d *DownloaderMgr) dispatchDownloadTask(task *downloadTask) {
	if oldTask, exist := d.readyTaskMap[task.keyName]; exist && oldTask != task {
		// 如果有新的执行任务，则进行取消掉旧的下载逻辑
		oldTask.setCancel()
	}

	d.readyTaskMap[task.keyName] = task

	d.tc.addDownloadTask(task)
}

func (d *DownloaderMgr) handleStopTask(t *stopTask) {
	//取消还在等待下载的文件
	if task, exist := d.readyTaskMap[t.key]; exist {
		task.setCancel()
	} else if _, ex := d.delayTaskMap[t.key]; ex {
		delete(d.delayTaskMap, t.key) //删除即可
	}
}

func (d *DownloaderMgr) handleFinishTask(task *downloadTask) {
	if task.isCanceled() { //下载过程中被取消了
		logger.Debug("task在下载过程中被取消了")
		return
	}
	result := task.getResult()
	if result.Source == common.DOWNLOAD_SOURCE_FAIL {
		task.failCount += 1
	}
	switch result.Result {
	case common.DOWNLOAD_RESULT_TOS_RETRY, common.DOWNLOAD_RESULT_TOS_NOTFOUND:
		if task.failCount < 20 {
			second := math.Pow(3, float64(task.failCount)) //3s、9s、27s、1m、4m ... 36year
			nextRetryTime := time.Now().Add(time.Second * time.Duration(second))
			dTask := delayTask{
				task:          task,
				nextRetryTime: nextRetryTime,
			}
			d.delayTaskMap[task.keyName] = &dTask
		}
		util.EmitErrorWatch(task.keyName, int(task.SvrItem.Version()), result.Source.String(), result.Result.String())
	}
	d.addRespMsg(&common.FinishloaderMsg{
		Result: result,
	})
	delete(d.readyTaskMap, task.keyName)

}

func (d *DownloaderMgr) addRespMsg(msg *common.FinishloaderMsg) {
	// 进行下载回调
	d.mgr.AddMsg(msg)
}

func (d *DownloaderMgr) handleDelayTask() {
	for keyName, task := range d.delayTaskMap {
		if task.task.isCanceled() {
			logger.Debug("直接丢弃不是最新的超时任务 %v", task)
			// 直接丢弃不是最新的超时任务
			delete(d.delayTaskMap, keyName)
		}
		if time.Now().After(task.nextRetryTime) {
			delete(d.delayTaskMap, keyName)
			d.dispatchDownloadTask(task.task)
		}
	}
}

func (d *DownloaderMgr) Close() {
	if !atomic.CompareAndSwapUint32(&d.state, openState, closedState) {
		return
	}
	close(d.closeChan)
}

func (d *DownloaderMgr) Closed() bool {
	return atomic.LoadUint32(&d.state) == closedState
}

//========================================================================================

func (d *DownloaderMgr) bigFileRun() {
	for {
		select {
		case <-d.dc.stopChan:
			return
		case task := <-d.dc.bigFileChan:
			d.dc.bigFileDownloader.downloadFile(task)
			d.dc.finishDownloadChan <- task
		}
	}
}

func (d *DownloaderMgr) smallFileRun() {
	for {
		select {
		case <-d.dc.stopChan:
			return
		case task := <-d.dc.smallFileChan:
			d.dc.smallFileDownloader.downloadFile(task)
			d.dc.finishDownloadChan <- task
		}
	}
}
