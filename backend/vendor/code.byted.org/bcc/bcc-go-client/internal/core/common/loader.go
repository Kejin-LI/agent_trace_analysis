package common

import (
	"time"

	"code.byted.org/bcc/bcc-go-client/coreclient/model"
	"code.byted.org/gopkg/env"
)

var EMPTY_VALUE = []byte("[clear]") //常量

type DownloadOption struct {
	RandomFail           int           //用于测试时进行随机失败事件
	BufferSize           int           //
	BigFileSize          int64         //大小文件下载队列的阈值。默认100MB为大文件：100*1024*1024
	SmallFileDownloadNum int           //小文件下载队列协程数
	SmallFileTimeout     time.Duration //小文件请求超时时间
	BigFileDownloadNum   int           //大文件下载队列协程数
	BigFileTimeout       time.Duration //大文件请求超时时间
}

func NewDefaultDownloadOption() *DownloadOption {
	// 默认小文件下载队列为5个，大文件(大于100M)下载队列为1个
	smallNum, smallTimeout := 5, time.Second*30
	bigNum, bigTimeout := 1, time.Minute*10
	if env.IsBoe() {
		smallNum, smallTimeout = 5, time.Second*120
		bigNum, bigTimeout = 1, time.Minute*30
	}
	return &DownloadOption{
		BufferSize:           16 * 1024 * 1024,
		BigFileSize:          100 * 1024 * 1024,
		SmallFileDownloadNum: smallNum,
		SmallFileTimeout:     smallTimeout,
		BigFileDownloadNum:   bigNum,
		BigFileTimeout:       bigTimeout,
	}
}

func (opt *DownloadOption) WithBufferSize(size int) *DownloadOption {
	opt.BufferSize = size
	return opt
}
func (opt *DownloadOption) WithBigFileSize(size int64) *DownloadOption {
	opt.BigFileSize = size
	return opt
}
func (opt *DownloadOption) WithSmallFileDownloadNum(num int) *DownloadOption {
	opt.SmallFileDownloadNum = num
	return opt
}

func (opt *DownloadOption) WithSmallFileTimeout(timeout time.Duration) *DownloadOption {
	opt.SmallFileTimeout = timeout
	return opt
}

func (opt *DownloadOption) WithBigFileDownloadNum(num int) *DownloadOption {
	opt.BigFileDownloadNum = num
	return opt
}

func (opt *DownloadOption) WithBigFileTimeout(timeout time.Duration) *DownloadOption {
	opt.BigFileTimeout = timeout
	return opt
}

type DownloadTask struct {
	SvrItem *ServerItem
	Opt     model.WatchOptions //【input】监听参数，控制下载行为。
}

// LoaderResult 下载结果
type LoaderResult struct {
	Value      []byte //返回数据
	BackupFile string //返回数据
	Source     DownloadItemSource
	Result     DownloadItemResult
	FailMsg    string
	BeginTime  time.Time
	SvrItem    *ServerItem // 下载中
}

// FinishloaderMsg 大文件下载完成后的消息结果
type FinishloaderMsg struct {
	Result *LoaderResult
}
