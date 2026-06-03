/**
  see https://bytedance.feishu.cn/docs/doccndEvBDduyL1SiwzUBHYrnBh#.
	仅支持单线程处理.
*/
package serializer

import (
	"github.com/pkg/errors"
)

const (
	MaxStringLength = 255

	StringSplitByte byte = 0x00

	FalseByte byte = 0x0
	TrueByte  byte = 0x1
)

var (
	// 字典相关
	TrueBoolData             = []byte{BoolType, TrueByte}
	FalseBoolData            = []byte{BoolType, FalseByte}
	IndexOutOfBoundError     = errors.New("Index out of bound!")
	OutOfBufferBoundError    = errors.New("Out of buffer bound!")
	NotApplyTypeError        = errors.New("Not apply type!")
	NotSupportValueTypeError = errors.New("not support value type!")
	UnRecognizeTypeError     = errors.New("type is unRecognize!")
)

type pool struct {
	stringHeader    []byte
	textHeader      []byte
	intHeader       []byte
	longHeader      []byte
	uint64Header    []byte
	doubleHeader    []byte
	bytesHeader     []byte
	ipv4Header      []byte
	ipv6Header      []byte
	bytesUintHeader []byte
	uuidHeader      []byte

	lengthBody      []byte
	traceLengthBody []byte

	intLine    []byte
	uint64Line []byte
}

func newPool() pool {
	return pool{
		stringHeader:    []byte{StringType, 0},
		textHeader:      []byte{TextType, 0, 0, 0, 0},
		intHeader:       []byte{IntType, 0, 0, 0, 0},
		longHeader:      []byte{LongType, 0, 0, 0, 0, 0, 0, 0, 0},
		uint64Header:    []byte{Uint64Type, 0, 0, 0, 0, 0, 0, 0, 0},
		doubleHeader:    []byte{DoubleType, 0, 0, 0, 0, 0, 0, 0, 0},
		bytesUintHeader: []byte{0, 0, 0, 0, 0}, //type,length
		ipv4Header:      []byte{Ipv4Type, 0, 0, 0, 0},
		ipv6Header:      []byte{Ipv6Type, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		lengthBody:      []byte{0, 0, 0, 0},
		traceLengthBody: []byte{0, 0, 0, 0, 0, 0, 0, 0, 0},
		uuidHeader:      []byte{UUIdType, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		intLine:         make([]byte, 4),
		uint64Line:      make([]byte, 8),
	}
}

type byteBuffer struct {
	buffer          Buffer
	bigEndian       bool
	patternPos      int  // pattern起始位置
	commonHeaderPos int  // commonHeader起始位置
	traceHeaderPos  int  // traceHeader起始位置
	traceContentPos int  // traceContent起始位置
	nowPos          int  // 当前的位置
	pool            pool // 资源池
}

func NewByteBuffer(bigEndian bool) WriterBuffer {
	return &byteBuffer{
		bigEndian:       bigEndian,
		patternPos:      0,
		commonHeaderPos: 0,
		traceHeaderPos:  0,
		traceContentPos: 0,
		pool:            newPool(),
	}
}

func (byteBuf *byteBuffer) Bytes() []byte {
	return byteBuf.buffer.Bytes()
}

func (byteBuf *byteBuffer) BytesCopy() []byte {
	bs := byteBuf.buffer.Bytes()
	cp := make([]byte, len(bs))
	copy(cp, bs)
	return cp
}

func (byteBuf *byteBuffer) Read(data []byte) (n int, err error) {
	return byteBuf.buffer.Read(data)
}

func (byteBuf *byteBuffer) Len() int {
	return byteBuf.buffer.Len()
}

func (byteBuf *byteBuffer) Next(n int) []byte {
	return byteBuf.buffer.Next(n)
}

func (byteBuf *byteBuffer) Reset() {
	byteBuf.buffer.Reset()
}

func (byteBuf *byteBuffer) peekInt() (value int, err error) {
	bs, err := byteBuf.buffer.Peek(4)
	if err != nil {
		return 0, err
	}
	return int(BytesToInt32(bs, byteBuf.bigEndian)), nil
}

func (byteBuf *byteBuffer) peekByte() (value byte, err error) {
	bs, err := byteBuf.buffer.Peek(1)
	if err != nil {
		return 0, err
	}
	return bs[0], nil
}

func (byteBuf *byteBuffer) peekKeyIdAndType() (keyId byte, t byte, err error) {
	bs, err := byteBuf.buffer.Peek(2)
	if err != nil {
		return 0, 0, err
	}
	return bs[0], bs[1], nil
}

// 继承接口.
func (byteBuf *byteBuffer) Write(data []byte) (n int) {
	n, _ = byteBuf.buffer.Write(data)
	return n
}

func (byteBuf *byteBuffer) WriteValue(value interface{}) (n int, err error) {
	switch v := value.(type) {
	case string:
		return byteBuf.WriteString(v)
	case []byte:
		return byteBuf.WriteBytes(v)
	case int:
		return byteBuf.WriteInt(v)
	case uint:
		return byteBuf.WriteUint(v)
	case int8:
		return byteBuf.WriteInt8(v)
	case uint8:
		return byteBuf.WriteUint8(v)
	case int16:
		return byteBuf.WriteInt16(v)
	case uint16:
		return byteBuf.WriteUint16(v)
	case int32:
		return byteBuf.WriteInt32(v)
	case uint32:
		return byteBuf.WriteUint32(v)
	case int64:
		return byteBuf.WriteInt64(v)
	case uint64:
		return byteBuf.WriteUint64(v)
	case float32:
		return byteBuf.WriteFloat32(v)
	case float64:
		return byteBuf.WriteFloat64(v)
	case bool:
		return byteBuf.WriteBool(v)
	case Ipv4:
		return byteBuf.WriteIPV4(v)
	case Ipv6:
		return byteBuf.WriteIPV6(v)
	case Uuid:
		return byteBuf.WriteUUid(v)
	default:
		return 0, nil
	}
}

func (byteBuf *byteBuffer) WriteDicKeyValue(key byte, value interface{}) (n int, err error) {
	switch v := value.(type) {
	case string:
		return byteBuf.WriteDicStringValue(key, v)
	case []byte:
		return byteBuf.WriteDicBytesValue(key, v)
	case int:
		return byteBuf.WriteDicIntValue(key, v)
	case uint:
		return byteBuf.WriteDicUintValue(key, v)
	case int8:
		return byteBuf.WriteDicInt8Value(key, v)
	case uint8:
		return byteBuf.WriteDicUint8Value(key, v)
	case int16:
		return byteBuf.WriteDicInt16Value(key, v)
	case uint16:
		return byteBuf.WriteDicUint16Value(key, v)
	case int32:
		return byteBuf.WriteDicInt32Value(key, v)
	case uint32:
		return byteBuf.WriteDicUint32Value(key, v)
	case int64:
		return byteBuf.WriteDicInt64Value(key, v)
	case uint64:
		return byteBuf.WriteDicUint64Value(key, v)
	case float32:
		return byteBuf.WriteDicFloat32Value(key, v)
	case float64:
		return byteBuf.WriteDicFloat64Value(key, v)
	case bool:
		return byteBuf.WriteDicBoolValue(key, v)
	case Ipv4:
		return byteBuf.WriteDicIPV4Value(key, v)
	case Ipv6:
		return byteBuf.WriteDicIPV6Value(key, v)
	case Uuid:
		return byteBuf.WriteDicUUidValue(key, v)
	default:
		return 0, UnRecognizeTypeError
	}
}

func (byteBuf *byteBuffer) WriteKeyValue(key string, value interface{}) (n int, err error) {
	switch v := value.(type) {
	case string:
		return byteBuf.WriteStringValue(key, v)
	case []byte:
		return byteBuf.WriteBytesValue(key, v)
	case int:
		return byteBuf.WriteIntValue(key, v)
	case uint:
		return byteBuf.WriteUintValue(key, v)
	case int8:
		return byteBuf.WriteInt8Value(key, v)
	case uint8:
		return byteBuf.WriteUint8Value(key, v)
	case int16:
		return byteBuf.WriteInt16Value(key, v)
	case uint16:
		return byteBuf.WriteUint16Value(key, v)
	case int32:
		return byteBuf.WriteInt32Value(key, v)
	case uint32:
		return byteBuf.WriteUint32Value(key, v)
	case int64:
		return byteBuf.WriteInt64Value(key, v)
	case uint64:
		return byteBuf.WriteUint64Value(key, v)
	case float32:
		return byteBuf.WriteFloat32Value(key, v)
	case float64:
		return byteBuf.WriteFloat64Value(key, v)
	case bool:
		return byteBuf.WriteBoolValue(key, v)
	case Ipv4:
		return byteBuf.WriteIPV4Value(key, v)
	case Ipv6:
		return byteBuf.WriteIPV6Value(key, v)
	case Uuid:
		return byteBuf.WriteUUidValue(key, v)
	default:
		return 0, UnRecognizeTypeError
	}
}

func (byteBuf *byteBuffer) WriteDouble(num float64) (n int, err error) {
	b := Float64ToBytes(num, false)
	byteBuf.pool.doubleHeader[1] = b[0]
	byteBuf.pool.doubleHeader[2] = b[1]
	byteBuf.pool.doubleHeader[3] = b[2]
	byteBuf.pool.doubleHeader[4] = b[3]
	byteBuf.pool.doubleHeader[5] = b[4]
	byteBuf.pool.doubleHeader[6] = b[5]
	byteBuf.pool.doubleHeader[7] = b[6]
	byteBuf.pool.doubleHeader[8] = b[7]
	return byteBuf.buffer.Write(byteBuf.pool.doubleHeader)
}

// Dic.
func (byteBuf *byteBuffer) WriteDicIPV4Value(key byte, value Ipv4) (n int, err error) {
	e := byteBuf.buffer.WriteByte(key)
	if e != nil {
		return 0, e
	}
	l, e := byteBuf.WriteIPV4(value)
	return 1 + l, e
}

func (byteBuf *byteBuffer) WriteDicIPV6Value(key byte, value Ipv6) (n int, err error) {
	e := byteBuf.buffer.WriteByte(key)
	if e != nil {
		return 0, e
	}
	l, e := byteBuf.WriteIPV6(value)
	return 1 + l, e
}

func (byteBuf *byteBuffer) WriteDicUUidValue(key byte, value Uuid) (n int, err error) {
	e := byteBuf.buffer.WriteByte(key)
	if e != nil {
		return 0, e
	}
	l, e := byteBuf.WriteUUid(value)
	return 1 + l, e
}

func (byteBuf *byteBuffer) WriteDicStringValue(key byte, value string) (n int, err error) {
	e := byteBuf.buffer.WriteByte(key)
	if e != nil {
		return 0, e
	}
	vl, e := byteBuf.WriteString(value)
	return 1 + vl, e
}

func (byteBuf *byteBuffer) WriteDicBoolValue(key byte, value bool) (n int, err error) {
	e := byteBuf.buffer.WriteByte(key)
	if e != nil {
		return 0, e
	}
	l, e := byteBuf.WriteBool(value)
	return 1 + l, e
}

func (byteBuf *byteBuffer) WriteDicIntValue(key byte, value int) (n int, err error) {
	e := byteBuf.buffer.WriteByte(key)
	if e != nil {
		return 0, e
	}
	l, err := byteBuf.WriteInt(value)
	return 1 + l, err
}

// int8,int16,int32 => int32.
func (byteBuf *byteBuffer) WriteDicInt8Value(key byte, value int8) (n int, err error) {
	return byteBuf.WriteDicInt32Value(key, int32(value))
}

func (byteBuf *byteBuffer) WriteDicInt16Value(key byte, value int16) (n int, err error) {
	return byteBuf.WriteDicInt32Value(key, int32(value))
}

func (byteBuf *byteBuffer) WriteDicInt32Value(key byte, value int32) (n int, err error) {
	e := byteBuf.buffer.WriteByte(key)
	if e != nil {
		return 0, e
	}
	l, err := byteBuf.WriteInt32(value)
	return 1 + l, err
}

func (byteBuf *byteBuffer) WriteDicInt64Value(key byte, value int64) (n int, err error) {
	e := byteBuf.buffer.WriteByte(key)
	if e != nil {
		return 0, e
	}
	l, err := byteBuf.WriteInt64(value)
	return 1 + l, err
}

// uint8,uint16 => int32.
func (byteBuf *byteBuffer) WriteDicUint8Value(key byte, value uint8) (n int, err error) {
	return byteBuf.WriteDicInt32Value(key, int32(value))
}

func (byteBuf *byteBuffer) WriteDicUint16Value(key byte, value uint16) (n int, err error) {
	return byteBuf.WriteDicInt32Value(key, int32(value))
}

// uint32, uint64 => uint64.
func (byteBuf *byteBuffer) WriteDicUint32Value(key byte, value uint32) (n int, err error) {
	return byteBuf.WriteDicUint64Value(key, uint64(value))
}

func (byteBuf *byteBuffer) WriteDicUint64Value(key byte, value uint64) (n int, err error) {
	e := byteBuf.buffer.WriteByte(key)
	if e != nil {
		return 0, e
	}
	l, e := byteBuf.WriteUint64(value)
	return 1 + l, nil
}

func (byteBuf *byteBuffer) WriteDicUintValue(key byte, value uint) (n int, err error) {
	return byteBuf.WriteDicUint64Value(key, uint64(value))
}

// float32转成float64.
func (byteBuf *byteBuffer) WriteDicFloat32Value(key byte, value float32) (n int, err error) {
	return byteBuf.WriteDicFloat64Value(key, float64(value))
}

func (byteBuf *byteBuffer) WriteDicFloat64Value(key byte, value float64) (n int, err error) {
	e := byteBuf.buffer.WriteByte(key)
	if e != nil {
		return 0, e
	}
	fl, err := byteBuf.WriteDouble(value)
	return 1 + fl, err
}

func (byteBuf *byteBuffer) WriteDicBytesValue(key byte, value []byte) (n int, err error) {
	e := byteBuf.buffer.WriteByte(key)
	if e != nil {
		return 0, e
	}
	l, e := byteBuf.WriteBytes(value)
	return 1 + l, e
}

// write not dic value.

func (byteBuf *byteBuffer) WriteIPV4Value(key string, value Ipv4) (n int, err error) {
	wl, e := byteBuf.WriteStringKey(key)
	if e != nil {
		return wl, e
	}
	l, e := byteBuf.WriteIPV4(value)
	return wl + l, e
}

func (byteBuf *byteBuffer) WriteIPV6Value(key string, value Ipv6) (n int, err error) {
	wl, e := byteBuf.WriteStringKey(key)
	if e != nil {
		return wl, e
	}
	l, e := byteBuf.WriteIPV6(value)
	return wl + l, e
}

func (byteBuf *byteBuffer) WriteUUidValue(key string, value Uuid) (n int, err error) {
	wl, e := byteBuf.WriteStringKey(key)
	if e != nil {
		return wl, e
	}
	l, e := byteBuf.WriteUUid(value)
	return wl + l, e
}

func (byteBuf *byteBuffer) WriteStringValue(key string, value string) (n int, err error) {
	wl, e := byteBuf.WriteStringKey(key)
	if e != nil {
		return wl, e
	}
	vl, e := byteBuf.WriteString(value)
	return wl + vl, e
}

// 写入长度<256的String.
func (byteBuf *byteBuffer) writeStringByte(data []byte) (n int, err error) {
	byteBuf.pool.stringHeader[1] = byte(len(data))
	wl, err := byteBuf.buffer.Write(byteBuf.pool.stringHeader)
	if err != nil {
		return wl, err
	}
	_, err = byteBuf.buffer.Write(data)
	if err != nil {
		return 0, err
	}
	err = byteBuf.buffer.WriteByte(StringSplitByte)
	return wl + len(data) + 1, err
}

// 写入String的key, key长度<256.
func (byteBuf *byteBuffer) WriteStringKey(key string) (n int, err error) {
	data := StringToSliceByte(key)
	if len(data) > MaxStringLength {
		return byteBuf.writeStringByte(data[:MaxStringLength])
	}
	return byteBuf.writeStringByte(data)
}

func (byteBuf *byteBuffer) WriteBoolValue(key string, value bool) (n int, err error) {
	wl, e := byteBuf.WriteStringKey(key)
	if e != nil {
		return wl, e
	}
	l, e := byteBuf.WriteBool(value)
	return wl + l, e
}

func (byteBuf *byteBuffer) WriteIntValue(key string, value int) (n int, err error) {
	wl, e := byteBuf.WriteStringKey(key)
	if e != nil {
		return wl, e
	}
	l, err := byteBuf.WriteInt(value)
	return wl + l, err
}

// int8,int16,int32 => int32.
func (byteBuf *byteBuffer) WriteInt8Value(key string, value int8) (n int, err error) {
	return byteBuf.WriteInt32Value(key, int32(value))
}

func (byteBuf *byteBuffer) WriteInt16Value(key string, value int16) (n int, err error) {
	return byteBuf.WriteInt32Value(key, int32(value))
}

func (byteBuf *byteBuffer) WriteInt32Value(key string, value int32) (n int, err error) {
	wl, e := byteBuf.WriteStringKey(key)
	if e != nil {
		return wl, e
	}
	l, err := byteBuf.WriteInt32(value)
	return wl + l, err
}

func (byteBuf *byteBuffer) WriteInt64Value(key string, value int64) (n int, err error) {
	wl, e := byteBuf.WriteStringKey(key)
	if e != nil {
		return wl, e
	}
	l, err := byteBuf.WriteInt64(value)
	return wl + l, err
}

// uint8,uint16 => int32.
func (byteBuf *byteBuffer) WriteUint8Value(key string, value uint8) (n int, err error) {
	return byteBuf.WriteInt32Value(key, int32(value))
}

func (byteBuf *byteBuffer) WriteUint16Value(key string, value uint16) (n int, err error) {
	return byteBuf.WriteInt32Value(key, int32(value))
}

// uint32, uint64 => uint64.
func (byteBuf *byteBuffer) WriteUint32Value(key string, value uint32) (n int, err error) {
	return byteBuf.WriteUint64Value(key, uint64(value))
}

func (byteBuf *byteBuffer) WriteUint64Value(key string, value uint64) (n int, err error) {
	wl, e := byteBuf.WriteStringKey(key)
	if e != nil {
		return wl, e
	}
	l, e := byteBuf.WriteUint64(value)
	return wl + l, nil
}

func (byteBuf *byteBuffer) WriteUintValue(key string, value uint) (n int, err error) {
	return byteBuf.WriteUint64Value(key, uint64(value))
}

// float32转成float64.
func (byteBuf *byteBuffer) WriteFloat32Value(key string, value float32) (n int, err error) {
	return byteBuf.WriteFloat64Value(key, float64(value))
}

func (byteBuf *byteBuffer) WriteFloat64Value(key string, value float64) (n int, err error) {
	wl, e := byteBuf.WriteStringKey(key)
	if e != nil {
		return wl, e
	}
	fl, err := byteBuf.WriteDouble(value)
	return wl + fl, err
}

func (byteBuf *byteBuffer) WriteBytesValue(key string, value []byte) (n int, err error) {
	wl, e := byteBuf.WriteStringKey(key)
	if e != nil {
		return wl, e
	}
	l, e := byteBuf.WriteBytes(value)
	return wl + l, e
}

// Only write value.

func (byteBuf *byteBuffer) WriteBool(value bool) (n int, err error) {
	if value {
		return byteBuf.buffer.Write(TrueBoolData)
	} else {
		return byteBuf.buffer.Write(FalseBoolData)
	}
}

func (byteBuf *byteBuffer) WriteIPV4(value Ipv4) (n int, err error) {
	b := Uint32ToBytes(uint32(value), byteBuf.bigEndian)
	byteBuf.pool.ipv4Header[1] = b[0]
	byteBuf.pool.ipv4Header[2] = b[1]
	byteBuf.pool.ipv4Header[3] = b[2]
	byteBuf.pool.ipv4Header[4] = b[3]
	return byteBuf.buffer.Write(byteBuf.pool.ipv4Header)
}

func (byteBuf *byteBuffer) WriteIPV6(value Ipv6) (n int, err error) {
	byteBuf.pool.ipv6Header[1] = value[0]
	byteBuf.pool.ipv6Header[2] = value[1]
	byteBuf.pool.ipv6Header[3] = value[2]
	byteBuf.pool.ipv6Header[4] = value[3]
	byteBuf.pool.ipv6Header[5] = value[4]
	byteBuf.pool.ipv6Header[6] = value[5]
	byteBuf.pool.ipv6Header[7] = value[6]
	byteBuf.pool.ipv6Header[8] = value[7]
	byteBuf.pool.ipv6Header[9] = value[8]
	byteBuf.pool.ipv6Header[10] = value[9]
	byteBuf.pool.ipv6Header[11] = value[10]
	byteBuf.pool.ipv6Header[12] = value[11]
	byteBuf.pool.ipv6Header[13] = value[12]
	byteBuf.pool.ipv6Header[14] = value[13]
	byteBuf.pool.ipv6Header[15] = value[14]
	byteBuf.pool.ipv6Header[16] = value[15]
	return byteBuf.buffer.Write(byteBuf.pool.ipv6Header)
}

func (byteBuf *byteBuffer) WriteUUid(value Uuid) (n int, err error) {
	byteBuf.pool.uuidHeader[1] = value[0]
	byteBuf.pool.uuidHeader[2] = value[1]
	byteBuf.pool.uuidHeader[3] = value[2]
	byteBuf.pool.uuidHeader[4] = value[3]
	byteBuf.pool.uuidHeader[5] = value[4]
	byteBuf.pool.uuidHeader[6] = value[5]
	byteBuf.pool.uuidHeader[7] = value[6]
	byteBuf.pool.uuidHeader[8] = value[7]
	byteBuf.pool.uuidHeader[9] = value[8]
	byteBuf.pool.uuidHeader[10] = value[9]
	byteBuf.pool.uuidHeader[11] = value[10]
	byteBuf.pool.uuidHeader[12] = value[11]
	byteBuf.pool.uuidHeader[13] = value[12]
	byteBuf.pool.uuidHeader[14] = value[13]
	byteBuf.pool.uuidHeader[15] = value[14]
	byteBuf.pool.uuidHeader[16] = value[15]
	return byteBuf.buffer.Write(byteBuf.pool.uuidHeader)
}

// go里面int统一用Long表示.
func (byteBuf *byteBuffer) WriteInt(value int) (n int, err error) {
	return byteBuf.WriteInt64(int64(value))
}

// int8,int16,int32统一转成int32.
func (byteBuf *byteBuffer) WriteInt8(value int8) (n int, err error) {
	return byteBuf.WriteInt32(int32(value))
}

func (byteBuf *byteBuffer) WriteInt16(value int16) (n int, err error) {
	return byteBuf.WriteInt32(int32(value))
}

func (byteBuf *byteBuffer) WriteInt32(value int32) (n int, err error) {
	b := Int32ToBytes(value, byteBuf.bigEndian)
	byteBuf.pool.intHeader[1] = b[0]
	byteBuf.pool.intHeader[2] = b[1]
	byteBuf.pool.intHeader[3] = b[2]
	byteBuf.pool.intHeader[4] = b[3]
	return byteBuf.buffer.Write(byteBuf.pool.intHeader)
}

func (byteBuf *byteBuffer) WriteInt64(value int64) (n int, err error) {
	b := Int64ToBytes(value, byteBuf.bigEndian)
	byteBuf.pool.longHeader[1] = b[0]
	byteBuf.pool.longHeader[2] = b[1]
	byteBuf.pool.longHeader[3] = b[2]
	byteBuf.pool.longHeader[4] = b[3]
	byteBuf.pool.longHeader[5] = b[4]
	byteBuf.pool.longHeader[6] = b[5]
	byteBuf.pool.longHeader[7] = b[6]
	byteBuf.pool.longHeader[8] = b[7]
	return byteBuf.buffer.Write(byteBuf.pool.longHeader)
}

// uint8,uint16统一转成int32.
func (byteBuf *byteBuffer) WriteUint8(value uint8) (n int, err error) {
	return byteBuf.WriteInt32(int32(value))
}

func (byteBuf *byteBuffer) WriteUint16(value uint16) (n int, err error) {
	return byteBuf.WriteInt32(int32(value))
}

// uint32转成uint64.
func (byteBuf *byteBuffer) WriteUint32(value uint32) (n int, err error) {
	return byteBuf.WriteUint64(uint64(value))
}

func (byteBuf *byteBuffer) WriteUint64(value uint64) (n int, err error) {
	b := Uint64ToBytes(value, byteBuf.bigEndian)
	byteBuf.pool.uint64Header[1] = b[0]
	byteBuf.pool.uint64Header[2] = b[1]
	byteBuf.pool.uint64Header[3] = b[2]
	byteBuf.pool.uint64Header[4] = b[3]
	byteBuf.pool.uint64Header[5] = b[4]
	byteBuf.pool.uint64Header[6] = b[5]
	byteBuf.pool.uint64Header[7] = b[6]
	byteBuf.pool.uint64Header[8] = b[7]
	return byteBuf.buffer.Write(byteBuf.pool.uint64Header)
}

func (byteBuf *byteBuffer) WriteUint(value uint) (n int, err error) {
	return byteBuf.WriteUint64(uint64(value))
}

// float32转成float64.
func (byteBuf *byteBuffer) WriteFloat32(value float32) (n int, err error) {
	return byteBuf.WriteFloat64(float64(value))
}

func (byteBuf *byteBuffer) WriteFloat64(value float64) (n int, err error) {
	b := Float64ToBytes(value, byteBuf.bigEndian)
	byteBuf.pool.doubleHeader[1] = b[0]
	byteBuf.pool.doubleHeader[2] = b[1]
	byteBuf.pool.doubleHeader[3] = b[2]
	byteBuf.pool.doubleHeader[4] = b[3]
	byteBuf.pool.doubleHeader[5] = b[4]
	byteBuf.pool.doubleHeader[6] = b[5]
	byteBuf.pool.doubleHeader[7] = b[6]
	byteBuf.pool.doubleHeader[8] = b[7]
	return byteBuf.buffer.Write(byteBuf.pool.doubleHeader)
}

func (byteBuf *byteBuffer) WriteBytes(value []byte) (n int, err error) {
	length := len(value)
	b := IntToBytes(length, byteBuf.bigEndian)
	byteBuf.pool.bytesUintHeader[0] = BytesType
	byteBuf.pool.bytesUintHeader[1] = b[0]
	byteBuf.pool.bytesUintHeader[2] = b[1]
	byteBuf.pool.bytesUintHeader[3] = b[2]
	byteBuf.pool.bytesUintHeader[4] = b[3]
	rl, err := byteBuf.buffer.Write(byteBuf.pool.bytesUintHeader)
	if err != nil {
		return rl, err
	}
	rl, err = byteBuf.buffer.Write(value)
	if err != nil {
		return rl, err
	}
	return length + 5, nil
}

// 修改byteBuf从pos开始的四个字节代表的长度.
func (byteBuf *byteBuffer) SetPosValue(pos int, value int) error {
	if pos+4 > byteBuf.Len() {
		return IndexOutOfBoundError
	}
	b := IntToBytes(value, byteBuf.bigEndian)
	byteBuf.Bytes()[pos+0] = b[0]
	byteBuf.Bytes()[pos+1] = b[1]
	byteBuf.Bytes()[pos+2] = b[2]
	byteBuf.Bytes()[pos+3] = b[3]
	return nil
}

// 修改byteBuf从pos开始的一个字节.
func (byteBuf *byteBuffer) ChangeValueByte(pos int, value byte) error {
	if pos > byteBuf.Len() {
		return IndexOutOfBoundError
	}
	byteBuf.Bytes()[pos] = value
	return nil
}

// 以下的方法返回值都是往buffer中实际写入的长度（包括type,length,data）.

func (byteBuf *byteBuffer) WriteString(s string) (n int, err error) {
	data := StringToSliceByte(s)
	sl := len(data)
	if sl <= MaxStringLength {
		return byteBuf.writeStringByte(data)
	} else {
		lb := IntToBytes(sl, byteBuf.bigEndian)
		byteBuf.pool.textHeader[1] = lb[0]
		byteBuf.pool.textHeader[2] = lb[1]
		byteBuf.pool.textHeader[3] = lb[2]
		byteBuf.pool.textHeader[4] = lb[3]
		wl, err := byteBuf.buffer.Write(byteBuf.pool.textHeader)
		if err != nil {
			return 0, err
		}
		_, err = byteBuf.buffer.Write(data)
		if err != nil {
			return 0, err
		}
		err = byteBuf.buffer.WriteByte(StringSplitByte)
		return wl + sl + 1, err
	}
}
