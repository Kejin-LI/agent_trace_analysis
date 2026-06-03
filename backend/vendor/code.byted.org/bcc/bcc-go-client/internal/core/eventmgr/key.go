package eventmgr

import (
	"fmt"
	"time"

	cmodel "code.byted.org/bcc/bcc-go-client/coreclient/model"
	"code.byted.org/bcc/bcc-go-client/internal/core/common"
	internalerror "code.byted.org/bcc/bcc-go-client/internal/error"
	"code.byted.org/bcc/bcc-go-client/internal/util"
	"code.byted.org/bcc/bcc-go-client/logger"
	"code.byted.org/bcc/tools"
)

type cancelType string

const (
	outsideCancel cancelType = "outside"
	insideCancel  cancelType = "inside"
)

var _EMPTY_VALUE = []byte("[clear]")                         //常量
var _EMPTY_VALUEs = []*cmodel.Content{{Value: _EMPTY_VALUE}} //常量

var _ cmodel.Item = (*KeyInfo)(nil)

// todo 分拆结构
type KeyInfo struct {
	Name            string               `json:"name"` //【input】Name
	KeyPath         string               `msgpack:"key_path"`
	KeyUpdateTime   int64                `json:"key_update_time"` //【output】配置更新时间，admin的更新时间。纳秒
	Opt             *cmodel.WatchOptions `json:"opt"`             //【input】监听参数
	Val             []byte               `json:"-" msgpack:"-"`   //sidecar snapshot 不需要序列化传输value                 //【output】数据
	Vals            []*cmodel.Content    //【output】数据
	GrayRule        string               `json:"sdk_gray_rule"`     //【output】流量灰度规则
	Md5             string               `json:"md5"`               //【output】数据md5
	ValueSize       int64                `json:"value_size"`        //【output】数据长度（不会被clear）
	Ver             int64                `json:"version"`           //【output】局部版本号
	BKFile          string               `json:"backup_file"`       //【output】备份文件
	Valid           bool                 `json:"valid"`             //【output】是否有效
	LocalUpdateTime time.Time            `json:"local_update_time"` //【output】本地更新时间
	IsInline        bool                 `json:"is_inline"`         //是否inline
	Dir             *PathInfo            `json:"-" msgpack:"-"`     //（目录监听） 序列化不会把dir的信息序列化，避免循环依赖，必须调用 PathSnapshot 才会序列化 Path的数据
	UID             int64                `json:"update_id"`         //【output】全局版本号
	downRecord      *downloadRecord      //
	callback        cmodel.KeyCallback   `msgpack:"-"` //目录监听为nil
	CallbackSuccess bool                 `json:"callback_success"`
	oldItem         *KeyInfo             `msgpack:"-"` //
	nextItem        *common.ServerItem   `msgpack:"-"` //
	Serializer      cmodel.SerializationType
}

func NewKeyInfo(keyname string, cb cmodel.KeyCallback, opt *cmodel.WatchOptions) *KeyInfo {
	info := &KeyInfo{
		Name:       keyname,
		Opt:        opt,
		callback:   cb,
		downRecord: NewDownloadRecord(keyname, 0),
	}

	return info
}
func NewKeyInfoFromPath(key string, p *PathInfo, opt *cmodel.WatchOptions) *KeyInfo {
	info := &KeyInfo{
		Name:       key,
		KeyPath:    p.Path,
		Opt:        opt,
		Valid:      true,
		Dir:        p,
		downRecord: NewDownloadRecord(key, 0),
	}
	return info
}

// NewKeyInfoWithSnapshot sidecar中，client与server侧都会使用
func NewKeyInfoWithSnapshot(snapshot []byte, cb cmodel.KeyCallback) *KeyInfo {
	k := &KeyInfo{}
	err := tools.MsgPackUnmarshal(snapshot, k)
	if err != nil {
		logger.Error("unmarshal snapshot failed, key:%s, err:%s origin data:%v", k.Name, err.Error(), snapshot)
		return nil
	}
	k.downRecord = NewDownloadRecord(k.Name, k.Ver) // 不需要恢复snapshot，曾经已经report过，如果有错误让server重新下发，会重新更新downRecord
	k.callback = cb

	return k
}

// 创建新的item，避免业务持有
func (k *KeyInfo) NewItem(svrItem *common.ServerItem, backupFile string) *KeyInfo {
	nk := &KeyInfo{
		Name:            k.Name,
		KeyPath:         k.KeyPath,
		KeyUpdateTime:   svrItem.UpdateTime(),
		Opt:             k.Opt,
		Val:             k.Val,
		GrayRule:        svrItem.SDKGrayRule(),
		Serializer:      cmodel.SerializationType(svrItem.Serialization()),
		Vals:            k.Vals,
		Md5:             svrItem.Md5(),
		ValueSize:       svrItem.Size(),
		Ver:             svrItem.Version(),
		BKFile:          backupFile,
		Valid:           svrItem.Valid(),
		LocalUpdateTime: time.Now(),
		IsInline:        k.downRecord.target.Source == common.DOWNLOAD_SOURCE_INLINE,
		Dir:             k.Dir,
		UID:             svrItem.UpdateID(),
		downRecord:      k.downRecord,
		callback:        k.callback,
		CallbackSuccess: false,
		oldItem:         k,
		nextItem:        nil,
	}
	k.oldItem = nil // 释放旧版本数据 stn
	return nk
}
func (k *KeyInfo) NewItemWithValue(svrItem *common.ServerItem, value []byte, backupFile string) *KeyInfo {
	nk := k.NewItem(svrItem, backupFile)
	nk.Val = value
	return nk
}

func (k *KeyInfo) KeySnapshot() []byte {
	res, err := tools.MsgPackMarshal(k)
	if err != nil {
		logger.Error("should not happen KeySnapshot marshal err:%v", err)
		return nil
	}
	return res
}

// 只包括 path 的元数据，不包括 items 的元数据
func (k *KeyInfo) PathSnapshot() []byte {
	if k.Dir == nil {
		return nil
	}
	return k.Dir.Snapshot()
}

func (k *KeyInfo) NewItemWitResult(result *common.LoaderResult) *KeyInfo {
	nk := k.NewItem(result.SvrItem, result.BackupFile)
	nk.downRecord.target.Result = result.Result
	nk.downRecord.target.FailMsg = result.FailMsg
	return nk
}
func (k *KeyInfo) Key() string {
	return k.Name
}

func (k *KeyInfo) Value() []byte {
	return k.Val
}

func (k *KeyInfo) Serialization() cmodel.SerializationType {
	return k.Serializer
}

func (k *KeyInfo) Values() []*cmodel.Content {
	return k.Vals
}

func (k *KeyInfo) SDKGrayRule() string {
	return k.GrayRule
}

func (k *KeyInfo) Version() int64 {
	return k.Ver
}
func (k *KeyInfo) UpdateID() int64 {
	return k.UID
}

func (k *KeyInfo) Clear() {
	k.Val = _EMPTY_VALUE
	k.Vals = _EMPTY_VALUEs
}

func (k *KeyInfo) BackupFile() string {
	return k.BKFile
}

func (k *KeyInfo) Path() string {
	return k.KeyPath
}

func (k *KeyInfo) empty() bool {
	return len(k.Val) == 0 && len(k.Values()) == 0
}

func (k *KeyInfo) source() common.DownloadItemSource {
	return k.downRecord.target.Source
}
func (k *KeyInfo) result() common.DownloadItemResult {
	return k.downRecord.target.Result
}

// AllowUpdate 分别判断监听key，监听目录，两种方式的允许监听规则
func (k *KeyInfo) AllowUpdate(svrItem *common.ServerItem) bool {
	localUpdateId := k.UID
	//可能收到老版本，服务端会控制，不应该出现
	if !svrItem.IsNewer(localUpdateId) {
		logger.Debug("updateOne older key=%v localUpdateID=%v recvUpdateID=%v", k.Name, localUpdateId, svrItem.UpdateID())
		return false
	}

	// 监听成功后，非首次的auth fail进行错误提示，不进行回调通知。
	if svrItem.IsAuthFail() {
		if k.isReady() || k.IsDownloading() {
			logger.Error("updateOne %v key=%v", internalerror.ErrAuthFailedMsg, k.Name)
			return false
		}
	}

	// key监听成功后，不允许删除
	if svrItem.IsDelete() {
		if k.isItem() && (k.isReady() || k.IsDownloading()) {
			logger.Warn("updateOne cannot delete key=%v", k.Name)
			return false
		}
	}
	return true
}

func (k *KeyInfo) isItem() bool {
	return k.Dir == nil
}
func (k *KeyInfo) isReady() bool {
	return !k.empty()
}
func (k *KeyInfo) valueError(result common.DownloadItemResult, err error) {
	r := k.downRecord
	r.valueError(result, err)
	k.Val = nil //todo 会不会影响empty的判断？
	k.Vals = nil
}

func (k *KeyInfo) getError() internalerror.BaseError {
	//todo 有可能为nil
	return common.GetResultError(k.downRecord.target.Result)
}

func (k *KeyInfo) callbackError(result common.DownloadItemResult, err error) {
	k.downRecord.callbackErr(result, err)

	k.Val = nil
	k.Vals = nil
	k.CallbackSuccess = false
	util.EmitError(k.Key(), "callback.add")
}
func (k *KeyInfo) getCallbackKey() string {
	if k.isItem() {
		return k.Name
	} else {
		return k.Dir.Path + "@" + k.Name
	}
}

func (k *KeyInfo) successCallback() {
	k.CallbackSuccess = true
}

func (k *KeyInfo) isCallbackSuccess() bool {
	return k.CallbackSuccess
}

func (k *KeyInfo) NeedCallbackRetry() bool {
	return !k.isCallbackSuccess() && !k.IsNextDownloading() && k.downRecord.target.Result == common.DOWNLOAD_RESULT_CALLBACK_RETRY
}

func (k *KeyInfo) reportCallback(needReport bool, dur time.Duration) {
	if needReport {
		if k.isItem() {
			logger.Error("core watchKey fail key=%v version=%v updateId=%v result=%v failCount=%v failMsg=%v",
				k.Name, k.Version(), k.UpdateID, k.downRecord.target.Result.String(), k.downRecord.target.FailCount, k.downRecord.target.FailMsg)
		} else {
			logger.Error("core watchPath fail key=%v version=%v updateId=%v result=%v failCount=%v failMsg=%v",
				k.Name, k.Version(), k.UpdateID, k.downRecord.target.Result.String(), k.downRecord.target.FailCount, k.downRecord.target.FailMsg)
		}
	} else { //非错误
		if k.isItem() {
			logger.Info("core watchKey wait key=%v version=%v updateId=%v result=%v count=%v try again after %v",
				k.Name, k.Version(), k.UpdateID, k.downRecord.target.Result.String(), k.downRecord.target.FailCount, k.downRecord.target.FailMsg, dur)
		} else {
			logger.Info("core watchPath wait key=%v version=%v updateId=%v result=%v count=%v try again after %v",
				k.Name, k.Version(), k.UpdateID, k.downRecord.target.Result.String(), k.downRecord.target.FailCount, k.downRecord.target.FailMsg, dur)
		}
	}
}

func (k *KeyInfo) IsDownloading() bool {
	return k.downRecord.isDownloading
}

func (k *KeyInfo) IsNextDownloading() bool {
	return k.nextItem != nil
}

func (k *KeyInfo) genCltItem() (cltItem *common.CltItem) {
	if k.Valid {
		cltItem = &common.CltItem{
			Key:        k.Name,
			Version:    k.Ver,
			UpdateID:   k.UpdateID(),
			Md5:        k.Md5,
			UpdateTime: k.LocalUpdateTime.Unix(),
			Valid:      k.Valid,
		}
		if k.downRecord != nil {
			cltItem.Target = k.downRecord.target
		}
	} else {
		cltItem = &common.CltItem{
			Key:   k.Name,
			Valid: k.Valid,
		}
	}
	// 正常或者异常都需要统一赋予sdk参数，push对此进行相应处理
	cltItem.EnableListen = !k.Opt.DisableListen
	cltItem.EnableEmpty = k.Opt.EnableEmpty
	return
}

func (k *KeyInfo) MustWriteDisk() bool {
	return !k.empty() && k.Opt.DisableMemory
}

// 只有小文件才会走这里
// todo 重启时读取
func (k *KeyInfo) WriteDisk(svrItem *common.ServerItem, fileCache *util.FileCache) string {
	// TODO: 多文件协议时的磁盘写入
	backupFile := ""
	//写磁盘
	if k.MustWriteDisk() {
		backupFile = fileCache.GenName(svrItem.Key(), svrItem.Version(), svrItem.Md5())
		err := fileCache.TryWrite(backupFile, k.Val)
		if err != nil {
			k.valueError(common.DOWNLOAD_RESULT_WRITEFILE, fmt.Errorf("write inline file fail name=%v err=%v", backupFile, err))
		} else {
			//DisableMemory回调，防止业务方误用，因此设置为空值
			k.Val = _EMPTY_VALUE
			k.Vals = _EMPTY_VALUEs
		}
	}
	return backupFile
}

func (k *KeyInfo) deletePathItem() {
	dir := k.Dir
	dir.itemDelete(k)
}

func (k *KeyInfo) valueUpdate(val []byte) {
	k.Val = val
}

func (k *KeyInfo) setValues(values []*cmodel.Content) {
	k.Vals = values
}

func (k *KeyInfo) deleteOldBackupFile(cache *util.FileCache) {
	oldItem := k.oldItem
	//每个item只保留最新的版本
	if (!oldItem.IsInline || oldItem.Opt.DisableMemory || oldItem.Opt.BigFileDisableMemory) && oldItem.Ver != 0 && oldItem.Ver != k.Ver {
		cache.TryRemove(oldItem.BKFile) //io可能阻塞
	}
}

func (k *KeyInfo) pathCallbackSuccess() {
	dir := k.Dir
	dir.Items[k.Name] = k
	dir.ItemFinish(k)
}

// 检查大文件下载后要更新的item与key准备要更新的item是否一致
func (k *KeyInfo) isOlderSvrItem(svrItem *common.ServerItem) bool {
	return k.nextItem == nil || k.nextItem.IsNewer(svrItem.UpdateID())
}

func (k *KeyInfo) finishDownload(result *common.LoaderResult) {
	k.downRecord.onDownloadUpdate(result)
	k.downRecord.isDownloading = false
	k.nextItem = nil
}

func (k *KeyInfo) OnAddDownloadTask() {
	k.downRecord.isDownloading = true
}

// 是否是否需要下载和回调，如果不需要，代表只是updateID更新，不需要重复下载和回调
func (k *KeyInfo) needCallback(svrItem *common.ServerItem) bool {
	localUpdateID := k.UpdateID()
	localMD5 := k.Md5
	localVersion := k.Version()
	svrUpdateID := svrItem.UpdateID()
	svrMD5 := svrItem.Md5()
	svrVersion := svrItem.Version()
	if svrUpdateID != 0 && svrUpdateID <= localUpdateID {
		//不应该出现，前面已经比较好
		logger.Info("svr updateID <= local updateID key=%v localUpdateID=%v svrUpdateID=%v", k.Name, localUpdateID, svrUpdateID)
		return false
	}
	if svrVersion != localVersion {
		return true
	}
	if svrMD5 != localMD5 {
		return true
	}

	if svrVersion == localVersion && svrMD5 == localMD5 {
		return false
	}

	return true

}

// 当判断不需要回调，只变更了updateID的情况下，更新相关元信息
func (k *KeyInfo) updateMeta(item *common.ServerItem) {
	k.UID = item.UpdateID()
	k.LocalUpdateTime = time.Unix(0, item.UpdateTime())
}

//----------------------------------------------------------------------

type downloadRecord struct {
	target        *common.DownloadItemInfo
	isDownloading bool
}

func NewDownloadRecord(keyname string, version int64) *downloadRecord {
	record := &downloadRecord{
		target: &common.DownloadItemInfo{
			Key:        keyname,
			Version:    version,
			CreateTime: time.Now().Unix(),
			UpdateTime: time.Now().Unix(),
		},
		isDownloading: false,
	}

	return record
}

func (r *downloadRecord) inlineOk() {
	target := r.target
	target.Source = common.DOWNLOAD_SOURCE_INLINE
	target.Result = common.DOWNLOAD_RESULT_OK
}

func (r *downloadRecord) onDownloadUpdate(result *common.LoaderResult) {
	if result.FailMsg != "" {
		r.target.Source = result.Source
		r.target.Result = result.Result
		r.target.FailCount += 1
		r.target.FailMsg = result.FailMsg
	} else {
		r.target.Source = result.Source
		r.target.Result = common.DOWNLOAD_RESULT_OK
		r.target.FailCount = 0
		r.target.FailMsg = ""
	}
	r.target.UpdateTime = time.Now().Unix()

}

func (r *downloadRecord) onItemUpdate(svrItem *common.ServerItem) {
	r.target = &common.DownloadItemInfo{
		Key:        svrItem.Key(),
		Version:    svrItem.Version(),
		CreateTime: time.Now().Unix(),
		UpdateTime: time.Now().Unix(),
		Source:     common.DOWNLOAD_SOURCE_INIT,
		Result:     common.DOWNLOAD_RESULT_INIT,
		FailCount:  0,
		FailMsg:    "", //初始化时，应该是没有错误的
		Flow:       svrItem.Size(),
	}

}

func (r *downloadRecord) callbackErr(result common.DownloadItemResult, err error) {
	r.target.Result = result
	r.target.FailMsg = err.Error()
	r.target.FailCount += 1
	r.target.UpdateTime = time.Now().Unix()
}

func (r *downloadRecord) valueError(result common.DownloadItemResult, err error) {
	r.target.Result = result
	r.target.FailMsg = err.Error()
	r.target.FailCount = 1
}
