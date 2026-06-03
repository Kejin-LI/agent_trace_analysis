package serializer

type (
	Ipv4 uint32
	Ipv6 [16]byte
	Uuid [16]byte
)

var (
	EmptyIpv4 = Ipv4(0)
	EmptyIpv6 = Ipv6{}
	EmptyUuid = Uuid{}
)

const (
	// 私有协议内部用
	StringType byte = 0 //长度小于255的string
	BoolType   byte = 1
	IntType    byte = 2  //4字节, 8,16,32位有符号,byte,8,16位无符号
	LongType   byte = 3  //8字节, 64位有符号,int
	Uint64Type byte = 4  //8字节, 32,64位无符号
	DoubleType byte = 5  //8字节, float32,float64
	DateType   byte = 20 //暂不支持
	Ipv4Type   byte = 21
	Ipv6Type   byte = 22
	TextType   byte = 23 //长度大于255的string
	BytesType  byte = 24 //二进制字符流
	UUIdType   byte = 25 //128位
)
