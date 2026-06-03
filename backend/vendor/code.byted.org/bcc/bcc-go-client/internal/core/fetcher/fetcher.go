package fetcher

import (
	"reflect"
	"time"

	"code.byted.org/bcc/bcc-go-client/coreclient/model"
	"code.byted.org/bcc/bcc-go-client/internal/core/common"
	"code.byted.org/bcc/bcc-go-client/logger"
)

type Option struct {
	FetchInterval time.Duration
}
type Builder struct {
}

func (b Builder) Build(eventHandler common.MsgHandler, sdkOpts *model.SdkOptions) common.Fetcher {
	return NewFetcher(eventHandler, sdkOpts)
}

type Fetcher struct {
	keySet        map[string]*keyStat
	PathSet       map[string]*pathStat
	mgr           common.MsgHandler
	fetchInterval time.Duration
	worker        *worker
}

func NewFetcher(mgr common.MsgHandler, sdkOpts *model.SdkOptions) *Fetcher {
	if sdkOpts.PullChannelOptions.Disable {
		logger.Info("disable fetcher")
		return nil
	}
	clientOpts := fetchOptions{
		timeout:     sdkOpts.PullChannelOptions.Timeout,
		psm:         sdkOpts.PullChannelOptions.RemotePSM,
		addr:        sdkOpts.PullChannelOptions.RemoteAddr,
		cluster:     sdkOpts.PullChannelOptions.RemoteCluster,
		SdkLang:     sdkOpts.SDKLang,
		SdkPath:     sdkOpts.SDKPath,
		Tags:        sdkOpts.Tags,
		DisableAuth: sdkOpts.DisableAuth,
	}
	f := &Fetcher{
		keySet:        make(map[string]*keyStat),
		PathSet:       make(map[string]*pathStat),
		mgr:           mgr,
		fetchInterval: sdkOpts.PullChannelOptions.Interval,
		worker:        newWorker(mgr, clientOpts),
	}
	f.worker.run()
	return f
}
func (f *Fetcher) OnWatch(msg *common.OnWatchMsg) {
	if f == nil {
		return
	}
	f.onWatchPath(msg.Path)
	f.onWatchKeys(msg.Keys)
}

func (f *Fetcher) OnUpdate(msg common.OnUpdateMsg) {
	if f == nil {
		return
	}
	switch m := msg.(type) {
	case *common.OnUpdatePathMsg:
		f.onUpdatePath(m)
	case *common.OnUpdateKeyMsg:
		f.onUpdateKey(m)
	default:
		logger.Error("unknown msg type:[%s]", reflect.TypeOf(msg))
	}

}

func (f *Fetcher) NotifyFetchUpdate() {
	pathList := make([]pathStat, 0)
	keyList := make([]keyStat, 0)
	for _, p := range f.PathSet {
		if p.needFetch(f.fetchInterval) {
			pathList = append(pathList, *p)
		}
	}
	for _, k := range f.keySet {
		if k.needFetch(f.fetchInterval) {
			keyList = append(keyList, *k)
		}
	}
	if len(pathList) == 0 && len(keyList) == 0 {
		return
	}
	fetchReq := newFetchRequest(pathList, keyList)

	f.worker.AddPullConfTask(fetchReq)
}
func newFetchRequest(pathList []pathStat, keyList []keyStat) fetchRequest {
	return fetchRequest{
		pathList: pathList,
		keyList:  keyList,
	}
}

func (f *Fetcher) Close() {
	if f == nil {
		return
	}

	f.worker.close()
}

func (f *Fetcher) onWatchPath(path string) {
	if path == "" {
		return
	}
	f.PathSet[path] = NewPathStat(path)
}

func (f *Fetcher) onWatchKeys(keys []string) {
	for _, key := range keys {
		f.keySet[key] = NewKeyStat(key)
	}
}

func (f *Fetcher) onUpdatePath(m *common.OnUpdatePathMsg) {
	if m.Delete {
		delete(f.PathSet, m.Path)
		return
	}
	if p, ok := f.PathSet[m.Path]; ok {
		p.Update()
	} else {
		logger.Warn("path:[%s] update not exist should not exist", m.Path)
	}
}

func (f *Fetcher) onUpdateKey(m *common.OnUpdateKeyMsg) {
	if m.Delete {
		delete(f.keySet, m.Key)
		return
	}
	if k, ok := f.keySet[m.Key]; ok {
		k.Update(m.UpdateId)
	} else {
		logger.Warn("key:[%s] update not exist should not exist", m.Key)
	}
}
