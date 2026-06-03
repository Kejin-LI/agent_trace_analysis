package common

import (
	"context"

	"code.byted.org/bcc/bcc-go-client/coreclient/model"
	internalerror "code.byted.org/bcc/bcc-go-client/internal/error"
)

type Connection interface {
	MsgHandler
	NeedRegister() bool //mgr定时检查注册情况，并进行重新注册
	AddTask(task interface{})
	Close()
}

// Loader  负责大文件下载。下载完毕后，会发起一个完成task通知EventMg
type Loader interface {
	Stop(key string) //取消下载
	StopKeys(keys []string)
	AddDownloadTask(task *DownloadTask) //添加下载任务
	// Init 严重错误异常返回error，不影响下载功能只打错误日志
	// 可重入，每次register包都需要调用
	Init(task RegisterMsg) error
	Close()
}
type Fetcher interface {
	OnWatch(msg *OnWatchMsg)
	OnUpdate(msg OnUpdateMsg) //更新、删除
	NotifyFetchUpdate()       //定时轮询pull server
	Close()                   //关闭工作协程
}

type Dumper interface {
	StorageItem(item DumpItem)
	Stop()
}

type WatchTask interface {
	AddError(err internalerror.Error)
	Done() // 异步任务完成，通知等待者
	Ctx() context.Context
}

type MsgHandler interface {
	AddMsg(interface{})
}
type EventMgr interface {
	AddWatchKeysTask(task *WatchKeysTask) error
	AddWatchPathTask(task *WatchPathTask) error
	AddCancelKeysTask(task *CancelKeysTask)
	AddCancelPathTask(task *CancelPathTask)
	Close()
}

type TransportBuilder interface {
	Build(eventHandler MsgHandler, name string, opt *model.SdkOptions) Connection
}
type FetcherBuilder interface {
	Build(eventHandler MsgHandler, sdkOpts *model.SdkOptions) Fetcher
}

type DownloaderBuilder interface {
	Build(eventHandler MsgHandler, sdkOpts *model.SdkOptions) Loader
}

// 将配置dump到磁盘中，用于排查问题
type DumpBuilder interface {
	Build(name string, sdkOpts *model.SdkOptions) Dumper
}
