package serializer

import (
	"encoding/binary"
	"math"
	"math/big"
	"net"
	"reflect"
	"unsafe"
)

// 将int转成[]byte,返回4位,即认为int型的数据不会大于32,为了节省存储考虑.
func IntToBytes(value int, bigEndian bool) []byte {
	return Uint32ToBytes(uint32(value), bigEndian)
}

func BytesToInt(data []byte, bigEndian bool) int {
	return int(BytesToUint32(data, bigEndian))
}

// 将int32转成[]byte,返回4位.
func Int32ToBytes(value int32, bigEndian bool) []byte {
	return Uint32ToBytes(uint32(value), bigEndian)
}

func BytesToInt32(data []byte, bigEndian bool) int32 {
	return int32(BytesToUint32(data, bigEndian))
}

// 将int64转成[]byte,返回8位.
func Int64ToBytes(value int64, bigEndian bool) []byte {
	bs := make([]byte, 8)
	if bigEndian {
		binary.BigEndian.PutUint64(bs, uint64(value))
	} else {
		binary.LittleEndian.PutUint64(bs, uint64(value))
	}
	return bs
}

func BytesToInt64(bs []byte, bigEndian bool) int64 {
	if bigEndian {
		return int64(binary.BigEndian.Uint64(bs))
	} else {
		return int64(binary.LittleEndian.Uint64(bs))
	}
}

// 将uint32转成[]byte,返回4位.
func Uint32ToBytes(value uint32, bigEndian bool) []byte {
	bs := make([]byte, 4)
	if bigEndian {
		binary.BigEndian.PutUint32(bs, value)
	} else {
		binary.LittleEndian.PutUint32(bs, value)
	}
	return bs
}

func BytesToUint32(data []byte, bigEndian bool) uint32 {
	if bigEndian {
		return binary.BigEndian.Uint32(data)
	} else {
		return binary.LittleEndian.Uint32(data)
	}
}

// 将uint64转成[]byte,返回8位.
func Uint64ToBytes(value uint64, bigEndian bool) []byte {
	bs := make([]byte, 8)
	if bigEndian {
		binary.BigEndian.PutUint64(bs, value)
	} else {
		binary.LittleEndian.PutUint64(bs, value)
	}
	return bs
}

func BytesToUint64(bs []byte, bigEndian bool) uint64 {
	if bigEndian {
		return binary.BigEndian.Uint64(bs)
	} else {
		return binary.LittleEndian.Uint64(bs)
	}
}

func Float64ToBytes(value float64, bigEndian bool) []byte {
	bits := math.Float64bits(value)
	bs := make([]byte, 8)
	if bigEndian {
		binary.BigEndian.PutUint64(bs, bits)
	} else {
		binary.LittleEndian.PutUint64(bs, bits)
	}
	return bs
}

func BytesToFloat64(data []byte, bigEndian bool) float64 {
	if bigEndian {
		bit := binary.BigEndian.Uint64(data)
		return math.Float64frombits(bit)
	} else {
		bit := binary.LittleEndian.Uint64(data)
		return math.Float64frombits(bit)
	}
}

func InetAtoN(ip string) int64 {
	ret := big.NewInt(0)
	b := net.ParseIP(ip).To16()
	ret.SetBytes(b)
	return ret.Int64()
}

func truncatedString(data string) string {
	if len(data) > 256 {
		return data[:256]
	}
	return data
}

func StringToSliceByte(s string) []byte {
	l := len(s)
	return *(*[]byte)(unsafe.Pointer(&reflect.SliceHeader{
		Data: (*(*reflect.StringHeader)(unsafe.Pointer(&s))).Data,
		Len:  l,
		Cap:  l,
	}))
}
