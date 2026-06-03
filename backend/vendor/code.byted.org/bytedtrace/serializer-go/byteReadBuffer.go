package serializer

type byteReadBuffer struct {
	bigEndian bool
}

func NewByteReadBuffer(bigEndian bool) ReadBuffer {
	return &byteReadBuffer{
		bigEndian: bigEndian,
	}
}

// 读取offset开始的一字节
func (b *byteReadBuffer) ReadByteRaw(data []byte, offset int) (result byte, readLength int, err error) {
	// todo 判断是否需要加1
	if len(data) <= offset {
		return 0, 0, OutOfBufferBoundError
	}
	return data[offset], 1, nil
}

// 读取8位表示的长度
func (b *byteReadBuffer) readLength8(data []byte, offset int) (result int, readLength int, err error) {
	v, rl, e := b.ReadByteRaw(data, offset)
	return int(v), rl, e
}

// 读取32位表示的长度
func (b *byteReadBuffer) ReadLength32(data []byte, offset int) (result int, readLength int, err error) {
	if len(data) < offset+4 {
		return 0, 0, OutOfBufferBoundError
	}
	return BytesToInt(data[offset:offset+4], b.bigEndian), 4, nil
}

func (b *byteReadBuffer) ReadUint64Raw(data []byte, offset int) (result uint64, readLength int, err error) {
	if len(data) < offset+8 {
		return 0, 0, OutOfBufferBoundError
	}
	return BytesToUint64(data[offset:offset+8], b.bigEndian), 8, nil
}

func (b *byteReadBuffer) readFloat64Raw(data []byte, offset int) (result float64, readLength int, err error) {
	if len(data) < offset+8 {
		return 0, 0, OutOfBufferBoundError
	}
	return BytesToFloat64(data[offset:offset+8], b.bigEndian), 8, nil
}

// 读取 data[start:end] 的数据
func (b *byteReadBuffer) ReadRange(data []byte, start int, end int) (value []byte, err error) {
	if end < start || end > len(data) {
		return nil, OutOfBufferBoundError
	}
	return data[start:end], nil
}

func (b *byteReadBuffer) checkType(data []byte, offset int, t byte) (c bool, readLength int, err error) {
	v, rl, e := b.ReadByteRaw(data, offset)
	if e != nil {
		return false, rl, e
	}
	if v == t {
		return true, rl, nil
	}
	return false, rl, nil
}

// 检查数据类型是否相符,valueType上一个字节是keyId
func (b *byteReadBuffer) checkTypeWithKeyId(data []byte, offset int, t byte) (c bool, keyId byte, realType byte, readLength int, err error) {
	if len(data) < offset+2 {
		return false, 0, 0, 0, OutOfBufferBoundError
	}
	keyId = data[offset]
	realType = data[offset+1]
	if realType != t {
		return false, keyId, realType, 2, nil
	}
	return true, keyId, realType, 2, nil
}

//检查数据类型是否相符,没有keyId
func (b *byteReadBuffer) checkTypeWithKey(data []byte, offset int, t byte) (c bool, key string, realType byte, readLength int, err error) {
	key, readLength, err = b.ReadString(data, offset)
	if err != nil {
		return false, key, 0, readLength, err
	}
	offset = offset + readLength
	realType, rl, err := b.ReadByteRaw(data, offset)
	if err != nil {
		return false, key, realType, rl + readLength, err
	}
	return realType == t, key, realType, rl + readLength, nil
}

func (b *byteReadBuffer) readInt64Raw(data []byte, offset int) (result int64, readLength int, err error) {
	if len(data) < offset+8 {
		return 0, 0, OutOfBufferBoundError
	}
	return BytesToInt64(data[offset:offset+8], b.bigEndian), 8, nil
}

func (b *byteReadBuffer) ReadInterface(data []byte, offset int) (value interface{}, readLength int, err error) {
	t, rl, e := b.ReadByteRaw(data, offset)
	if e != nil {
		return nil, rl, e
	}
	return b.readInterface(data, offset+rl, t)
}

func (b *byteReadBuffer) readInterface(data []byte, offset int, t byte) (value interface{}, readLength int, err error) {
	switch t {
	case StringType:
		return b.readString(data, offset, StringType)
	case BoolType:
		return b.readBool(data, offset)
	case IntType:
		return b.readInt32(data, offset)
	case LongType:
		return b.readInt64Raw(data, offset)
	case Uint64Type:
		return b.ReadUint64Raw(data, offset)
	case DoubleType:
		return b.readFloat64Raw(data, offset)
	case Ipv4Type:
		return b.readIPV4(data, offset)
	case Ipv6Type:
		return b.readIPV6(data, offset)
	case TextType:
		return b.readString(data, offset, TextType)
	case BytesType:
		return b.readBytes(data, offset)
	case UUIdType:
		return b.readUUid(data, offset)
	default:
		return nil, 0, NotSupportValueTypeError
	}
}

func (b *byteReadBuffer) ReadString(data []byte, offset int) (value string, readLength int, err error) {
	t, readLength, err := b.ReadByteRaw(data, offset)
	if err != nil {
		return "", readLength, err
	}
	if t != StringType && t != TextType {
		return "", readLength, NotApplyTypeError
	}
	v, rl, err := b.readString(data, offset+readLength, t)
	return v, rl + readLength, err
}

func (b *byteReadBuffer) readString(data []byte, offset int, t byte) (value string, readLength int, err error) {
	length := 0
	if t == StringType {
		length, readLength, err = b.readLength8(data, offset)
	} else if t == TextType {
		length, readLength, err = b.ReadLength32(data, offset)
	}
	if err != nil {
		return "", readLength, err
	}
	offset = offset + readLength
	if len(data) < offset+length {
		return "", 0, OutOfBufferBoundError
	}
	return string(data[offset : offset+length]), readLength + length + 1, nil
}

func (b *byteReadBuffer) ReadByte(data []byte, offset int) (value byte, readLength int, err error) {
	d, readLength, e := b.ReadInt32(data, offset)
	return byte(d), readLength, e
}

func (b *byteReadBuffer) ReadBytes(data []byte, offset int) (value []byte, readLength int, err error) {
	v, readLength, e := b.checkType(data, offset, BytesType)
	if e != nil {
		return nil, readLength, e
	}
	if !v {
		return nil, readLength, NotApplyTypeError
	}
	bs, rl, e := b.readBytes(data, offset+readLength)
	return bs, rl + readLength, e
}

func (b *byteReadBuffer) readBytes(data []byte, offset int) (value []byte, readLength int, err error) {
	length, readLength, err := b.ReadLength32(data, offset)
	if err != nil {
		return nil, readLength, err
	}
	offset = offset + readLength
	v, e := b.ReadRange(data, offset, offset+length)
	return v, readLength + length, e
}

func (b *byteReadBuffer) ReadBool(data []byte, offset int) (value bool, readLength int, err error) {
	v, readLength, e := b.checkType(data, offset, BoolType)
	if e != nil {
		return false, readLength, e
	}
	if !v {
		return false, readLength, NotApplyTypeError
	}
	v, rl, e := b.readBool(data, offset+readLength)
	return v, rl + readLength, e
}

func (b *byteReadBuffer) readBool(data []byte, offset int) (value bool, readLength int, err error) {
	br, readLength, err := b.ReadByteRaw(data, offset)
	if err != nil {
		return false, readLength, err
	}
	if br == TrueByte {
		return true, readLength, nil
	} else if br == FalseByte {
		return false, readLength, nil
	}
	return false, readLength, NotApplyTypeError
}

func (b *byteReadBuffer) ReadInt8(data []byte, offset int) (value int8, readLength int, err error) {
	r, readLength, e := b.ReadInt32(data, offset)
	return int8(r), readLength, e
}

func (b *byteReadBuffer) ReadInt16(data []byte, offset int) (value int16, readLength int, err error) {
	r, readLength, e := b.ReadInt32(data, offset)
	return int16(r), readLength, e
}

func (b *byteReadBuffer) ReadInt32(data []byte, offset int) (value int32, readLength int, err error) {
	v, readLength, e := b.checkType(data, offset, IntType)
	if e != nil {
		return 0, 0, e
	}
	if !v {
		return 0, 0, NotApplyTypeError
	}
	r, rl, e := b.readInt32(data, readLength+offset)
	return r, readLength + rl, e
}

func (b *byteReadBuffer) readInt32(data []byte, offset int) (value int32, readLength int, err error) {
	if len(data) < offset+4 {
		return 0, 0, OutOfBufferBoundError
	}
	return BytesToInt32(data[offset:offset+4], b.bigEndian), 4, nil
}

func (b *byteReadBuffer) ReadInt64(data []byte, offset int) (value int64, readLength int, err error) {
	v, readLength, e := b.checkType(data, offset, LongType)
	if e != nil {
		return 0, readLength, e
	}
	if !v {
		return 0, readLength, NotApplyTypeError
	}
	result, rl, e := b.readInt64Raw(data, offset+readLength)
	return result, rl + readLength, e
}

func (b *byteReadBuffer) ReadInt(data []byte, offset int) (value int, readLength int, err error) {
	v, readLength, e := b.readInt64Raw(data, offset)
	return int(v), readLength, e
}

func (b *byteReadBuffer) ReadUint8(data []byte, offset int) (value uint8, readLength int, err error) {
	r, readLength, e := b.ReadInt32(data, offset)
	return uint8(r), readLength, e
}

func (b *byteReadBuffer) ReadUint16(data []byte, offset int) (value uint16, readLength int, err error) {
	r, readLength, e := b.ReadInt32(data, offset)
	return uint16(r), readLength, e
}

func (b *byteReadBuffer) ReadUint32(data []byte, offset int) (value uint32, readLength int, err error) {
	r, readLength, e := b.ReadUint64(data, offset)
	return uint32(r), readLength, e
}

func (b *byteReadBuffer) ReadUint64(data []byte, offset int) (value uint64, readLength int, err error) {
	v, readLength, e := b.checkType(data, offset, Uint64Type)
	if e != nil {
		return 0, readLength, e
	}
	if !v {
		return 0, readLength, NotApplyTypeError
	}
	value, rl, e := b.ReadUint64Raw(data, offset+readLength)
	return value, rl + readLength, e
}

func (b *byteReadBuffer) ReadUint(data []byte, offset int) (value uint, readLength int, err error) {
	r, readLength, e := b.ReadUint64(data, offset)
	return uint(r), readLength, e
}

func (b *byteReadBuffer) ReadFloat32(data []byte, offset int) (value float32, readLength int, err error) {
	r, readLength, e := b.ReadFloat64(data, offset)
	return float32(r), readLength, e
}

func (b *byteReadBuffer) ReadFloat64(data []byte, offset int) (value float64, readLength int, err error) {
	v, readLength, e := b.checkType(data, offset, DoubleType)
	if e != nil {
		return 0, readLength, e
	}
	if !v {
		return 0, readLength, NotApplyTypeError
	}
	result, l, e := b.readFloat64Raw(data, offset+readLength)
	return result, l + readLength, e
}

func (b *byteReadBuffer) ReadIPV4(data []byte, offset int) (result Ipv4, readLength int, err error) {
	v, readLength, e := b.checkType(data, offset, Ipv4Type)
	if e != nil {
		return EmptyIpv4, readLength, e
	}
	if !v {
		return EmptyIpv4, readLength, NotApplyTypeError
	}
	result, rl, e := b.readIPV4(data, offset+readLength)
	return result, rl + readLength, e
}

func (b *byteReadBuffer) readIPV4(data []byte, offset int) (result Ipv4, readLength int, err error) {
	v, rl, e := b.ReadLength32(data, offset)
	return Ipv4(v), rl, e
}

func (b *byteReadBuffer) ReadIPV6(data []byte, offset int) (result Ipv6, readLength int, err error) {
	v, readLength, e := b.checkType(data, offset, Ipv6Type)
	if e != nil {
		return EmptyIpv6, readLength, e
	}
	if !v {
		return EmptyIpv6, readLength, NotApplyTypeError
	}
	result, rl, e := b.readIPV6(data, offset+readLength)
	return result, rl + readLength, e
}

func (b *byteReadBuffer) readIPV6(data []byte, offset int) (result Ipv6, readLength int, err error) {
	var ipv6 = EmptyIpv6
	if len(data) < offset+16 {
		return ipv6, 0, OutOfBufferBoundError
	}
	copy(ipv6[:], data[offset:offset+16])
	return ipv6, 16, nil
}

func (b *byteReadBuffer) ReadUUid(data []byte, offset int) (value Uuid, readLength int, err error) {
	v, readLength, e := b.checkType(data, offset, UUIdType)
	if e != nil {
		return EmptyUuid, readLength, e
	}
	if !v {
		return EmptyUuid, readLength, NotApplyTypeError
	}
	result, rl, e := b.readUUid(data, offset+readLength)
	return result, rl + readLength, e
}

func (b *byteReadBuffer) readUUid(data []byte, offset int) (value Uuid, readLength int, err error) {
	var uuid = EmptyUuid
	if len(data) < offset+16 {
		return uuid, 0, OutOfBufferBoundError
	}
	copy(uuid[:], data[offset:offset+16])
	return uuid, 16, nil
}

//read value dic
func (b *byteReadBuffer) ReadStringValueDic(data []byte, offset int) (keyId byte, value string, readLength int, err error) {
	v, keyId, t, readLength, e := b.checkTypeWithKeyId(data, offset, StringType)
	if e != nil {
		return keyId, "", readLength, e
	}
	if !v {
		return keyId, "", readLength, NotApplyTypeError
	}
	result, rl, e := b.readString(data, offset+readLength, t)
	return keyId, result, rl + readLength, e
}

func (b *byteReadBuffer) ReadByteValueDic(data []byte, offset int) (keyId byte, value byte, readLength int, err error) {
	keyId, result, readLength, e := b.ReadInt32ValueDic(data, offset)
	return keyId, byte(result), readLength, e
}

func (b *byteReadBuffer) ReadBytesValueDic(data []byte, offset int) (keyId byte, value []byte, readLength int, err error) {
	v, keyId, _, readLength, e := b.checkTypeWithKeyId(data, offset, BytesType)
	if e != nil {
		return keyId, nil, readLength, e
	}
	if !v {
		return keyId, nil, readLength, NotApplyTypeError
	}
	result, rl, e := b.readBytes(data, offset+readLength)
	return keyId, result, rl + readLength, e
}

func (b *byteReadBuffer) ReadBoolValueDic(data []byte, offset int) (keyId byte, value bool, readLength int, err error) {
	v, keyId, _, readLength, e := b.checkTypeWithKeyId(data, offset, BoolType)
	if e != nil {
		return keyId, false, readLength, e
	}
	if !v {
		return keyId, false, readLength, NotApplyTypeError
	}
	result, rl, e := b.readBool(data, offset+readLength)
	return keyId, result, rl + readLength, e
}

func (b *byteReadBuffer) ReadInt8ValueDic(data []byte, offset int) (keyId byte, value int8, readLength int, err error) {
	keyId, result, readLength, e := b.ReadInt32ValueDic(data, offset)
	return keyId, int8(result), readLength, e
}

func (b *byteReadBuffer) ReadInt16ValueDic(data []byte, offset int) (keyId byte, value int16, readLength int, err error) {
	keyId, result, readLength, e := b.ReadInt32ValueDic(data, offset)
	return keyId, int16(result), readLength, e
}

func (b *byteReadBuffer) ReadInt32ValueDic(data []byte, offset int) (keyId byte, value int32, readLength int, err error) {
	v, keyId, _, readLength, e := b.checkTypeWithKeyId(data, offset, IntType)
	if e != nil {
		return keyId, 0, readLength, e
	}
	if !v {
		return keyId, 0, readLength, NotApplyTypeError
	}
	result, rl, e := b.readInt32(data, offset+readLength)
	return keyId, int32(result), rl + readLength, e
}

func (b *byteReadBuffer) ReadInt64ValueDic(data []byte, offset int) (keyId byte, value int64, readLength int, err error) {
	v, keyId, _, readLength, e := b.checkTypeWithKeyId(data, offset, LongType)
	if e != nil {
		return keyId, 0, readLength, e
	}
	if !v {
		return keyId, 0, readLength, NotApplyTypeError
	}
	result, rl, e := b.readInt64Raw(data, offset+readLength)
	return keyId, result, rl + readLength, e
}

func (b *byteReadBuffer) ReadIntValueDic(data []byte, offset int) (keyId byte, value int, readLength int, err error) {
	keyId, result, readLength, e := b.ReadInt64ValueDic(data, offset)
	return keyId, int(result), readLength, e
}

func (b *byteReadBuffer) ReadUint8ValueDic(data []byte, offset int) (keyId byte, value uint8, readLength int, err error) {
	keyId, result, readLength, e := b.ReadInt32ValueDic(data, offset)
	return keyId, uint8(result), readLength, e
}

func (b *byteReadBuffer) ReadUint16ValueDic(data []byte, offset int) (keyId byte, value uint16, readLength int, err error) {
	keyId, result, readLength, e := b.ReadInt32ValueDic(data, offset)
	return keyId, uint16(result), readLength, e
}

func (b *byteReadBuffer) ReadUint32ValueDic(data []byte, offset int) (keyId byte, value uint32, readLength int, err error) {
	v, keyId, _, readLength, e := b.checkTypeWithKeyId(data, offset, Uint64Type)
	if e != nil {
		return keyId, 0, readLength, e
	}
	if !v {
		return keyId, 0, readLength, NotApplyTypeError
	}
	result, rl, e := b.ReadUint64Raw(data, offset+readLength)
	return keyId, uint32(result), rl + readLength, e
}

func (b *byteReadBuffer) ReadUint64ValueDic(data []byte, offset int) (keyId byte, value uint64, readLength int, err error) {
	v, keyId, _, readLength, e := b.checkTypeWithKeyId(data, offset, Uint64Type)
	if e != nil {
		return keyId, 0, readLength, e
	}
	if !v {
		return keyId, 0, readLength, NotApplyTypeError
	}
	result, rl, e := b.ReadUint64Raw(data, offset+readLength)
	return keyId, result, rl + readLength, e
}

func (b *byteReadBuffer) ReadUintValueDic(data []byte, offset int) (keyId byte, value uint, readLength int, err error) {
	v, keyId, _, readLength, e := b.checkTypeWithKeyId(data, offset, Uint64Type)
	if e != nil {
		return keyId, 0, readLength, e
	}
	if !v {
		return keyId, 0, readLength, NotApplyTypeError
	}
	result, rl, e := b.ReadUint64Raw(data, offset+readLength)
	return keyId, uint(result), rl + readLength, e
}

func (b *byteReadBuffer) ReadFloat32ValueDic(data []byte, offset int) (keyId byte, value float32, readLength int, err error) {
	v, keyId, _, readLength, e := b.checkTypeWithKeyId(data, offset, DoubleType)
	if e != nil {
		return keyId, 0, readLength, e
	}
	if !v {
		return keyId, 0, readLength, NotApplyTypeError
	}
	result, rl, e := b.readFloat64Raw(data, offset+readLength)
	return keyId, float32(result), rl + readLength, e
}

func (b *byteReadBuffer) ReadFloat64ValueDic(data []byte, offset int) (keyId byte, value float64, readLength int, err error) {
	v, keyId, _, readLength, e := b.checkTypeWithKeyId(data, offset, DoubleType)
	if e != nil {
		return keyId, 0, readLength, e
	}
	if !v {
		return keyId, 0, readLength, NotApplyTypeError
	}
	result, rl, e := b.readFloat64Raw(data, offset+readLength)
	return keyId, result, rl + readLength, e
}

func (b *byteReadBuffer) ReadIPV4ValueDic(data []byte, offset int) (keyId byte, result Ipv4, readLength int, err error) {
	v, keyId, _, readLength, e := b.checkTypeWithKeyId(data, offset, Ipv4Type)
	if e != nil {
		return keyId, EmptyIpv4, readLength, e
	}
	if !v {
		return keyId, EmptyIpv4, readLength, NotApplyTypeError
	}
	result, rl, e := b.readIPV4(data, offset+readLength)
	return keyId, result, readLength + rl, e
}

func (b *byteReadBuffer) ReadIPV6ValueDic(data []byte, offset int) (keyId byte, result Ipv6, readLength int, err error) {
	v, keyId, _, readLength, e := b.checkTypeWithKeyId(data, offset, Ipv6Type)
	if e != nil {
		return keyId, EmptyIpv6, readLength, e
	}
	if !v {
		return keyId, EmptyIpv6, readLength, NotApplyTypeError
	}
	result, rl, e := b.readIPV6(data, offset+readLength)
	return keyId, result, readLength + rl, e
}

func (b *byteReadBuffer) ReadUUidValueDic(data []byte, offset int) (keyId byte, value Uuid, readLength int, err error) {
	v, keyId, _, readLength, e := b.checkTypeWithKeyId(data, offset, UUIdType)
	if e != nil {
		return keyId, EmptyUuid, readLength, e
	}
	if !v {
		return keyId, EmptyUuid, readLength, NotApplyTypeError
	}
	result, rl, e := b.readUUid(data, offset+readLength)
	return keyId, result, rl + readLength, e
}

func (b *byteReadBuffer) ReadInterfaceValueDic(data []byte, offset int) (keyId byte, value interface{}, readLength int, err error) {
	if len(data) < offset+2 {
		return 0, nil, 0, OutOfBufferBoundError
	}
	keyId = data[offset]
	t := data[offset+1]
	v, rl, e := b.readInterface(data, offset+2, t)
	return keyId, v, rl + 2, e
}

// read value not dic
func (b *byteReadBuffer) ReadStringValue(data []byte, offset int) (key string, value string, readLength int, err error) {
	v, key, t, readLength, e := b.checkTypeWithKey(data, offset, StringType)
	if e != nil {
		return key, "", readLength, e
	}
	if !v {
		return key, "", readLength, NotApplyTypeError
	}
	result, rl, e := b.readString(data, offset+readLength, t)
	return key, result, rl + readLength, e
}

func (b *byteReadBuffer) ReadByteValue(data []byte, offset int) (key string, value byte, readLength int, err error) {
	key, result, rl, e := b.ReadInt32Value(data, offset+readLength)
	return key, byte(result), rl + readLength, e
}

func (b *byteReadBuffer) ReadBytesValue(data []byte, offset int) (key string, value []byte, readLength int, err error) {
	v, key, _, readLength, e := b.checkTypeWithKey(data, offset, BytesType)
	if e != nil {
		return key, nil, readLength, e
	}
	if !v {
		return key, nil, readLength, NotApplyTypeError
	}
	result, rl, e := b.readBytes(data, offset+readLength)
	return key, result, rl + readLength, e
}

func (b *byteReadBuffer) ReadBoolValue(data []byte, offset int) (key string, value bool, readLength int, err error) {
	v, key, _, readLength, e := b.checkTypeWithKey(data, offset, BoolType)
	if e != nil {
		return key, false, readLength, e
	}
	if !v {
		return key, false, readLength, NotApplyTypeError
	}
	result, rl, e := b.readBool(data, offset+readLength)
	return key, result, rl + readLength, e
}

func (b *byteReadBuffer) ReadInt8Value(data []byte, offset int) (key string, value int8, readLength int, err error) {
	key, result, readLength, err := b.ReadInt32Value(data, offset)
	return key, int8(result), readLength, err
}

func (b *byteReadBuffer) ReadInt16Value(data []byte, offset int) (key string, value int16, readLength int, err error) {
	key, result, readLength, err := b.ReadInt32Value(data, offset)
	return key, int16(result), readLength, err
}

func (b *byteReadBuffer) ReadInt32Value(data []byte, offset int) (key string, value int32, readLength int, err error) {
	v, key, _, readLength, e := b.checkTypeWithKey(data, offset, IntType)
	if e != nil {
		return key, 0, readLength, e
	}
	if !v {
		return key, 0, readLength, NotApplyTypeError
	}
	result, rl, e := b.readInt32(data, offset+readLength)
	return key, int32(result), rl + readLength, e
}

func (b *byteReadBuffer) ReadInt64Value(data []byte, offset int) (key string, value int64, readLength int, err error) {
	v, key, _, readLength, e := b.checkTypeWithKey(data, offset, LongType)
	if e != nil {
		return key, 0, readLength, e
	}
	if !v {
		return key, 0, readLength, NotApplyTypeError
	}
	result, rl, e := b.readInt64Raw(data, offset+readLength)
	return key, result, rl + readLength, e
}

func (b *byteReadBuffer) ReadIntValue(data []byte, offset int) (key string, value int, readLength int, err error) {
	key, result, readLength, e := b.ReadInt64Value(data, offset)
	return key, int(result), readLength, e
}

func (b *byteReadBuffer) ReadUint8Value(data []byte, offset int) (key string, value uint8, readLength int, err error) {
	key, result, readLength, e := b.ReadInt32Value(data, offset)
	return key, uint8(result), readLength, e
}

func (b *byteReadBuffer) ReadUint16Value(data []byte, offset int) (key string, value uint16, readLength int, err error) {
	key, result, readLength, e := b.ReadInt32Value(data, offset)
	return key, uint16(result), readLength, e
}

func (b *byteReadBuffer) ReadUint32Value(data []byte, offset int) (key string, value uint32, readLength int, err error) {
	v, key, _, readLength, e := b.checkTypeWithKey(data, offset, Uint64Type)
	if e != nil {
		return key, 0, readLength, e
	}
	if !v {
		return key, 0, readLength, NotApplyTypeError
	}
	result, rl, e := b.ReadUint64Raw(data, offset+readLength)
	return key, uint32(result), rl + readLength, e
}

func (b *byteReadBuffer) ReadUint64Value(data []byte, offset int) (key string, value uint64, readLength int, err error) {
	v, key, _, readLength, e := b.checkTypeWithKey(data, offset, Uint64Type)
	if e != nil {
		return key, 0, readLength, e
	}
	if !v {
		return key, 0, readLength, NotApplyTypeError
	}
	result, rl, e := b.ReadUint64Raw(data, offset+readLength)
	return key, result, rl + readLength, e
}

func (b *byteReadBuffer) ReadUintValue(data []byte, offset int) (key string, value uint, readLength int, err error) {
	v, key, _, readLength, e := b.checkTypeWithKey(data, offset, Uint64Type)
	if e != nil {
		return key, 0, readLength, e
	}
	if !v {
		return key, 0, readLength, NotApplyTypeError
	}
	result, rl, e := b.ReadUint64Raw(data, offset+readLength)
	return key, uint(result), rl + readLength, e
}

func (b *byteReadBuffer) ReadFloat32Value(data []byte, offset int) (key string, value float32, readLength int, err error) {
	v, key, _, readLength, e := b.checkTypeWithKey(data, offset, DoubleType)
	if e != nil {
		return key, 0, readLength, e
	}
	if !v {
		return key, 0, readLength, NotApplyTypeError
	}
	result, rl, e := b.readFloat64Raw(data, offset+readLength)
	return key, float32(result), rl + readLength, e
}

func (b *byteReadBuffer) ReadFloat64Value(data []byte, offset int) (key string, value float64, readLength int, err error) {
	v, key, _, readLength, e := b.checkTypeWithKey(data, offset, DoubleType)
	if e != nil {
		return key, 0, readLength, e
	}
	if !v {
		return key, 0, readLength, NotApplyTypeError
	}
	result, rl, e := b.readFloat64Raw(data, offset+readLength)
	return key, result, rl + readLength, e
}

func (b *byteReadBuffer) ReadIPV4Value(data []byte, offset int) (key string, result Ipv4, readLength int, err error) {
	v, key, _, readLength, e := b.checkTypeWithKey(data, offset, Ipv4Type)
	if e != nil {
		return key, EmptyIpv4, readLength, e
	}
	if !v {
		return key, EmptyIpv4, readLength, NotApplyTypeError
	}
	result, rl, e := b.readIPV4(data, offset+readLength)
	return key, result, readLength + rl, e
}

func (b *byteReadBuffer) ReadIPV6Value(data []byte, offset int) (key string, result Ipv6, readLength int, err error) {
	v, key, _, readLength, e := b.checkTypeWithKey(data, offset, Ipv6Type)
	if e != nil {
		return key, EmptyIpv6, readLength, e
	}
	if !v {
		return key, EmptyIpv6, readLength, NotApplyTypeError
	}
	result, rl, e := b.readIPV6(data, offset+readLength)
	return key, result, readLength + rl, e
}

func (b *byteReadBuffer) ReadUUidValue(data []byte, offset int) (key string, value Uuid, readLength int, err error) {
	v, key, _, readLength, e := b.checkTypeWithKey(data, offset, UUIdType)
	if e != nil {
		return key, EmptyUuid, readLength, e
	}
	if !v {
		return key, EmptyUuid, readLength, NotApplyTypeError
	}
	result, rl, e := b.readUUid(data, offset+readLength)
	return key, result, rl + readLength, e
}

func (b *byteReadBuffer) ReadInterfaceValue(data []byte, offset int) (key string, value interface{}, readLength int, err error) {
	key, readLength, err = b.ReadString(data, offset)
	if err != nil {
		return key, nil, readLength, err
	}
	offset = offset + readLength
	realType, rbl, err := b.ReadByteRaw(data, offset)
	if err != nil {
		return key, nil, rbl + readLength, err
	}
	v, rl, e := b.readInterface(data, offset+rbl, realType)
	return key, v, rl + rbl + readLength, e
}
