package serializer

type WriterBuffer interface {

	// 单写byte数组
	Write(data []byte) (n int)

	// 单独写入一个数据，没有key
	WriteValue(value interface{}) (n int, err error)

	// 获取所有写入的数据
	Bytes() []byte

	// 返回的数组为复制
	BytesCopy() []byte

	// 获取已写入数据的长度
	Len() int

	// 数据清零
	Reset()

	// 修改从post开始，4个字节表示的长度的值
	SetPosValue(pos int, value int) error

	// 写入一个keyValue,key是一个字典
	WriteDicKeyValue(key byte, value interface{}) (n int, err error)

	WriteDicIPV4Value(key byte, value Ipv4) (n int, err error)

	WriteDicIPV6Value(key byte, value Ipv6) (n int, err error)

	// uuid是128bit,及[16]byte
	WriteDicUUidValue(key byte, value Uuid) (n int, err error)

	// 写入一个keyValue,key是一个string
	WriteKeyValue(key string, value interface{}) (n int, err error)

	// 写入一个string
	WriteString(data string) (n int, err error)

	// 写入String的key, key长度<256.
	WriteStringKey(key string) (n int, err error)
}
