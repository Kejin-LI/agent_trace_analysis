package serializer

type ValueType byte

const (
	// raw型,没有valueType和valueSize,只有value
	ByteRaw   ValueType = iota // 占用一个byte
	IntRaw                     // 占用4byte
	Uint64Raw                  // 占用8byte

	// 普通value型,存储结构为:valueType+(valueSize)+value
	String
	Byte
	Bool
	Int
	Int8
	Int16
	Int32
	Int64
	Uint
	Uint8
	Uint16
	Uint32
	Uint64
	Float32
	Float64
	IPv4
	IPv6
	UUID
	Bytes
	Date //暂不支持
)

// 在私有协议中,平铺头部的抽象，这些头部没有key，并且占用的空间大小以及顺序是固定的，例如Header以及LogHeader.
type Header interface {
	// 将配置展平
	Flatten() []ValueType
}

type KeyValue struct {
	Key   string
	Value interface{}
}

type LogHeader struct {
	ReserveHeader   Header
	CustomizeHeader []KeyValue
}

type LogContent struct {
	KeyValues []KeyValue
}

type DataPack struct {
	LogHeader  *LogHeader
	LogContent *LogContent
}

type Serializer interface {
	// 设置协议头
	SetHeader(option Header)

	SetCommonHeaders(headers []KeyValue) error

	// 清除LogHeaders和LogContents的数据
	Clear()

	// 清除header以外的数据
	Reset()

	// 添加数据
	Feed(dataPack *DataPack) error
	// 获取编码数据
	Serialize() ([]byte, error)
}
