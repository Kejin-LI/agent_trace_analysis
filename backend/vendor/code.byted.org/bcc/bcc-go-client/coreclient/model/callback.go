package model

type PathCallbackStatus string

const (
	PathUpdate   PathCallbackStatus = "update"    // 新增或者更新
	PathDelete   PathCallbackStatus = "delete"    // 删除
	PathAtuhFail PathCallbackStatus = "auth_fail" // 鉴权失败
)

type SerializationType string

const (
	DefaultSerialize SerializationType = "" //json
	JsonSerialize    SerializationType = "json"
	MsgPackSerialize SerializationType = "msgpack"
)

func (s SerializationType) IsJsonSerialization() bool {
	return s == DefaultSerialize || s == JsonSerialize
}
func (s SerializationType) IsMsgpackSerialization() bool {
	return s == MsgPackSerialize
}

// 回调函数by目录
// 1、和KeyCallback类似
// 2、 PathUpdate：key新增或更新, PathDelete：key被删除, PAthAuthFail:对应key鉴权失败
type PathCallback func(item Item, pathStatus PathCallbackStatus) error

// 回调函数
// 1、数据准备好后，由sdk主动调用
// 2、所有key都串行调用回调函数，业务方不需要考虑并发问题，client级别串行处理
// 3、回调函数必须足够快，否则会阻塞sdk内部逻辑，最好只是反序列化或简单的全局初始化，如果超过100毫秒最好另外启动协程
// 4、回调函数返回的Item，不会再修改，业务方可以放心持有
// 5、特殊情况如果需要重试，可以使用 NewCallbackRetryError
type KeyCallback func(item Item) error

// item定义
// 1、通过回调函数返回，Item创建后不会修改（除非调用Clear函数）
// 2、使用方可以把item放在本地内存
type Item interface {
	Key() string                      //完整路径
	Value() []byte                    //原始数据（使用WithWatchDisableMemory后数据不可用）//外部不允许修改
	Values() []*Content               //多文件原始数据（使用WithWatchDisableMemory后数据不可用）//外部不允许修改
	Serialization() SerializationType //Value的序列化的算法。“”、json或者msgpack。为空时等同于json
	SDKGrayRule() string              //流量灰度规则 //外部不允许修改
	Version() int64                   //版本号，可能会变小（1~n）
	Clear()                           // 清理内部缓存并赋值为"[clear]"，只允许在回调函数里执行，一般用于大文件而用户自己管理内存
	BackupFile() string               //备份文件路径（2种情况有数据：从tos下载并写文件成功，设置了WithWatchDisableMemory）//只保证回调函数内有效，回调结束后随时会删除文件
	Path() string                     //PathCallback时有效 //多个path共用回调函数的时候使用
	UpdateID() int64
	KeySnapshot() []byte
	PathSnapshot() []byte
}

// Content 多文件单块传输内容
type Content struct {
	Value      []byte `msgpack:"-" json:"-"` // 配置内容
	BackupFile string // 备份文件路径（2种情况有数据：从tos下载并写文件成功，设置了WithWatchDisableMemory）//只保证回调函数内有效，回调结束后随时会删除文件
	MD5        string // 配置MD5
	Size       int64  // 配置大小
	Desc       string // 使用用途描述
}
