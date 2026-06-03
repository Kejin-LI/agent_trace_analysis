package codec

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

type VarString struct {
	length  uint8
	content string
}

const hextable = "0123456789abcdef"

func EncodeUint8(buf []byte, i uint8) []byte {
	return append(buf, i)
}

func EncodeUint16(buf []byte, i uint16) []byte {
	return append(buf, byte(i), byte(i>>8))
}

func EncodeUint32(buf []byte, i uint32) []byte {
	return append(buf, byte(i), byte(i>>8), byte(i>>16), byte(i>>24))
}

func EncodeUint64(buf []byte, i uint64) []byte {
	return append(buf, byte(i), byte(i>>8), byte(i>>16), byte(i>>24),
		byte(i>>32), byte(i>>40), byte(i>>48), byte(i>>56))
}

func WriteUint8(buf []byte, pos int, value uint8) {
	buf[pos] = value
}

func WriteUint64(buf []byte, pos int, value uint64) {
	_ = buf[pos+7]
	buf[pos+0] = byte(value)
	buf[pos+1] = byte(value >> 8)
	buf[pos+2] = byte(value >> 16)
	buf[pos+3] = byte(value >> 24)
	buf[pos+4] = byte(value >> 32)
	buf[pos+5] = byte(value >> 40)
	buf[pos+6] = byte(value >> 48)
	buf[pos+7] = byte(value >> 56)
}

func WriteUint32(buf []byte, pos int, value uint32) {
	_ = buf[pos+3]
	buf[pos+0] = byte(value)
	buf[pos+1] = byte(value >> 8)
	buf[pos+2] = byte(value >> 16)
	buf[pos+3] = byte(value >> 24)
}

func WriteUint16(buf []byte, pos int, value uint16) {
	_ = buf[pos+1]
	buf[pos+0] = byte(value)
	buf[pos+1] = byte(value >> 8)
}

func WriteUint64Hex(buf []byte, pos int, value uint64) {
	_ = buf[pos+15]
	j := 0
	for j < 8 {
		v := byte(value >> (j * 8))
		buf[pos+(7-j)*2] = hextable[v>>4]
		buf[pos+(7-j)*2+1] = hextable[v&0x0f]
		j += 1
	}
}

func WriteBytes(buf []byte, pos int, value []byte, len int) {
	_, _ = value[len-1], buf[pos+len-1]
	for i := 0; i < len; i++ {
		buf[pos+i] = value[i]
	}
}

func EncodeKeyValueStr(buf []byte, k, v string) []byte {
	buf = encodeStr(buf, k)
	buf = encodeStr(buf, v)
	return buf
}

// EncodeKeyValue encodes a key-value pair.
// The value should be a string, a text, or byte[]
func EncodeKeyValue(buf []byte, k string, v []byte, valueType byte) []byte {
	buf = encodeStr(buf, k)
	buf = encodeVarWithType(buf, v, valueType)
	return buf
}

func EncodeKeyValueUint64(buf []byte, k string, value uint64) []byte {
	buf = encodeStr(buf, k)
	buf = encodeUint64(buf, value)
	return buf
}

// EncodedStringSize computes the actual size of a string after encoding.
func EncodedStringSize(s string) int {
	strLen := len(s)
	if strLen > SHORT_STRING_MAX_LEN {
		return encodedLongStringSize(s)
	} else {
		return encodedShortStringSize(s)
	}
}

// EncodedKVSizeStr computes the actual size of a key-value pair after encoding.
// But the value must be a string, e.g., {"_psm", "toutiao.service.log"}
func EncodedKVSizeStr(k, v string) int {
	return EncodedStringSize(k) + EncodedStringSize(v)
}

// EncodedKVIPv4Size computes the actual size of a key-value pair when the value is ipv4.
// | {Key} | length | ipv4
func EncodedKVIPv4Size(key string) int {
	return EncodedStringSize(key) + 1 + BYTELOG_IPV4_BYTES
}

// EncodedKVIPv6Size computes the actual size of a key-value pair when the value is ipv6.
// | {Key} | length| ipv6
func EncodedKVIPv6Size(key string) int {
	return EncodedStringSize(key) + 1 + BYTELOG_IPV6_BYTES
}

// EncodedKVIBatchIDSize computes the actual size of a key-value pair when the value is batchid
func EncodedKVIBatchIDSize(key string) int {
	return EncodedStringSize(key) + 3 + BATCH_ID_LENGTH
}

// EncodedKVFlagSize computes the actual size of a key-value pair when the value is ipv4.
// | {Key} | length | ipv4
func EncodedKVFlagSize(key string) int {
	return EncodedStringSize(key) + 1 + BYTELOG_FLAG_BYTES
}

/*
 * All decode functions will return the instance.
 * For decodedData with fixed size, it may also return an error.
 * For decodedData with uncertain length, it also returns the readLength value, which means how many bytes it processed.
 * Then we can process the remaining bytes from there.
 */

func DecodeUint8(data []byte) uint8 {
	return data[0]
}

func DecodeUint16(data []byte) uint16 {
	_ = data[1]
	return uint16(data[0]) | uint16(data[1])<<8
}

func DecodeUint32(data []byte) uint32 {
	_ = data[3]
	return uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
}

func DecodeUint64(b []byte) uint64 {
	_ = b[7]
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}

func StringToUint32(s string) (uint32, error) {
	return DecodeUint32(StringToSliceByte(s)), nil
}

func StringToSliceByte(s string) []byte {
	l := len(s)
	return *(*[]byte)(unsafe.Pointer(&reflect.SliceHeader{
		Data: (*(*reflect.StringHeader)(unsafe.Pointer(&s))).Data,
		Len:  l,
		Cap:  l,
	}))
}

func SliceByteToString(buf []byte) string {
	l := len(buf)
	return *(*string)(unsafe.Pointer(&reflect.StringHeader{
		Data: (*(*reflect.SliceHeader)(unsafe.Pointer(&buf))).Data,
		Len:  l,
	}))
}

func ValueSize(o interface{}) int {
	switch v := o.(type) {
	case nil:
		return EncodedStringSize("")
	case fmt.Stringer:
		value := reflect.ValueOf(o)
		if !value.IsValid() || value.Kind() == reflect.Ptr && value.IsNil() {
			valueStr := fmt.Sprintf("%#v", o)
			return EncodedStringSize(valueStr)
		}
		return EncodedStringSize(v.String())
	case string:
		return EncodedStringSize(v)
	case error:
		value := reflect.ValueOf(o)
		if !value.IsValid() || value.Kind() == reflect.Ptr && value.IsNil() {
			valueStr := fmt.Sprintf("%#v", o)
			return EncodedStringSize(valueStr)
		}
		return EncodedStringSize(v.Error())
	case bool:
		return 1 + 1
	case int8:
		return 4 + 1
	case int16:
		return 4 + 1
	case uint8:
		return 4 + 1
	case uint16:
		return 4 + 1
	case int32:
		return 4 + 1
	case int:
		return 8 + 1
	case int64:
		return 8 + 1
	case uint32:
		return 8 + 1
	case uint64:
		return 8 + 1
	case float32:
		return 8 + 1
	case float64:
		return 8 + 1
	default:
		byteBuf := make([]byte, 0, 32)
		tmpBuf := (*jsonBuf)(unsafe.Pointer(&byteBuf))
		e := json.NewEncoder(tmpBuf)
		e.SetEscapeHTML(false)
		err := e.Encode(v)
		if err != nil {
			valueStr := fmt.Sprintf("%#v", o)
			return EncodedStringSize(valueStr)
		}
		return EncodedStringSize(SliceByteToString(tmpBuf.buf))
	}
}

// ValueToBytes encodes an interface to bytes.
// It also puts the type byte in front of the data bytes.
func ValueToBytes(buf []byte, o interface{}) []byte {
	switch v := o.(type) {
	case nil:
		return append(buf, emptyStrForByteLog...)
	case fmt.Stringer:
		value := reflect.ValueOf(o)
		var valueStr string
		if !value.IsValid() || value.Kind() == reflect.Ptr && value.IsNil() {
			valueStr = fmt.Sprintf("%#v", o)
			return encodeStr(buf, valueStr)
		}
		valueStr = v.String()
		return encodeStr(buf, valueStr)
	case string:
		return encodeStr(buf, v)
	case error:
		value := reflect.ValueOf(o)
		if !value.IsValid() || value.Kind() == reflect.Ptr && value.IsNil() {
			valueStr := fmt.Sprintf("%#v", o)
			return encodeStr(buf, valueStr)
		}
		return encodeStr(buf, v.Error())
	case bool:
		return encodeBool(buf, v)
	case int8:
		return encodeInt(buf, uint32(v))
	case int16:
		return encodeInt(buf, uint32(v))
	case int32:
		return encodeInt(buf, uint32(v))
	case uint8:
		return encodeInt(buf, uint32(v))
	case uint16:
		return encodeInt(buf, uint32(v))
	case int:
		return encodeLong(buf, uint64(v))
	case int64:
		return encodeLong(buf, uint64(v))
	case uint32:
		return encodeUint64(buf, uint64(v))
	case uint64:
		return encodeUint64(buf, v)
	case float32:
		return encodeDouble(buf, float64(v))
	case float64:
		return encodeDouble(buf, v)
	default:
		byteBuf := make([]byte, 0, 32)
		tmpBuf := (*jsonBuf)(unsafe.Pointer(&byteBuf))
		e := json.NewEncoder(tmpBuf)
		e.SetEscapeHTML(false)
		err := e.Encode(v)
		if err != nil {
			valueStr := fmt.Sprintf("%#v", o)
			return encodeStr(buf, valueStr)
		}
		if len(tmpBuf.buf) > SHORT_STRING_MAX_LEN {
			return encodeVarWithType(buf, tmpBuf.buf, TextType)
		}
		return encodeVarWithType(buf, tmpBuf.buf, StringType)
	}
}

// ValuesToBytes converts some objects to bytes and append them to the buf.
// It calls ValueToBytes indeed.
func ValuesToBytes(buf []byte, os ...interface{}) []byte {
	for _, o := range os {
		buf = ValueToBytes(buf, o)
	}
	return buf
}

// encodeVarWithType is a helper function that directly write the type and bytes to buf.
// If we only know the type, it still writes some default bytes.
func encodeVarWithType(buf []byte, value []byte, valueType byte) []byte {
	if valueType == StringType {
		return encodeShortStr(buf, SliceByteToString(value))
	} else if valueType == TextType {
		return encodeLongStr(buf, SliceByteToString(value))
	} else {
		buf = append(buf, valueType)
		switch valueType {
		case BoolType:
			if value == nil || len(value) < 1 {
				return EncodeUint8(buf, 0)
			}
			return append(buf, value[:1]...)
		case IntType:
			if value == nil || len(value) < 4 {
				return EncodeUint32(buf, 0)
			}
			return append(buf, value[:4]...)
		case LongType:
			if value == nil || len(value) < 8 {
				return EncodeUint64(buf, 0)

			}
			return append(buf, value[:8]...)
		case Uint64Type:
			if value == nil || len(value) < 8 {
				return EncodeUint64(buf, 0)
			}
			return append(buf, value[:8]...)
		case DoubleType:
			if value == nil || len(value) < 8 {
				return EncodeUint64(buf, 0)
			}
			return append(buf, value[:8]...)
		default:
			return append(buf, value...)
		}
	}
}

func encodeStr(buf []byte, s string) []byte {
	if len(s) <= math.MaxUint8 {
		return encodeShortStr(buf, s)
	} else {
		return encodeLongStr(buf, s)
	}
}

// encodeShortStr encodes a short string whose length is <= 255.
// Codec scheme of short string is as follows.
// Fields:     | type | length | value | delimiter |
// num of bytes:  1      1         n        1
func encodeShortStr(buf []byte, s string) []byte {
	// TODO: check how to encode empty string

	if len(s) >= math.MaxUint8 {
		s = s[:math.MaxUint8]
	}

	buf = append(buf, StringType)
	buf = EncodeUint8(buf, uint8(len(s)))
	buf = append(buf, s...)
	buf = append(buf, StringSplitByte)
	return buf
}

// encodeLongStr encodes a long string which is also called text.
// Codec scheme of a long string is as follows.
// Fields:     | type | length | value | delimiter |
// num of bytes:  1      4         n        1
func encodeLongStr(buf []byte, s string) []byte {
	if len(s) >= math.MaxInt32 {
		s = s[:math.MaxInt32]
	}
	buf = append(buf, TextType)
	buf = EncodeUint32(buf, uint32(len(s)))
	buf = append(buf, s...)
	buf = append(buf, StringSplitByte)
	return buf
}

// encodeVarStr encodes a VarString instance: {length: uint8, content string}.
// The length should be 1 + len(content). There is No '\0' at the end of the content.
func encodeVarStr(buf []byte, vs VarString) []byte {
	if len(vs.content) > math.MaxInt8 {
		vs.content = vs.content[:math.MaxInt8]
	}
	buf = EncodeUint8(buf, uint8(len(vs.content)))
	buf = append(buf, vs.content...)
	return buf
}

func encodeUint64(buf []byte, value uint64) []byte {
	buf = append(buf, Uint64Type)
	return EncodeUint64(buf, value)
}

func encodeInt(buf []byte, value uint32) []byte {
	buf = append(buf, IntType)
	return EncodeUint32(buf, value)
}

func encodeLong(buf []byte, value uint64) []byte {
	buf = append(buf, LongType)
	return EncodeUint64(buf, value)
}

func encodeDouble(buf []byte, value float64) []byte {
	buf = encodeVarWithType(buf, nil, DoubleType)
	WriteUint64(buf, len(buf)-8, math.Float64bits(value))
	return buf
}

func encodeBool(buf []byte, value bool) []byte {
	buf = append(buf, BoolType)
	if value {
		return EncodeUint8(buf, 1)
	}
	return EncodeUint8(buf, 0)
}

// encodedShortStringSize computes the actual size of a short string after encoding.
// Codec scheme of short string is as follows.
// Fields:     | type | length | value | delimiter |
// num of bytes:  1      1         n        1
func encodedShortStringSize(s string) int {
	strLen := len(s)
	if strLen > SHORT_STRING_MAX_LEN {
		strLen = SHORT_STRING_MAX_LEN
	}
	return strLen + 3
}

// encodedLongStringSize computes the actual size of a long string after encoding.
// Codec scheme of short string is as follows.
// Fields:     | type | length | value | delimiter |
// num of bytes:  1      4         n        1
func encodedLongStringSize(s string) int {
	strLen := len(s)
	if strLen > LONG_STRING_MAX_LEN {
		strLen = LONG_STRING_MAX_LEN
	}
	return strLen + 6
}

func usToMinute(timeStampInUs uint64) uint64 {
	return timeStampInUs / uint64(time.Minute/time.Microsecond)
}

type jsonBuf struct {
	buf []byte
}

func (b *jsonBuf) Write(p []byte) (n int, err error) {
	b.buf = append(b.buf, p...)
	return len(p), err
}

// KeyValue represents a key-value pair.
// The key must be a string and the value type may be string, int, or float. The value data is stored in bytes.
// For available types, please check https://code.byted.org/bytedtrace/serializer-go/blob/master/types.go
type KeyValue struct {
	Key       string
	Value     []byte
	ValueType byte
	isLong    bool
	refCount  int64
}

// NewKeyValue creates a key-value instance. It also does some necessary checks and encodes the value.
// This KeyValue is short by default: The lengths of the key and value cannot exceed 0xFF.
// You can pass an extra bool argument to mark it as a long KV.
func NewKeyValue(key, value interface{}, isLong ...bool) (*KeyValue, error) {
	isLongKV := false
	if len(isLong) > 0 && isLong[0] {
		isLongKV = true
	}
	kv := keyValuePool.Get().(*KeyValue)
	atomic.StoreInt64(&kv.refCount, 1)
	kv.isLong = isLongKV

	switch v := key.(type) {
	case string:
		kv.Key = v
	case fmt.Stringer:
		kv.Key = v.String()
	default:
		kv.Recycle()
		return nil, fmt.Errorf("invalid key: %v", key)
	}

	switch v := value.(type) {
	case string:
		kv.Value = append(kv.Value, v...)
		kv.ValueType = StringType
		if len(kv.Value) > math.MaxUint8 {
			kv.ValueType = TextType
		}
	case fmt.Stringer:
		kv.Value = append(kv.Value, v.String()...)
		kv.ValueType = StringType
		if len(kv.Value) > math.MaxUint8 {
			kv.ValueType = TextType
		}
	case bool:
		kv.ValueType = BoolType
		if v {
			kv.Value = EncodeUint8(kv.Value, 1)
		} else {
			kv.Value = EncodeUint8(kv.Value, 0)
		}
	case int8:
		kv.ValueType = IntType
		kv.Value = EncodeUint32(kv.Value, uint32(v))
	case int16:
		kv.ValueType = IntType
		kv.Value = EncodeUint32(kv.Value, uint32(v))
	case int32:
		kv.ValueType = IntType
		kv.Value = EncodeUint32(kv.Value, uint32(v))
	case uint8:
		kv.ValueType = IntType
		kv.Value = EncodeUint32(kv.Value, uint32(v))
	case uint16:
		kv.ValueType = IntType
		kv.Value = EncodeUint32(kv.Value, uint32(v))
	case int:
		kv.ValueType = LongType
		kv.Value = EncodeUint64(kv.Value, uint64(v))
	case int64:
		kv.ValueType = LongType
		kv.Value = EncodeUint64(kv.Value, uint64(v))
	case uint32:
		kv.ValueType = Uint64Type
		kv.Value = EncodeUint64(kv.Value, uint64(v))
	case uint:
		kv.ValueType = Uint64Type
		kv.Value = EncodeUint64(kv.Value, uint64(v))
	case uint64:
		kv.ValueType = Uint64Type
		kv.Value = EncodeUint64(kv.Value, v)
	case float32:
		kv.ValueType = DoubleType
		kv.Value = EncodeUint64(kv.Value, math.Float64bits(float64(v)))
	case float64:
		kv.ValueType = DoubleType
		kv.Value = EncodeUint64(kv.Value, math.Float64bits(v))
	default:
		kv.Recycle()
		return nil, fmt.Errorf("invalid key: %v", value)
	}

	// Check whether the short kv is too long. If it is too long, then trim the key and value.
	if !kv.isLong {
		if len(kv.Key) > SHORT_STRING_MAX_LEN {
			kv.Key = kv.Key[:SHORT_STRING_MAX_LEN]
		}
		if len(kv.Value) > SHORT_STRING_MAX_LEN {
			kv.Value = kv.Value[:SHORT_STRING_MAX_LEN]
			if kv.ValueType == TextType {
				kv.ValueType = StringType
			}
		}
	}

	return kv, nil
}

// NewOmniKeyValue create a KeyValue instance for any type of key and value.
func NewOmniKeyValue(key, value interface{}) *KeyValue {
	kv := keyValuePool.Get().(*KeyValue)
	atomic.StoreInt64(&kv.refCount, 1)
	kv.isLong = true

	switch v := key.(type) {
	case string:
		kv.Key = v
	case fmt.Stringer:
		kv.Key = v.String()
	default:
		byteBuf := make([]byte, 0, 32)
		tmpBuf := (*jsonBuf)(unsafe.Pointer(&byteBuf))
		e := json.NewEncoder(tmpBuf)
		e.SetEscapeHTML(false)
		err := e.Encode(v)
		if err != nil {
			kv.Key = fmt.Sprintf("%#v", v)
		}
		kv.Key = string(byteBuf[:len(byteBuf)-1])
	}

	switch v := value.(type) {
	case string:
		kv.Value = append(kv.Value, v...)
		kv.ValueType = StringType
		if len(kv.Value) > math.MaxUint8 {
			kv.ValueType = TextType
		}
	case fmt.Stringer:
		kv.Value = append(kv.Value, v.String()...)
		kv.ValueType = StringType
		if len(kv.Value) > math.MaxUint8 {
			kv.ValueType = TextType
		}
	case bool:
		kv.ValueType = BoolType
		if v {
			kv.Value = EncodeUint8(kv.Value, 1)
		} else {
			kv.Value = EncodeUint8(kv.Value, 0)
		}
	case int8:
		kv.ValueType = IntType
		kv.Value = EncodeUint32(kv.Value, uint32(v))
	case int16:
		kv.ValueType = IntType
		kv.Value = EncodeUint32(kv.Value, uint32(v))
	case int32:
		kv.ValueType = IntType
		kv.Value = EncodeUint32(kv.Value, uint32(v))
	case uint8:
		kv.ValueType = IntType
		kv.Value = EncodeUint32(kv.Value, uint32(v))
	case uint16:
		kv.ValueType = IntType
		kv.Value = EncodeUint32(kv.Value, uint32(v))
	case int:
		kv.ValueType = LongType
		kv.Value = EncodeUint64(kv.Value, uint64(v))
	case int64:
		kv.ValueType = LongType
		kv.Value = EncodeUint64(kv.Value, uint64(v))
	case uint32:
		kv.ValueType = Uint64Type
		kv.Value = EncodeUint64(kv.Value, uint64(v))
	case uint:
		kv.ValueType = Uint64Type
		kv.Value = EncodeUint64(kv.Value, uint64(v))
	case uint64:
		kv.ValueType = Uint64Type
		kv.Value = EncodeUint64(kv.Value, v)
	case float32:
		kv.ValueType = DoubleType
		kv.Value = EncodeUint64(kv.Value, math.Float64bits(float64(v)))
	case float64:
		kv.ValueType = DoubleType
		kv.Value = EncodeUint64(kv.Value, math.Float64bits(v))
	default:
		kv.ValueType = StringType
		byteBuf := make([]byte, 0, 32)
		tmpBuf := (*jsonBuf)(unsafe.Pointer(&byteBuf))
		e := json.NewEncoder(tmpBuf)
		e.SetEscapeHTML(false)
		err := e.Encode(v)
		if err != nil {
			kv.Value = append(kv.Value, fmt.Sprintf("%#v", v)...)
		} else {
			kv.Value = append(kv.Value, byteBuf[:len(byteBuf)-1]...)
		}
		if len(kv.Value) > SHORT_STRING_MAX_LEN {
			kv.ValueType = TextType
		}
	}

	return kv
}

func (kv *KeyValue) IsLong() bool {
	return kv.isLong
}

func (kv *KeyValue) Encode(buf []byte) []byte {
	return EncodeKeyValue(buf, kv.Key, kv.Value, kv.ValueType)
}

func (kv *KeyValue) Size() int {
	switch kv.ValueType {
	case TextType:
		return EncodedKVSizeStr(kv.Key, SliceByteToString(kv.Value))
	case StringType:
		return EncodedKVSizeStr(kv.Key, SliceByteToString(kv.Value))
	default:
		return EncodedStringSize(kv.Key) + len(kv.Value) + 1
	}
}

// String() returns a string "{key}={value}".
func (kv *KeyValue) String() string {
	var valueStr string
	switch kv.ValueType {
	case BoolType:
		if len(kv.Value) > 0 && kv.Value[0] == 1 {
			valueStr = "true"
		} else {
			valueStr = "false"
		}
	case IntType:
		valInt := DecodeUint32(kv.Value)
		valueStr = strconv.FormatInt(int64(valInt), 10)
	case LongType:
		valLong := DecodeUint64(kv.Value)
		valueStr = strconv.FormatInt(int64(valLong), 10)
	case Uint64Type:
		valUint64 := DecodeUint64(kv.Value)
		valueStr = strconv.FormatUint(valUint64, 10)
	case DoubleType:
		valInt64 := DecodeUint64(kv.Value)
		valDouble := math.Float64frombits(valInt64)
		valueStr = strconv.FormatFloat(valDouble, 'f', -1, 64)
	default:
		valueStr = string(kv.Value)
	}
	return kv.Key + "=" + valueStr
}

// ToKV returns a key-value pair
func (kv *KeyValue) ToKV() (string, string) {
	var valueStr string
	switch kv.ValueType {
	case BoolType:
		if len(kv.Value) > 0 && kv.Value[0] == 1 {
			valueStr = "true"
		} else {
			valueStr = "false"
		}
	case IntType:
		valInt := DecodeUint32(kv.Value)
		valueStr = strconv.FormatInt(int64(valInt), 10)
	case LongType:
		valLong := DecodeUint64(kv.Value)
		valueStr = strconv.FormatInt(int64(valLong), 10)
	case Uint64Type:
		valUint64 := DecodeUint64(kv.Value)
		valueStr = strconv.FormatUint(valUint64, 10)
	case DoubleType:
		valInt64 := DecodeUint64(kv.Value)
		valDouble := math.Float64frombits(valInt64)
		valueStr = strconv.FormatFloat(valDouble, 'f', -1, 64)
	default:
		valueStr = string(kv.Value)
	}
	return kv.Key, valueStr
}

func (kv *KeyValue) Clone() *KeyValue {
	atomic.AddInt64(&kv.refCount, 1)
	return kv
}

func (kv *KeyValue) Recycle() {
	if atomic.CompareAndSwapInt64(&kv.refCount, 1, 0) {
		kv.Key = ""
		kv.Value = kv.Value[:0]
		kv.ValueType = 0
		kv.isLong = false
		keyValuePool.Put(kv)
	} else {
		atomic.AddInt64(&kv.refCount, -1)
	}
}

var keyValuePool = &sync.Pool{
	New: func() interface{} {
		return &KeyValue{
			Key:       "",
			Value:     make([]byte, 0, 16),
			ValueType: 0,
			isLong:    false,
			refCount:  0,
		}
	},
}
