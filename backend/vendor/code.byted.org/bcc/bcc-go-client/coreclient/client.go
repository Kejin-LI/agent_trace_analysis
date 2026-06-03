package coreclient

import (
	"fmt"
	"os"
	"sync"
	"time"

	"code.byted.org/bcc/tools"

	"code.byted.org/bcc/bcc-go-client/coreclient/debug"
	"code.byted.org/bcc/bcc-go-client/coreclient/model"
	"code.byted.org/bcc/bcc-go-client/internal/core"
	"code.byted.org/bcc/bcc-go-client/internal/core/common"
	"code.byted.org/bcc/bcc-go-client/internal/core/pb"
	internalerror "code.byted.org/bcc/bcc-go-client/internal/error"
	"code.byted.org/bcc/bcc-go-client/internal/util"
	"code.byted.org/bcc/bcc-go-client/logger"
)

var _ model.Client = (*client)(nil)
var _ debug.Debugger = (*client)(nil)

type client struct {
	opts *model.SdkOptions
	mgr  common.EventMgr
	name string
}

var (
	defaultClient    model.Client
	defaultClientErr error
	clientMu         sync.RWMutex
	gClientID        = 0
	gClientMap       = make(map[string]model.Client)
)
var disableSidecar = isDisableSidecar()

func isDisableSidecar() bool {
	return os.Getenv("DISABLE_BCC_SIDECAR") == "true"
}

func DisableSidecar() {
	disableSidecar = true
}

const _DEFAULT_NAME = "default"

var getDefaultOnce sync.Once

func getDefaultClient() (model.Client, error) {
	getDefaultOnce.Do(func() {
		defaultClient, defaultClientErr = NewClient("default")
	})
	return defaultClient, defaultClientErr
}
func WatchKey(key string, callback model.KeyCallback, opts ...model.WatchOption) error {
	if cli, err := getDefaultClient(); err != nil {
		return err
	} else {
		return cli.WatchKey(key, callback, opts...)
	}
}

func WatchKeys(keys []string, callback model.KeyCallback, opts ...model.WatchOption) error {
	if cli, err := getDefaultClient(); err != nil {
		return err
	} else {
		return cli.WatchKeys(keys, callback, opts...)
	}
}
func WatchPath(path string, callback model.PathCallback, opts ...model.WatchOption) error {
	if cli, err := getDefaultClient(); err != nil {
		return err
	} else {
		return cli.WatchPath(path, callback, opts...)
	}
}

// CancelKey 取消监听 key ，必须保证是调用 WatchKey 之后调用，否则是 Undefined Behavior
func CancelKey(key string, opt ...model.CancelOption) {
	if cli, err := getDefaultClient(); err == nil {
		cli.CancelKey(key, opt...)
	}
}

// CancelKeys 取消监听 keys ，必须保证是调用 WatchKeys 之后调用，否则是 Undefined Behavior
func CancelKeys(keys []string, opt ...model.CancelOption) {
	if cli, err := getDefaultClient(); err == nil {
		cli.CancelKeys(keys, opt...)
	}
}

// CancelPath 取消监听 path，必须保证是调用 WatchPath 之后调用，否则是 Undefined Behavior
func CancelPath(path string, opt ...model.CancelOption) {
	if cli, err := getDefaultClient(); err == nil {
		cli.CancelPath(path, opt...)
	}
}

func NewClient(name string, opt ...model.ClientOption) (model.Client, error) {
	if err := util.CheckFileCache(); err != nil {
		return nil, err
	}
	return getClient(name, opt...), nil
}
func getClient(name string, opts ...model.ClientOption) model.Client {
	if name == "" {
		clientMu.Lock()
		gClientID++
		name = fmt.Sprintf("anonymous%v", gClientID)
		clientMu.Unlock()
	}

	clientMu.RLock()
	c := gClientMap[name]
	clientMu.RUnlock()

	if c == nil {
		clientMu.Lock()
		c = gClientMap[name]
		if c == nil {
			c = newClient(name, opts...)

			gClientMap[name] = c
			if name != _DEFAULT_NAME {
				logger.Notice("new client name=%v", name)
			}
		}

		clientMu.Unlock()
	}

	return c
}

func newClient(name string, opt ...model.ClientOption) model.Client {
	opts := model.GetClientOption(opt...)
	return &client{
		opts: opts,
		mgr:  core.NewEventMgr(name, opts),
		name: name,
	}
}

func (c *client) Name() string {
	return c.name
}

func (c *client) WatchKey(key string, callback model.KeyCallback, opts ...model.WatchOption) error {

	return c.WatchKeys([]string{key}, callback, opts...)
}

func (c *client) WatchKeys(keys []string, callback model.KeyCallback, opts ...model.WatchOption) error {

	opt := model.GetWatchOption(opts...)
	task := common.NewWatchKeysTask(opt.Ctx, callback, keys, opt)
	if err := c.mgr.AddWatchKeysTask(task); err != nil {
		return err
	}
	res := task.GetResult()
	c.handleItemValueErr(res)

	return res.Err

}

func (c *client) WatchPath(path string, callback model.PathCallback, opts ...model.WatchOption) error {
	opt := model.GetWatchOption(opts...)
	logger.Debug("watch path:%v watch opt:%v", path, tools.ToJsonStringer(opt))
	task := common.NewWatchPathTask(opt.Ctx, callback, path, opt)
	logger.Debug("new path task, begin add path task")
	if err := c.mgr.AddWatchPathTask(task); err != nil {
		return err
	}
	res := task.GetResult()
	c.handleItemValueErr(res)

	return res.Err
}

func (c *client) handleItemValueErr(res common.WatchReplyResult) {
	if res.Err != nil {
		//这个item错误一般是因为svr读取存储引擎失败导致的。这种失败发生会高并发向存储引擎读取较大的大文件才会出现。属于非常低频的可能。一般业务方重试即可成功
		//这里sleep是为了防止业务方碰到这种情况还无脑重试加剧存储引擎的读压力。一般来说，业务方碰到watch失败是会panic或者其他处理，不会无限重试的。而且
		//业务方watch的时候一般都是会阻塞的，因此这500ms的sleep影响不大。
		if err := res.Err.(*internalerror.MultiError); err.IsItemErr() {
			time.Sleep(500 * time.Millisecond)
		}
	}
}

func (c *client) CancelKey(key string, opt ...model.CancelOption) {
	c.CancelKeys([]string{key}, opt...)
}

func (c *client) CancelKeys(keys []string, opt ...model.CancelOption) {
	if len(keys) == 0 {
		return
	}
	opts := model.GetCancelOptions(opt...)
	task := common.NewCancelKeysTask(opts.Ctx, keys)
	c.mgr.AddCancelKeysTask(task)
	task.Wait()
}

func (c *client) CancelPath(path string, opt ...model.CancelOption) {
	opts := model.GetCancelOptions(opt...)
	task := common.NewCancelPathTask(opts.Ctx, path)
	c.mgr.AddCancelPathTask(task)
	task.Wait()
}

func (c *client) Close() {
	c.mgr.Close()
}

// EnvParam implements debug.Debugger.
func (c *client) EnvParam() *pb.EnvParam {
	registerInfo := common.RegisterInfo{
		SdkPath:     c.opts.SDKPath,
		SdkLang:     c.opts.SDKLang,
		ClientName:  c.name,
		Tags:        c.opts.Tags,
		DisableAuth: c.opts.DisableAuth,
	}
	return registerInfo.BuildEnvParam()
}
