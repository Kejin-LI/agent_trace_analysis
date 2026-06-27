package model

import (
	"context"
	"time"

	"code.byted.org/bcc/bcc-go-client/internal/util"
	"code.byted.org/bcc/bcc-go-client/logger"
	"code.byted.org/bcc/tools"
)

type WatchOption func(o *WatchOptions)

type WatchOptions struct {
	Timeout              time.Duration   `json:"timeout"`
	EnableEmpty          bool            `json:"enable_empty"`
	DisableListen        bool            `json:"disable_listen"`
	DisableMemory        bool            `json:"disable_memory"`
	FastTerminate        bool            `json:"fast_terminate"`
	DisableBackupFile    bool            `json:"disable_backup_file"`
	Ctx                  context.Context `msgpack:"-"`                    //context 不能序列化，否则panic
	BigFileDisableMemory bool            `json:"big_file_disable_memory"` // 所有系统定义的大文件不只写磁盘文件不保留在内存
	// 增加 option 必须对应修改初始化的值
}

func (o *WatchOptions) MarshalMsgPack() []byte {
	logger.Debug("begin watch opt marshal")
	res, err := tools.MsgPackMarshal(o)
	logger.Debug("watch opt marshal finish")
	if err != nil {
		logger.Fatal("impossible marshal watch options error:%v", err)
		return nil
	}
	return res
}
func (o *WatchOptions) UnmarshalMsgpack(input []byte) {
	newRes := &WatchOptions{}
	logger.Debug("begin watch opt unmarshal")
	err := tools.MsgPackUnmarshal(input, newRes)
	logger.Debug("watch opt unmarshal finish")
	if err != nil {
		logger.Fatal("impossible unmarshal watch options error:%v", err)
		return
	}
	*o = *newRes
}

func GetWatchOption(opt ...WatchOption) *WatchOptions {
	opts := &WatchOptions{
		Timeout:              0 * time.Second, //默认一直阻塞
		EnableEmpty:          false,
		DisableListen:        false,
		DisableMemory:        false,
		FastTerminate:        false,
		DisableBackupFile:    false,
		Ctx:                  util.CreateCtx(),
		BigFileDisableMemory: false,
	}
	for _, o := range opt {
		o(opts)
	}

	if opts.DisableMemory && opts.DisableBackupFile {
		tools.Panic("new bcc sdk fail option conflicts! should both disable memory and disable backup file")
	}

	return opts
}
func WithWatchSnapshot(snapshot []byte) WatchOption {
	opts := &WatchOptions{}
	opts.UnmarshalMsgpack(snapshot)

	return func(o *WatchOptions) {
		o.Timeout = opts.Timeout
		o.EnableEmpty = opts.EnableEmpty
		o.DisableListen = opts.DisableListen
		o.DisableMemory = opts.DisableMemory
		o.FastTerminate = opts.FastTerminate
		o.DisableBackupFile = opts.DisableBackupFile
		o.BigFileDisableMemory = opts.BigFileDisableMemory
	}
}

func WithWatchTimeout(timeout time.Duration) WatchOption {
	return func(o *WatchOptions) {
		o.Timeout = timeout
	}
}
func WithWatchEmpty() WatchOption {
	return func(o *WatchOptions) {
		o.EnableEmpty = true
	}
}

func WithWatchDisableListen() WatchOption {
	return func(o *WatchOptions) {
		o.DisableListen = true
	}
}
func WithWatchDisableMemory() WatchOption {
	return func(o *WatchOptions) {
		o.DisableMemory = true
	}
}
func WithWatchFastTerminate() WatchOption {
	return func(o *WatchOptions) {
		o.FastTerminate = true
	}
}

func WithWatchBigfileDisableMemory() WatchOption {
	return func(o *WatchOptions) {
		o.BigFileDisableMemory = true
	}
}

func WithWatchDisableBackupFile() WatchOption {
	return func(o *WatchOptions) {
		o.DisableBackupFile = true
	}
}
func WithWatchContext(ctx context.Context) WatchOption {
	return func(o *WatchOptions) {
		o.Ctx = ctx
	}
}
