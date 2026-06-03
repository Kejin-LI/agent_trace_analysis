package serializer

type Deserializer interface {
	// 读入数据,解析协议头，如果协议头解析错误，直接报错
	Read(data []byte) error

	// 获取私有协议头
	GetOptions() (Header, error)

	GetTotalLength() (int, error)

	GetPatterns() ([]string, error)

	GetCommonHeaders() ([]KeyValue, error)

	GetLogHeaders() (logHeader []LogHeader, err error)

	GetLogContents() (contents []LogContent, err error)
}
