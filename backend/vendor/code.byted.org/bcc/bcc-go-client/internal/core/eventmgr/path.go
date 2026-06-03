package eventmgr

import (
	"time"

	"code.byted.org/bcc/bcc-go-client/coreclient/model"
	"code.byted.org/bcc/bcc-go-client/internal/core/common"
	"code.byted.org/bcc/bcc-go-client/logger"
	"code.byted.org/bcc/tools"
)

// PathInfo 数据必须与 model.SnapshotPath 对齐
type PathInfo struct {
	Path      string              `json:"path"`       //【input】
	Opt       *model.WatchOptions `json:"opt"`        //【input】
	WatchTime time.Time           `json:"watch_time"` //监听时间
	Items     map[string]*KeyInfo `json:"items"`      //文件列表
	CallBack  model.PathCallback  `msgpack:"-"`
	Stat      *pathStat           `json:"stat"`
}
type pathStat struct {
	FirstTime  int                 `json:"first_time"` //首次监听，大于0标识首批元信息已下发，标识下发完成时间
	FirstTotal int64               //首次监听，数量
	FirstItems map[string]*KeyInfo //首次监听，未完成列表
}

func NewPathInfo(path string, cb model.PathCallback, opt *model.WatchOptions) *PathInfo {
	pathInfo := &PathInfo{
		Path:      path,
		Opt:       opt,
		WatchTime: time.Now(),
		Items:     make(map[string]*KeyInfo),
		CallBack:  cb,
		Stat: &pathStat{
			FirstTime:  0,
			FirstTotal: 0,
			FirstItems: make(map[string]*KeyInfo),
		},
	}
	return pathInfo
}

// NewPathInfoWithSnapshot sidecar中，client侧不会使用，server侧状态恢复使用
func NewPathInfoWithSnapshot(snapshot []byte, cb model.PathCallback) *PathInfo {
	pathInfo := &PathInfo{}
	err := tools.MsgPackUnmarshal(snapshot, pathInfo)
	if err != nil {
		logger.Fatal("impossible pathInfo unmarshal error path:%v snapshot err=%v", pathInfo.Path, err)
		return pathInfo
	}
	pathInfo.CallBack = cb
	pathInfo.Stat.FirstItems = make(map[string]*KeyInfo)

	for _, item := range pathInfo.Items {
		item.Dir = pathInfo //这个Dir在快照的时候，没有被序列化。此时要重新赋值。
		item.downRecord = NewDownloadRecord(item.Name, item.Ver)
	}

	return pathInfo
}

// 包括item的内容
func (p *PathInfo) Snapshot() []byte {
	r, err := tools.MsgPackMarshal(p)
	if err != nil {
		logger.Fatal("impossible pathInfo path:%v snapshot err=%v", p.Path, err)
		return nil
	}
	return r
}

func (p *PathInfo) getStreamPathInfo() *common.CltDir {
	opt := p.Opt
	dir := &common.CltDir{
		Path:         p.Path,
		EnableListen: !opt.DisableListen,
		FirstTime:    int64(p.Stat.FirstTime),
		Items:        make(map[string]*common.CltItem, len(p.Items)),
	}
	for _, item := range p.Items {
		dir.Items[item.Name] = item.genCltItem()
	}
	return dir
}

func (p *PathInfo) totalSize() int64 {
	r := int64(0)
	for _, item := range p.Items {
		r += item.ValueSize
	}
	return r
}

func (p *PathInfo) itemDelete(item *KeyInfo) {
	delete(p.Items, item.Name)
}

func (p *PathInfo) GetItem(key string) *KeyInfo {
	return p.Items[key]
}

func (p *PathInfo) ItemFirstUpdate(svrItem *common.ServerItem) *KeyInfo {
	if !svrItem.Valid() { // 不应该出现
		return nil
	}
	//todo item第一次更新的初始化

	item := NewKeyInfoFromPath(svrItem.Key(), p, p.Opt)

	p.Items[svrItem.Key()] = item

	//外部等待，记录第一次监听的key
	if !p.IsFinish() {
		p.Stat.FirstItems[item.Name] = item
	}

	return item
}

func (p *PathInfo) pathFinish(task common.FinishPathMsg) {
	failMsg := task.FailMsg

	//重连后出现严重错误
	if p.Stat.FirstTime > 0 && failMsg != "" {
		logger.Error("finishDir path=%v failMsg=%v", p.Path, failMsg)
	}
	if p.Stat.FirstTime == 0 || !p.Opt.DisableListen {
		p.Stat.FirstTime = int(time.Now().Unix())
		p.Stat.FirstTotal = task.Total
	}
}
func (p *PathInfo) IsFinish() bool {
	return p.Stat.FirstTime != 0
}
func (p *PathInfo) IsItemsFinish() bool {
	return len(p.Stat.FirstItems) == 0
}

func (p *PathInfo) getFirstItems() map[string]*KeyInfo {
	return p.Stat.FirstItems
}

func (p *PathInfo) ItemFinish(item *KeyInfo) {
	delete(p.Stat.FirstItems, item.Name)
}

// 目录级别错误，清空firstitems
func (p *PathInfo) PathError() {
	p.Stat.FirstItems = make(map[string]*KeyInfo)
}
