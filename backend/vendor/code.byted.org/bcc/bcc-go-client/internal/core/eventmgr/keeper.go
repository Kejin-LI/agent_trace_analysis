package eventmgr

import (
	"strings"

	coreclientmodel "code.byted.org/bcc/bcc-go-client/coreclient/model"
	"code.byted.org/bcc/bcc-go-client/internal/core/common"
	internalerror "code.byted.org/bcc/bcc-go-client/internal/error"
	"code.byted.org/bcc/bcc-go-client/logger"
)

type ConfKeeper struct {
	watchingPath map[string]*PathInfo
	watchingKey  map[string]*KeyInfo
}

func newConfKeeper() *ConfKeeper {
	keeper := &ConfKeeper{
		watchingPath: make(map[string]*PathInfo),
		watchingKey:  make(map[string]*KeyInfo),
	}

	return keeper
}

func (c *ConfKeeper) checkPath(path string) internalerror.BaseError {
	if len(path) == 0 || path[0] == '/' { //服务端做完整判断
		logger.Error("checkPath invalid format Path=%v, Path can not empty or start with /", path)
		return internalerror.NewError(internalerror.ErrInvalidParam, `path "%s" is invalid, it must not empty and not start with "/"`, path)
	}
	//不能相同目录，不能父子目录
	for _, dir := range c.watchingPath {
		if path == dir.Path || strings.HasPrefix(path, dir.Path) || strings.HasPrefix(dir.Path, path) {
			logger.Error("onWatchPathMsg reuse Path=%v oldPath=%v", path, dir.Path)
			return internalerror.NewError(internalerror.ErrReuse, `path "%s" is already being watched. Watching the same path or a parent/child path is not allowed.`, path)
		}
	}
	return nil
}

func (c *ConfKeeper) AddWatchPath(task *common.WatchPathTask) (*PathInfo, internalerror.BaseError) {
	if err := c.checkPath(task.Path()); err != nil {
		return nil, err
	}

	pathInfo := NewPathInfo(task.Path(), task.Callback(), task.Option())

	c.addPathInfo(pathInfo)

	return pathInfo, nil
}

func (c *ConfKeeper) addPathInfo(pathInfo *PathInfo) {
	c.watchingPath[pathInfo.Path] = pathInfo
}

func (c *ConfKeeper) GetPathInfo(path string) *PathInfo {
	return c.watchingPath[path]
}
func (c *ConfKeeper) CancelWatchKey(key string) {
	delete(c.watchingKey, key) //取消监听 //todo 可能重连到其他server后会存在key？
}

func (c *ConfKeeper) GetAllStreamWatchPathTask() []*common.StreamWatchPathTask {
	pathTaskList := make([]*common.StreamWatchPathTask, 0, len(c.watchingPath))
	for _, dir := range c.watchingPath {
		pathTaskList = append(pathTaskList, c.GetStreamPathTask(dir.Path))
	}

	return pathTaskList
}
func (c *ConfKeeper) GetStreamPathTask(path string) *common.StreamWatchPathTask {

	pathInfo, ok := c.watchingPath[path]
	if !ok {
		//理论上不可能出现
		logger.Error("GetStreamPathTask path=%v not exist", path)
		return nil
	}

	return &common.StreamWatchPathTask{
		CltDir: pathInfo.getStreamPathInfo(),
	}
}
func (c *ConfKeeper) AddWatchKey(key string, opt *coreclientmodel.WatchOptions, callback coreclientmodel.KeyCallback) (*KeyInfo, internalerror.BaseError) {

	item := NewKeyInfo(key, callback, opt)
	if err := c.checkKey(item); err != nil {
		return nil, err
	}
	c.KeyUpdate(item)
	return item, nil
}
func (c *ConfKeeper) checkKey(item *KeyInfo) internalerror.BaseError {
	key := item.Name
	opt := item.Opt
	callback := item.callback
	//key至少2级
	if key == "" || strings.Count(key, "/") == 0 {
		logger.Error("add key invalid format key=%v", key)
		return internalerror.NewError(internalerror.ErrInvalidParam, `key "%s" is invalid, it must not empty and should be like "/some_dir/some_key"`, key)
	}
	//key不能用"/"结尾
	if key[len(key)-1] == '/' {
		logger.Error("add key invalid format key=%v", key)
		return internalerror.NewError(internalerror.ErrInvalidParam, `key "%s" is invalid, it must not end with "/"`, key)
	}
	//必须有回调函数
	if callback == nil {
		logger.Error("add key invalid callback key=%v", key)
		return internalerror.NewError(internalerror.ErrInvalidParam, `key "%s" must have callback`, key)
	}
	//参数冲突
	if opt.DisableListen && opt.EnableEmpty {
		logger.Error("add key invalid option key=%v", key)
		return internalerror.NewError(internalerror.ErrInvalidParam, `key "%s": "DisableListen" and "EnableEmpty" options cannot be enabled simultaneously`, key)
	}
	//重复监听
	if c.watchingKey[key] != nil {
		logger.Error("add key reuse key=%v", key)
		return internalerror.NewError(internalerror.ErrReuse, `key "%s" is already being watched. Duplicate key watching is not allowed.`, key)
	}
	return nil
}
func (c *ConfKeeper) KeyUpdate(item *KeyInfo) {
	c.watchingKey[item.Name] = item
}
func (c *ConfKeeper) GetAllStreamWatchKeyTask() *common.StreamWatchKeyTask {
	keyTask := common.NewStreamWatchKeyTask()
	//监听item
	for _, v := range c.watchingKey {
		keyTask.Items[v.Name] = v.genCltItem()
	}
	return keyTask
}

// 取消监听时检查key是否存在
// true: key存在； false: key不存在
func (c *ConfKeeper) CancelKey(key string) bool {
	if _, ok := c.watchingKey[key]; !ok {
		return false
	}
	delete(c.watchingKey, key)
	return true
}
func (c *ConfKeeper) GetPath(path string) *PathInfo {
	return c.watchingPath[path]
}

func (c *ConfKeeper) CancelPath(path string) {
	delete(c.watchingPath, path)
}

func (c *ConfKeeper) GetKey(key string) *KeyInfo {
	return c.watchingKey[key]
}

func (c *ConfKeeper) PathUpdate(item *KeyInfo) {
	c.watchingPath[item.Dir.Path].Items[item.Name] = item
}

func (c *ConfKeeper) GetKeyInfo(key string) (res []*KeyInfo) {

	if k, exist := c.watchingKey[key]; exist {
		res = append(res, k)
	}
	keyname := strings.TrimRight(key, "/")

	strArr := strings.Split(keyname, "/")
	path := ""

	for i := 0; i < len(strArr)-1; i++ { //最后一层不是目录
		path += strArr[i] + "/"
		if pathInfo, exist := c.watchingPath[path]; exist {
			if k, kExist := pathInfo.Items[key]; kExist {
				res = append(res, k)
				break
			}
		}
	}
	return
}
