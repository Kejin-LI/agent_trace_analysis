package model

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"code.byted.org/bcc/bcc-go-client/internal/util"
	"code.byted.org/bcc/bcc-go-client/logger"
	"code.byted.org/bcc/tools"
	"code.byted.org/gopkg/env"
)

type SdkOptions struct {
	SDKLang            string            `json:"sdk_lang"`           // 标识客户端的语言
	SDKPath            string            `json:"sdk_path"`           //客户端的引用路径
	RemoteCluster      string            `json:"remote_cluster"`     // push_server的cluster
	RemoteIDC          string            `json:"remote_idc"`         // push_server的idc
	RemotePSM          string            `json:"remote_psm"`         //push_server的psm
	RemoteAddr         string            `json:"remote_addr"`        // push_server的addr
	DisableChannel     bool              `json:"disable_channel"`    // 禁用push_server
	StreamTimeout      time.Duration     `json:"stream_timeout"`     //创建长连接的timeout
	Tags               map[string]string `json:"tags"`               //客户端tag，自定义
	Snapshot           *SnapshotData     `json:"snapshot,omitempty"` // sidecar模式快照数据，用于进程恢复
	DownloadOption     *DownloadOption
	PullChannelOptions *PullChannelOptions `json:"pull_channel_options,omitempty"`
	DisableAuth        bool                `json:"disable_auth"` //是否禁用鉴权
}

func (s *SdkOptions) MarshalMsgPack() []byte {
	logger.Debug("begin sdk opt marshal")
	res, err := tools.MsgPackMarshal(s)
	logger.Debug("sdk opt marshal finish")

	if err != nil {
		logger.Error("impossible marhsal sdkoptions error:%v origin object:%+v", err, s)
		return nil
	}
	return res
}
func (s *SdkOptions) unmarshalMsgPack(input []byte) error {
	opt := &SdkOptions{}
	logger.Debug("begin sdk opt unmarshal")
	err := tools.MsgPackUnmarshal(input, opt)
	logger.Debug("sdk opt unmarshal finish")

	if err != nil {
		return err
	}
	*s = *opt
	return nil
}

type ClientOption func(*SdkOptions)

func DisablePushChannel() ClientOption {
	return func(options *SdkOptions) {
		options.DisableChannel = true
		options.RemoteAddr = "127.0.0.1:1234" //连接到一个不存在的端口模拟禁用推渠道
	}
}

func ClientWithRemoteAddr(addr string) ClientOption {
	return func(options *SdkOptions) {
		if !options.DisableChannel { //不禁用才能设置
			options.RemoteAddr = addr
		}
	}
}
func ClientWithRemoteCluster(cluster string) ClientOption {
	return func(options *SdkOptions) {
		options.RemoteCluster = cluster
	}
}
func ClientWithRemoteIDC(idc string) ClientOption {
	return func(options *SdkOptions) {
		options.RemoteIDC = idc
	}
}
func ClientWithRemotePSM(psm string) ClientOption {
	return func(options *SdkOptions) {
		options.RemotePSM = psm
	}
}
func ClientWithRemoteStreamTimeout(timeout time.Duration) ClientOption {
	return func(options *SdkOptions) {
		options.StreamTimeout = timeout
	}
}
func ClientWithTags(tags map[string]string) ClientOption {
	return func(options *SdkOptions) {
		options.Tags = tags
	}
}
func ClientWithDisableAuth() ClientOption {
	return func(options *SdkOptions) {
		options.DisableAuth = true
	}
}

func ClientWithSnapshot(snapshot []byte, keyCallback KeyCallback, pathCallback PathCallback) (ClientOption, error) {
	opts := &SdkOptions{}
	err := opts.unmarshalMsgPack(snapshot)
	if err != nil {
		msg := fmt.Sprintf("ClientWithSnapshot failed, err:%v", err)
		logger.Fatal("%v", msg)
		return nil, fmt.Errorf(msg)
	}
	return func(options *SdkOptions) {
		*options = *opts
		if options.Snapshot == nil {
			options.Snapshot = &SnapshotData{}
		}
		options.Snapshot.KeyCallback = keyCallback
		options.Snapshot.PathCallback = pathCallback

		if options.PullChannelOptions == nil {
			options.PullChannelOptions = NewDefaultPullChannelOption()
		}
		if options.DownloadOption == nil {
			options.DownloadOption = NewDefaultDownloadOption()
		}
	}, nil
}

func GetClientOption(opt ...ClientOption) *SdkOptions {
	opts := &SdkOptions{
		SDKLang:            "go",
		SDKPath:            "code.byted.org/bcc/bcc-go-client",
		PullChannelOptions: NewDefaultPullChannelOption(),
		DownloadOption:     NewDefaultDownloadOption(),
		DisableAuth:        false,
	}

	if os.Getenv("BCC_CLIENT_WITH_REMOTE_ADDR") != "" {
		opt = append(opt, ClientWithRemoteAddr(os.Getenv("BCC_CLIENT_WITH_REMOTE_ADDR")))
	}
	if os.Getenv("BCC_WITH_PULL_CHANNEL_REMOTE_ADDR") != "" {
		opt = append(opt, WithPullChannelRemoteAddr(os.Getenv("BCC_WITH_PULL_CHANNEL_REMOTE_ADDR")))
	}
	for _, o := range opt {
		o(opts)
	}
	return opts
}

type CancelOption func(o *CancelOptions)
type CancelOptions struct {
	Ctx context.Context
}

func GetCancelOptions(opt ...CancelOption) *CancelOptions {
	opts := &CancelOptions{
		Ctx: util.CreateCtx(),
	}
	for _, o := range opt {
		o(opts)
	}
	return opts
}

type PullChannelOptions struct {
	Disable       bool          `json:"disable"`     // 是否禁用拉渠道, True：禁用
	Interval      time.Duration `json:"interval"`    // 拉渠道定时拉取间隔
	Timeout       time.Duration `json:"timeout"`     // 请求超时时间
	RemoteAddr    string        `json:"remote_addr"` // 自定义拉渠道addr
	RemoteCluster string        `json:"remote_cluster"`
	RemotePSM     string        `json:"remote_psm"`
}

func NewDefaultPullChannelOption() *PullChannelOptions {
	interval := 10
	ei := os.Getenv("BCC_PULL_DEFAULT_INTERVAL_SEC")
	if ei != "" {
		if eInterval, err := strconv.Atoi(ei); err == nil {
			interval = eInterval
		}
	}

	return &PullChannelOptions{
		Interval:      time.Duration(interval) * time.Second,
		Timeout:       10 * time.Second,
		RemotePSM:     "bytedance.bcc.pull_server",
		RemoteCluster: "default",
	}
}

// DisablePullChannel 禁用定时拉取配置渠道
func DisablePullChannel() ClientOption {
	return func(options *SdkOptions) {
		options.PullChannelOptions.Disable = true
	}
}

func WithPullChannelInterval(interval time.Duration) ClientOption {
	return func(options *SdkOptions) {
		options.PullChannelOptions.Interval = interval
	}
}

func WithPullChannelTimeout(timeout time.Duration) ClientOption {
	return func(options *SdkOptions) {
		options.PullChannelOptions.Timeout = timeout
	}
}

func WithPullChannelRemoteAddr(addr string) ClientOption {
	return func(options *SdkOptions) {
		options.PullChannelOptions.RemoteAddr = addr
	}
}
func WithPullChannelRemoteCluster(Cluster string) ClientOption {
	return func(options *SdkOptions) {
		options.PullChannelOptions.RemoteCluster = Cluster
	}
}
func WithPullChannelRemotePsm(psm string) ClientOption {
	return func(options *SdkOptions) {
		options.PullChannelOptions.RemotePSM = psm
	}
}

type DownloadOption struct {
	RandomFail           int           // 用于测试时进行随机失败事件
	BufferSize           int           //
	BigFileSize          int64         // 大小文件下载队列的阈值。默认100MB为大文件：100*1024*1024
	SmallFileDownloadNum int           // 小文件下载队列协程数
	SmallFileTimeout     time.Duration // 小文件请求超时时间
	BigFileDownloadNum   int           // 大文件下载队列协程数
	BigFileTimeout       time.Duration // 大文件请求超时时间
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

func WithDownloadBufferSize(size int) ClientOption {
	return func(options *SdkOptions) {
		options.DownloadOption.BufferSize = size
	}
}
func WithDownloadBigFileSize(size int64) ClientOption {
	return func(options *SdkOptions) {
		options.DownloadOption.BigFileSize = size
	}
}
func WithDownloadSmallFileDownloadNum(num int) ClientOption {
	return func(options *SdkOptions) {
		options.DownloadOption.SmallFileDownloadNum = num
	}
}
func WithDownloadSmallFileTimeout(timeout time.Duration) ClientOption {
	return func(options *SdkOptions) {
		options.DownloadOption.SmallFileTimeout = timeout
	}
}
func WithDownloadBigFileDownloadNum(num int) ClientOption {
	return func(options *SdkOptions) {
		options.DownloadOption.BigFileDownloadNum = num
	}
}
func WithDownloadBigFileTimeout(timeout time.Duration) ClientOption {
	return func(options *SdkOptions) {
		options.DownloadOption.BigFileTimeout = timeout
	}
}
