package common

import (
	"errors"
	"fmt"
	"time"

	cmodel "code.byted.org/bcc/bcc-go-client/coreclient/model"
	internalerror "code.byted.org/bcc/bcc-go-client/internal/error"
	"code.byted.org/bcc/tools"
)

type SvrKeyInfo struct {
	Key           string                   `json:"key,omitempty"`        //
	Value         []byte                   `json:"value,omitempty"`      //
	Version       int64                    `json:"version,omitempty"`    //
	Md5           string                   `json:"md5,omitempty"`        //inline不用校验，tos才校验
	Desc          string                   `json:"desc,omitempty"`       //删了？
	Compressor    string                   `json:"compressor,omitempty"` //保留，通常为gzip或者zstd
	Status        ItemStatus               `json:"status,omitempty"`     //
	HitEnv        string                   `json:"hit_env,omitempty"`    //命中环境：online/gray/ppe名称
	Size          int64                    `json:"size,omitempty"`       //实际大小（必须大于0）
	UpdateTime    int64                    `json:"updateTime,omitempty"` //更新时间，unix时间戳。UnixNano()返回的
	UpdateID      int64                    `json:"update_id,omitempty"`
	SDKGrayRule   string                   `json:"SDKGrayRule,omitempty"`   //流量灰度规则
	Serialization cmodel.SerializationType `json:"serialization,omitempty"` //序列化的算法。json或者msgpack
	FailMsg       string                   `json:"fail_msg"`                //当Status为ITEM_ERR时的具体错误原因

	DownloadInfos []*DownloadInfo `json:"download_infos"` // TODO：大文件下载url，包括签名、token、agent等信息

	Contents []*ContentBlock `json:"contents"` // 分块内容

}

type ContentBlock struct {
	Content       []byte          `json:"content"`      // 配置内容。对于tos url type，值为空
	Compressor    string          `json:"compressor"`   // 保留，通常为gzip或者zstd
	ContentSize   int64           `json:"content_size"` // 单位为字节
	ContentMD5    string          `json:"content_md5"`
	ContentDesc   string          `json:"content_desc"`             // 用于用户描述每个content的作用，例如meta、base、fullBase
	DownloadInfos []*DownloadInfo `json:"download_infos,omitempty"` // 大文件下载任务
}

type ItemStatus int

const (
	ITEM_VALID       ItemStatus = 0 //正常有效
	ITEM_INVALID     ItemStatus = 1 //key被删除或不存在
	ITEM_AUTH_FAILED ItemStatus = 2 //鉴权失败
	ITEM_ERR         ItemStatus = 3 //配置错误。一般是server读取存储引擎失败
)

type DownloadInfo struct {
	Url    string             `json:"url"`
	Source DownloadItemSource `json:"source"`
	Agent  string             `json:"agent"`
}

func (d DownloadInfo) HasAgent() bool {
	return d.Agent != ""
}

type ServerItem struct {
	item *SvrKeyInfo
}

func NewServerItem(item *SvrKeyInfo) *ServerItem {
	return &ServerItem{item: item}
}

func (s ServerItem) Value() []byte {
	return s.item.Value
}

func (s ServerItem) Serialization() string {
	return string(s.item.Serialization)
}

func (s ServerItem) Key() string {
	return s.item.Key
}
func (s ServerItem) Status() ItemStatus {
	return s.item.Status
}
func (s ServerItem) Valid() bool {
	return s.item.Status == ITEM_VALID
}
func (s ServerItem) IsAuthFail() bool {
	return s.item.Status == ITEM_AUTH_FAILED
}
func (s ServerItem) IsDelete() bool {
	return s.item.Status == ITEM_INVALID
}

func (s ServerItem) IsItemErr() bool {
	return s.item.Status == ITEM_ERR
}

func (s ServerItem) FailMsg() string {
	return s.item.FailMsg
}

func (s ServerItem) DownloadInfos() []*DownloadInfo {
	return s.item.DownloadInfos
}

func (s ServerItem) IsNewer(localUpdateID int64) bool {
	item := s.item
	return localUpdateID < item.UpdateID || !s.Valid()
}

func (s ServerItem) Version() int64 {
	return s.item.Version
}

func (s ServerItem) UpdateID() int64 {
	return s.item.UpdateID
}

func (s ServerItem) KeyInfo() string {
	return fmt.Sprintf("keyname[%v].version[%v].updateID[%v].hitEnv[%v].status[%v].size[%v].md5[%v]",
		s.Key(), s.Version(), s.UpdateID(), s.item.HitEnv, s.Status(), s.Size(), s.item.Md5)
}

func (s ServerItem) SDKGrayRule() string {
	return s.item.SDKGrayRule
}

func (s ServerItem) NeedDownload() bool {
	if s.IsMultiContent() {
		// 多文件协议在该版本只支持非大文件的逻辑
		return false
	}
	return s.Valid() && len(s.item.Value) == 0
}

func (s ServerItem) GetDecompressValue() ([]byte, error) {
	item := s.item
	value := item.Value
	return s.DecompressValue(value, item.Compressor)
}

// GetDecompressValues 对多块协议中的数据进行解压和返回
func (s ServerItem) GetDecompressValues() ([]*cmodel.Content, error) {
	result := make([]*cmodel.Content, 0, len(s.item.Contents))
	for _, one := range s.item.Contents {
		v, err := s.DecompressValue(one.Content, one.Compressor)
		if err != nil {
			return nil, err
		}
		result = append(result, &cmodel.Content{
			Value: v,
			MD5:   one.ContentMD5,
			Size:  one.ContentSize,
			Desc:  one.ContentDesc,
		})
	}
	return result, nil
}

// IsMultiContent 判断是否仅仅是多文件协议
func (s ServerItem) IsMultiContent() bool {
	return len(s.item.Value) == 0 && len(s.item.Contents) > 0
}

// DecompressValue 解压单块内容
func (s ServerItem) DecompressValue(value []byte, compressor string) ([]byte, error) {
	if len(value) > 0 && compressor != "" {
		type decompressFunc func(src []byte) ([]byte, error)
		var deFunc decompressFunc
		if compressor == "gzip" {
			deFunc = tools.GzipDecompress
		} else if compressor == "zstd" {
			deFunc = tools.ZstdDeCompress
		} else {
			return nil, errors.New("invalid compressor=" + compressor)
		}

		if val, err := deFunc(value); err != nil {
			return nil, fmt.Errorf("decompress[%v] fail. err:%v", compressor, err)
		} else {
			return val, nil
		}
	} else {
		//未定义行为，不应是未压缩的值
		return value, nil
	}
}

func (s ServerItem) Md5() string {
	return s.item.Md5
}

func (s ServerItem) Size() int64 {
	return s.item.Size
}

func (s ServerItem) UpdateTime() int64 {
	return s.item.UpdateTime
}

//======================================================================================================================

type DownloadItemSource int32

const (
	DOWNLOAD_SOURCE_INIT   DownloadItemSource = 0 //初始化
	DOWNLOAD_SOURCE_INLINE DownloadItemSource = 1 //server直接返回
	DOWNLOAD_SOURCE_FILE   DownloadItemSource = 2 //本地文件缓存
	DOWNLOAD_SOURCE_P2P    DownloadItemSource = 3 //通过p2p向tos下载
	DOWNLOAD_SOURCE_BP2P   DownloadItemSource = 4 //通过备份p2p向tos下载
	DOWNLOAD_SOURCE_TOS    DownloadItemSource = 5 //直接从tos下载
	DOWNLOAD_SOURCE_FAIL   DownloadItemSource = 6 //失败
	DOWNLOAD_SOURCE_BTOS   DownloadItemSource = 7 //直接从slaveTos下载
	DOWNLOAD_SOURCE_HTTP2P DownloadItemSource = 8 //直接从HTTP2P PROXY下载
)

func (s DownloadItemSource) String() string {
	if str, exist := DownloadSourceName[int32(s)]; exist {
		return str
	}

	return "unknownSource"
}

func (s DownloadItemSource) IsDownloadFromNetwork() bool {
	return s == DOWNLOAD_SOURCE_TOS || s == DOWNLOAD_SOURCE_BTOS ||
		s == DOWNLOAD_SOURCE_P2P || s == DOWNLOAD_SOURCE_BP2P
}

type DownloadItemResult int32

const (
	DOWNLOAD_RESULT_INIT           DownloadItemResult = 0 //初始化
	DOWNLOAD_RESULT_OK             DownloadItemResult = 1 //成功
	DOWNLOAD_RESULT_INVALID        DownloadItemResult = 2 //不存在key
	DOWNLOAD_RESULT_TOS_RETRY      DownloadItemResult = 3 //tos其他错误，重试
	DOWNLOAD_RESULT_WRITEFILE      DownloadItemResult = 4 //写文件失败（权限问题）
	DOWNLOAD_RESULT_SYSTEM         DownloadItemResult = 5 //系统错误
	DOWNLOAD_RESULT_EMPTY          DownloadItemResult = 6 //监听不存在的文件，认为成功
	DOWNLOAD_RESULT_CALLBACK_RETRY DownloadItemResult = 7 //回调函数返回失败，但能重试
	//下面错误不应该出现，无法恢复
	DOWNLOAD_RESULT_TOS_NOTFOUND DownloadItemResult = 11 //tos不存在文件
	DOWNLOAD_RESULT_TOS_ZERO     DownloadItemResult = 12 //tos文件大小为0
	DOWNLOAD_RESULT_TOS_CLOSE    DownloadItemResult = 13 //tos功能关闭
	DOWNLOAD_RESULT_TOS_MD5      DownloadItemResult = 14 //md5不一致
	DOWNLOAD_RESULT_DECOMPRESS   DownloadItemResult = 15 //解压失败
	DOWNLOAD_RESULT_CALLBACK     DownloadItemResult = 16 //回调函数返回失败
	DOWNLOAD_RESULT_AUTH_FAILED  DownloadItemResult = 17 //ACL鉴权失败
	DOWNLOAD_RESULT_ITEM_ERR     DownloadItemResult = 18 //item错误，一般是读存储引擎失败导致的
)

func GetResultError(result DownloadItemResult, msgOpt ...string) internalerror.BaseError {

	msg := "download result"
	if len(msgOpt) != 0 {
		msg = msgOpt[0]
	}

	switch result {
	case DOWNLOAD_RESULT_OK, DOWNLOAD_RESULT_EMPTY, DOWNLOAD_RESULT_INIT:
		return nil
	case DOWNLOAD_RESULT_DECOMPRESS, DOWNLOAD_RESULT_TOS_MD5:
		return internalerror.NewError(internalerror.ErrItemContent, msg)
	case DOWNLOAD_RESULT_INVALID:
		return internalerror.NewError(internalerror.ErrNotExist, msg)
	case DOWNLOAD_RESULT_AUTH_FAILED:
		return internalerror.NewError(internalerror.ErrAuthFailed, msg)
	case DOWNLOAD_RESULT_WRITEFILE:
		return internalerror.NewError(internalerror.ErrWriteFile, msg)
	case DOWNLOAD_RESULT_CALLBACK:
		return internalerror.NewError(internalerror.ErrCallback, msg)
	case DOWNLOAD_RESULT_CALLBACK_RETRY:
		return internalerror.NewError(internalerror.ErrCallbackRetry, msg)
	case DOWNLOAD_RESULT_TOS_RETRY:
		return internalerror.NewError(internalerror.ErrTosRetry, msg)
	case DOWNLOAD_RESULT_TOS_NOTFOUND, DOWNLOAD_RESULT_TOS_ZERO, DOWNLOAD_RESULT_TOS_CLOSE:
		return internalerror.NewError(internalerror.ErrBigFileFail, msg)
	case DOWNLOAD_RESULT_ITEM_ERR:
		return internalerror.NewError(internalerror.ErrItemError, msg)

	default:
		return internalerror.NewError(internalerror.ErrUnknown, msg)
	}

	return nil
}

func (r DownloadItemResult) String() string {
	if str, exist := DownloadResultName[int32(r)]; exist {
		return str
	}

	return "unknownResult"
}

var (
	DownloadSourceName = map[int32]string{
		0: "SOURCE_INIT",
		1: "SOURCE_INLINE",
		2: "SOURCE_FILE",
		3: "SOURCE_P2P",
		4: "SOURCE_BP2P",
		5: "SOURCE_TOS",
		6: "SOURCE_FAIL",
		7: "SOURCE_BTOS",
		8: "SOURCE_HTTP2P",
	}

	DownloadResultName = map[int32]string{
		0:  "RESULT_INIT",
		1:  "RESULT_OK",
		2:  "RESULT_INVALID",
		3:  "RESULT_TOS_RETRY",
		4:  "RESULT_WRITEFILE",
		5:  "RESULT_SYSTEM",
		6:  "RESULT_EMPTY",
		7:  "RESULT_CALLBACK_RETRY",
		11: "RESULT_TOS_NOTFIND",
		12: "RESULT_TOS_ZERO",
		13: "RESULT_TOS_CLOSE",
		14: "RESULT_TOS_MD5",
		15: "RESULT_DECOMPRESS",
		16: "RESULT_CALLBACK",
	}
)

type DownloadItemInfo struct {
	Key        string             `json:"key,omitempty"`         //
	Version    int64              `json:"version,omitempty"`     //
	CreateTime int64              `json:"create_time,omitempty"` //创建时间
	UpdateTime int64              `json:"update_time,omitempty"` //更新时间
	Source     DownloadItemSource `json:"source,omitempty"`      //下载来源
	Result     DownloadItemResult `json:"result,omitempty"`      //下载结果
	FailCount  int64              `json:"fail_count,omitempty"`  //失败次数
	FailMsg    string             `json:"fail_msg,omitempty"`    //失败描述
	Flow       int64              `json:"flow,omitempty"`        //真实流量
}

//======================================================================================================================

type CltItem struct {
	Key          string            `json:"key,omitempty"`           //
	EnableListen bool              `json:"enable_listen,omitempty"` //是否监听变更
	EnableEmpty  bool              `json:"enable_empty,omitempty"`  //是否监听不存在的key
	UpdateID     int64             `json:"update_id,omitempty"`
	Md5          string            `json:"md5,omitempty"`
	Version      int64             `json:"version,omitempty"`     //
	UpdateTime   int64             `json:"update_time,omitempty"` //
	Valid        bool              `json:"valid,omitempty"`       //
	Target       *DownloadItemInfo `json:"target,omitempty"`      //下载记录
}

//======================================================================================================================

type CltDir struct {
	Path         string              `json:"path,omitempty"`          //
	EnableListen bool                `json:"enable_listen,omitempty"` //是否监听
	FirstTime    int64               `json:"first_time,omitempty"`    //首次获取完整目录列表的时间（enable_listen=false时使用）
	Items        map[string]*CltItem `json:"items,omitempty"`         //客户端持有的列表（重连时）
}

//======================================================================================================================

type DumpItem struct {
	DownloadTime   time.Time                `json:"download_time"`    //sdk获取到该item的时间
	IsSuccCallback bool                     `json:"is_succ_callback"` //是否成功回调
	Keyname        string                   `json:"keyname"`          //配置名
	UpdateID       int64                    `json:"update_id"`        //updateID
	Val            []byte                   `json:"-"`                // 原始数据，需要解压，反序列化
	Vals           []*cmodel.Content        `json:"-"`                // 原始数据，需要解压、反序列化
	BigFilepath    string                   `json:"big_filepath"`     //大文件路径。 这个不一定会有。如果禁用了磁盘写入，那么这里就会为空。
	Serializer     cmodel.SerializationType `json:"-"`                //序列化算法
}
