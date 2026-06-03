package confclient

import (
	"fmt"
	"time"

	"code.byted.org/bcc/bcc-go-client/coreclient/model"
)

type (
	ClientOption func(o *clientOption)
	clientOption struct {
		keyCallback          KeyCallback            // config callback
		pathCallback         PathCallback           // path callback
		initTimeout          time.Duration          // default is 0，timeout for init, used in key&path
		disableListen        bool                   // default is false，used in key&path
		enableEmpty          bool                   // default is false, whether allow to listen nonexist config，used in key
		newCoreClient        bool                   // default is false, whether to use independent cds namespace, it will influence dup watchs for key&path, better not use for performance consideration.
		newCoreClientName    string                 // 默认是空，不创建新的coreClient。相同的字符串会使得共享一个新创建的coreClient。保留newCoreClient有两个原因: 1. tcc那边已经使用了newCoreClient这个option。 2. 两者还有一些区别。 newCoreClient肯定不会重名，不会重复监听。newCoreClientName重名仍会有重复监听的问题。
		modelMap             map[string]interface{} // model map to deserialize, bcKey -> model
		disableAuth          bool
		identifier           string // identifier for client, used for metrics
		tccSdkVersion        string // tcc sdk version if identifier is tcc
		disablePullChannel   bool   // disable pull channel, just for test
		disablePushChannel   bool   // disable push channel, just for test
		pushChannelCluster   string // push channel cluster。 一般来说，push和pull的集群是一样的。这里先预留不同的能力
		pullChannelCluster   string // pull channel cluster
		SmallFileDownloadNum int    // 小文件下载队列协程数
		BigFileDownloadNum   int    // 大文件下载队列协程数
	}
)

func newClientOption(opts ...ClientOption) clientOption {
	opt := clientOption{
		initTimeout:        0,
		disableListen:      false,
		enableEmpty:        false,
		newCoreClient:      false,
		newCoreClientName:  "",
		identifier:         "bcc",
		tccSdkVersion:      "-",
		disablePullChannel: false,
		disablePushChannel: false,
		pushChannelCluster: "",
		pullChannelCluster: "",
	}
	for _, o := range opts {
		o(&opt)
	}
	return opt
}

func (c clientOption) isValid() error {
	needNewCoreClient := false
	msg := ""

	if c.newCoreClientName == defaultClientName { //bcc是defaultClient的名字，不能用
		return fmt.Errorf(`NewCoreClientName cannot equal ` + defaultClientName)
	}

	//独立的集群必须使用单独的coreClient,避免被污染
	if c.pushChannelCluster != "" || c.pullChannelCluster != "" {
		msg += "|option channel cluster|"
		needNewCoreClient = true
	}

	//禁用某个channel必须使用单独的coreClient,避免被污染
	if c.disablePullChannel || c.disablePushChannel {
		msg += "|option disable channel|"
		needNewCoreClient = true
	}

	if needNewCoreClient && c.newCoreClientName == "" {
		msg += ". need newCoreClient, but not set WithClientNewCoreClientName option"
		return fmt.Errorf(msg)
	}

	return nil
}

func WithClientKeyCallback(cb KeyCallback) ClientOption {
	return func(o *clientOption) {
		o.keyCallback = cb
	}
}

// Path callback
func WithClientPathCallback(cb PathCallback) ClientOption {
	return func(o *clientOption) {
		o.pathCallback = cb
	}
}

// Init timeout
func WithClientInitTimeout(t time.Duration) ClientOption {
	return func(o *clientOption) {
		o.initTimeout = t
	}
}

// Disable long connection or not after initation
func WithClientDisableListen(listen bool) ClientOption {
	return func(o *clientOption) {
		o.disableListen = listen
	}
}

// Support watch nonexistd config or not
func WithClientWatchEmpty(empty bool) ClientOption {
	return func(o *clientOption) {
		o.enableEmpty = empty
	}
}

// Fix duplicte watch issue for key&path in different module.
// But may have performance issue.
func WithClientNewCoreClient(new bool) ClientOption {
	return func(o *clientOption) {
		o.newCoreClient = new
	}
}

// 如果仅是避免重复监听，使用WithClientNewCoreClient。
// 这个option是为了能够设置一些core client的option
func WithClientNewCoreClientName(name string) ClientOption {
	return func(o *clientOption) {
		o.newCoreClientName = name
	}
}

// 必须跟WithClientNewCoreClientName配合使用。避免defaultCoreClient连接到指定的集群，干扰其他使用方
func WithClientCluster(cluster string) ClientOption {
	return func(o *clientOption) {
		o.pushChannelCluster = cluster
		o.pullChannelCluster = cluster
	}
}

// Model map to deserialize data
// schemaName -> model
func WithClientModelMap(m map[string]interface{}) ClientOption {
	return func(o *clientOption) {
		o.modelMap = m
	}
}

func WithClientDisableAuth() ClientOption {
	return func(o *clientOption) {
		o.disableAuth = true
	}
}

// WithClientIdentifier name must not empty
func WithClientIdentifier(name string) ClientOption {
	return func(o *clientOption) {
		if name == "" {
			name = "-"
		}
		o.identifier = name
	}
}

func WithClientTccSdkVersion(v string) ClientOption {
	return func(o *clientOption) {
		if v == "" {
			v = "-"
		}
		o.tccSdkVersion = v
	}
}

// 禁用定时拉取配置渠道。官方测试专用，业务方不能使用否则降低可用性
func DisablePullChannel() ClientOption {
	return func(o *clientOption) {
		o.disablePullChannel = true
	}
}

// 禁用推配置渠道。官方测试专用，业务方不能使用否则降低可用性
func DisablePushChannel() ClientOption {
	return func(o *clientOption) {
		o.disablePushChannel = true
	}
}

func transferConfClientOptionToCoreClientOption(c clientOption) (coreClientOpt []model.ClientOption) {
	if c.disablePullChannel {
		coreClientOpt = append(coreClientOpt, model.DisablePullChannel())
	}
	if c.disablePushChannel {
		coreClientOpt = append(coreClientOpt, model.DisablePushChannel())
	}
	if c.pushChannelCluster != "" {
		coreClientOpt = append(coreClientOpt, model.ClientWithRemoteCluster(c.pushChannelCluster))
	}
	if c.pullChannelCluster != "" {
		coreClientOpt = append(coreClientOpt, model.WithPullChannelRemoteCluster(c.pullChannelCluster))
	}

	if c.SmallFileDownloadNum != 0 {
		coreClientOpt = append(coreClientOpt, model.WithDownloadSmallFileDownloadNum(c.SmallFileDownloadNum))
	}

	if c.BigFileDownloadNum != 0 {
		coreClientOpt = append(coreClientOpt, model.WithDownloadBigFileDownloadNum(c.BigFileDownloadNum))
	}

	return
}

func WithDownloadSmallFileDownloadNum(num int) ClientOption {
	return func(options *clientOption) {
		if num <= 1 {
			num = 1
		}
		options.SmallFileDownloadNum = num
	}
}

func WithDownloadBigFileDownloadNum(num int) ClientOption {
	return func(options *clientOption) {
		if num <= 1 {
			num = 1
		}
		options.BigFileDownloadNum = num
	}
}
