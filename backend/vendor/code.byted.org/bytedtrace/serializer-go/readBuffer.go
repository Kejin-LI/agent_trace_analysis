package serializer

type ReadBuffer interface {
	ReadRange(data []byte, start int, end int) (value []byte, err error)

	//read raw
	ReadByteRaw(data []byte, offset int) (value byte, readLength int, err error)

	ReadLength32(data []byte, offset int) (result int, readLength int, err error)

	ReadUint64Raw(data []byte, offset int) (result uint64, readLength int, err error)

	//read
	ReadInterface(data []byte, offset int) (value interface{}, readLength int, err error)

	ReadString(data []byte, offset int) (value string, readLength int, err error)

	ReadByte(data []byte, offset int) (value byte, readLength int, err error)

	ReadBytes(data []byte, offset int) (value []byte, readLength int, err error)

	ReadBool(data []byte, offset int) (value bool, readLength int, err error)

	ReadInt8(data []byte, offset int) (value int8, readLength int, err error)

	ReadInt16(data []byte, offset int) (value int16, readLength int, err error)

	ReadInt32(data []byte, offset int) (value int32, readLength int, err error)

	ReadInt64(data []byte, offset int) (value int64, readLength int, err error)

	ReadInt(data []byte, offset int) (value int, readLength int, err error)

	ReadUint8(data []byte, offset int) (value uint8, readLength int, err error)

	ReadUint16(data []byte, offset int) (value uint16, readLength int, err error)

	ReadUint32(data []byte, offset int) (value uint32, readLength int, err error)

	ReadUint64(data []byte, offset int) (value uint64, readLength int, err error)

	ReadUint(data []byte, offset int) (value uint, readLength int, err error)

	ReadFloat32(data []byte, offset int) (value float32, readLength int, err error)

	ReadFloat64(data []byte, offset int) (value float64, readLength int, err error)

	ReadIPV4(data []byte, offset int) (result Ipv4, readLength int, err error)

	ReadIPV6(data []byte, offset int) (result Ipv6, readLength int, err error)

	ReadUUid(data []byte, offset int) (value Uuid, readLength int, err error)

	//read value dic
	ReadInterfaceValueDic(data []byte, offset int) (keyId byte, value interface{}, readLength int, err error)

	ReadStringValueDic(data []byte, offset int) (keyId byte, value string, readLength int, err error)

	ReadByteValueDic(data []byte, offset int) (keyId byte, value byte, readLength int, err error)

	ReadBytesValueDic(data []byte, offset int) (keyId byte, value []byte, readLength int, err error)

	ReadBoolValueDic(data []byte, offset int) (keyId byte, value bool, readLength int, err error)

	ReadInt8ValueDic(data []byte, offset int) (keyId byte, value int8, readLength int, err error)

	ReadInt16ValueDic(data []byte, offset int) (keyId byte, value int16, readLength int, err error)

	ReadInt32ValueDic(data []byte, offset int) (keyId byte, value int32, readLength int, err error)

	ReadInt64ValueDic(data []byte, offset int) (keyId byte, value int64, readLength int, err error)

	ReadIntValueDic(data []byte, offset int) (keyId byte, value int, readLength int, err error)

	ReadUint8ValueDic(data []byte, offset int) (keyId byte, value uint8, readLength int, err error)

	ReadUint16ValueDic(data []byte, offset int) (keyId byte, value uint16, readLength int, err error)

	ReadUint32ValueDic(data []byte, offset int) (keyId byte, value uint32, readLength int, err error)

	ReadUint64ValueDic(data []byte, offset int) (keyId byte, value uint64, readLength int, err error)

	ReadUintValueDic(data []byte, offset int) (keyId byte, value uint, readLength int, err error)

	ReadFloat32ValueDic(data []byte, offset int) (keyId byte, value float32, readLength int, err error)

	ReadFloat64ValueDic(data []byte, offset int) (keyId byte, value float64, readLength int, err error)

	ReadIPV4ValueDic(data []byte, offset int) (keyId byte, result Ipv4, readLength int, err error)

	ReadIPV6ValueDic(data []byte, offset int) (keyId byte, result Ipv6, readLength int, err error)

	ReadUUidValueDic(data []byte, offset int) (keyId byte, value Uuid, readLength int, err error)

	//read value not dic
	ReadInterfaceValue(data []byte, offset int) (key string, value interface{}, readLength int, err error)

	ReadStringValue(data []byte, offset int) (key string, value string, readLength int, err error)

	ReadByteValue(data []byte, offset int) (key string, value byte, readLength int, err error)

	ReadBytesValue(data []byte, offset int) (key string, value []byte, readLength int, err error)

	ReadBoolValue(data []byte, offset int) (key string, value bool, readLength int, err error)

	ReadInt8Value(data []byte, offset int) (key string, value int8, readLength int, err error)

	ReadInt16Value(data []byte, offset int) (key string, value int16, readLength int, err error)

	ReadInt32Value(data []byte, offset int) (key string, value int32, readLength int, err error)

	ReadInt64Value(data []byte, offset int) (key string, value int64, readLength int, err error)

	ReadIntValue(data []byte, offset int) (key string, value int, readLength int, err error)

	ReadUint8Value(data []byte, offset int) (key string, value uint8, readLength int, err error)

	ReadUint16Value(data []byte, offset int) (key string, value uint16, readLength int, err error)

	ReadUint32Value(data []byte, offset int) (key string, value uint32, readLength int, err error)

	ReadUint64Value(data []byte, offset int) (key string, value uint64, readLength int, err error)

	ReadUintValue(data []byte, offset int) (key string, value uint, readLength int, err error)

	ReadFloat32Value(data []byte, offset int) (key string, value float32, readLength int, err error)

	ReadFloat64Value(data []byte, offset int) (key string, value float64, readLength int, err error)

	ReadIPV4Value(data []byte, offset int) (key string, result Ipv4, readLength int, err error)

	ReadIPV6Value(data []byte, offset int) (key string, result Ipv6, readLength int, err error)

	ReadUUidValue(data []byte, offset int) (key string, value Uuid, readLength int, err error)
}
