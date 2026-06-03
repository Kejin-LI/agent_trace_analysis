package serializer

import (
	"code.byted.org/bytedtrace/serializer-go/compress"
	"errors"
)

var (
	TotalLengthVerificationError = errors.New("total length verification failed")
	EmptyDataError               = errors.New("data is empty")
	ErrorDataError               = errors.New("data is error")
	ContentLengthError           = errors.New("content length is error")
	DeCompressError              = errors.New("decompress error")
	OutOfSliceError              = errors.New("out of slice bound")
)

// 记录各个模块的长度和起始位置.
type globalStack struct {
	init bool

	// 全局的header起始offset,总是0
	globalHeaderOffset int

	// 全局的数据大小
	totalLength int

	// header区域压缩后的长度
	headerRegionCompressLength int
	// header区域压缩前的长度
	headerRegionOriginLength int

	// logContent区域压缩后的长度
	logContentsCompressLength int
	// logContent区域的长度
	logContentsOriginLength int
	// logContent起始offset
	logContentsOffset int
}

func (g *globalStack) isInit() bool {
	return g.init
}

func (g *globalStack) reset() {
	g.init = false
}

func (g *globalStack) setInit() {
	g.init = true
}

type headerStack struct {
	init bool
	// pattern区域的长度
	patternLength int
	// pattern起始offset
	patternOffset int
	// commonHeader区域的长度
	commonHeadersLength int
	// commonHeader起始offset
	commonHeadersOffset int
	// logHeader区域的长度
	logHeadersLength int
	// logHeader起始offset
	logHeadersOffset int
}

func (h *headerStack) isInit() bool {
	return h.init
}

func (h *headerStack) reset() {
	h.init = false
}

func (h *headerStack) setInit() {
	h.init = true
}

type flowDeserializer struct {
	compressors *compress.Compressors
	stack       globalStack
	headerStack headerStack
	buffer      ReadBuffer
	dictionary  Dictionary

	//data
	originData            []byte
	headerDeCompressData  []byte
	contentDeCompressData []byte

	bigEndian        bool
	headerOptionFlow HeaderOptionFlow
	patterns         []string
	commonHeaders    []KeyValue
	logHeaders       []LogHeader
	logContents      []LogContent
}

func NewDeserializer(serializerType string, bigEndian bool) Deserializer {
	if serializerType == FlowDeSerializerType {
		fr := &flowDeserializer{
			buffer:      NewByteReadBuffer(bigEndian),
			dictionary:  NewDictionary(),
			bigEndian:   bigEndian,
			compressors: compress.GetCompressors(),
		}
		return fr
	}
	return nil
}

func (f *flowDeserializer) readByte(offset int, data []byte) (byte, error) {
	if len(data) < offset+1 {
		return 0, OutOfSliceError
	}
	return data[offset], nil
}

func (f *flowDeserializer) reset() {
	f.headerDeCompressData = nil
	f.contentDeCompressData = nil
	f.patterns = nil
	f.commonHeaders = nil
	f.logHeaders = nil
	f.logContents = nil
	f.headerStack.reset()
	f.stack.reset()
	f.dictionary.Reset()
}

// 读入数据.
func (f *flowDeserializer) Read(data []byte) error {
	if len(data) == 0 {
		return EmptyDataError
	}
	f.reset()
	f.originData = data
	return f.verify()
}

// 处理header的stack,同时得到pattern字典.
func (f *flowDeserializer) genHeaderStack() error {
	if f.headerStack.isInit() {
		return nil
	}
	f.headerStack.setInit()
	offset := 24

	var headerData []byte
	if *f.headerOptionFlow.Compression != compress.None {
		if f.headerDeCompressData == nil {
			data, e := f.buffer.ReadRange(f.originData, offset, offset+f.stack.headerRegionCompressLength)
			if e != nil {
				return e
			}
			dcp, e := f.compressors.DeCompress(*f.headerOptionFlow.Compression, data)
			if e != nil {
				return e
			}
			if len(dcp) != f.stack.headerRegionOriginLength {
				return DeCompressError
			}
			f.headerDeCompressData = dcp
		}
		headerData = f.headerDeCompressData
		offset = 0
	} else {
		// 没有压缩，直接在originData上处理
		headerData = f.originData
	}

	patternLength, rl, e := f.buffer.ReadLength32(headerData, offset)
	if e != nil {
		return e
	}
	f.headerStack.patternOffset = offset + rl
	f.headerStack.patternLength = patternLength

	patterns, e := f.GetPatterns()
	if e != nil {
		return e
	}
	e = f.dictionary.Build(patterns)
	if e != nil {
		return e
	}

	offset = f.headerStack.patternOffset + patternLength

	commonHeaderLength, rl, e := f.buffer.ReadLength32(headerData, offset)
	if e != nil {
		return e
	}
	f.headerStack.commonHeadersOffset = offset + rl
	f.headerStack.commonHeadersLength = commonHeaderLength
	offset = f.headerStack.commonHeadersOffset + commonHeaderLength

	logHeadersLength, rl, e := f.buffer.ReadLength32(headerData, offset)
	if e != nil {
		return e
	}
	f.headerStack.logHeadersOffset = offset + rl
	f.headerStack.logHeadersLength = logHeadersLength
	return nil
}

// 处理content,如果有压缩,则解压.
func (f *flowDeserializer) genContent() error {
	if *f.headerOptionFlow.ContentCompression != compress.None {
		if f.contentDeCompressData == nil {
			offset := f.stack.logContentsOffset
			data, e := f.buffer.ReadRange(f.originData, offset, offset+f.stack.logContentsCompressLength)
			if e != nil {
				return e
			}
			dcp, e := f.compressors.DeCompress(*f.headerOptionFlow.ContentCompression, data)
			if e != nil {
				return e
			}
			if len(dcp) != f.stack.logContentsOriginLength {
				return DeCompressError
			}
			f.contentDeCompressData = dcp
		}
	}
	return nil
}

// 全局校验,确定全局的header,header区域的压缩前后大小,content区域压缩前后大小.
func (f *flowDeserializer) verify() error {
	if len(f.originData) < 24 {
		return ErrorDataError
	}

	offset := 0

	// todo 直接读下标
	version, rl, e := f.buffer.ReadByteRaw(f.originData, offset)
	if e != nil {
		return e
	}
	f.headerOptionFlow.Version = &version
	offset = offset + rl

	protoType, rl, e := f.buffer.ReadByteRaw(f.originData, offset)
	if e != nil {
		return e
	}
	f.headerOptionFlow.ProtoType = &protoType
	offset = offset + rl

	compression, rl, e := f.buffer.ReadByteRaw(f.originData, offset)
	if e != nil {
		return e
	}
	f.headerOptionFlow.Compression = &compression
	offset = offset + rl

	reserved, rl, e := f.buffer.ReadByteRaw(f.originData, offset)
	if e != nil {
		return e
	}
	f.headerOptionFlow.Reserved = &reserved
	offset = offset + rl

	// 校验totalLength是否合法
	totalLength, rl, e := f.buffer.ReadLength32(f.originData, offset)
	if e != nil {
		return e
	}
	if totalLength != len(f.originData)-24 {
		return TotalLengthVerificationError
	}
	f.stack.totalLength = totalLength
	offset = offset + rl
	// 不论是否压缩,headerCompressLength都是实际的长度
	headerCompressLength, rl, e := f.buffer.ReadLength32(f.originData, offset)
	if e != nil {
		return e
	}
	f.stack.headerRegionCompressLength = headerCompressLength
	offset = offset + rl

	headerOriginLength, rl, e := f.buffer.ReadLength32(f.originData, offset)
	if e != nil {
		return e
	}
	f.stack.headerRegionOriginLength = headerOriginLength
	offset = offset + rl
	// userId
	userId, rl, e := f.buffer.ReadUint64Raw(f.originData, offset)
	if e != nil {
		return e
	}
	f.headerOptionFlow.UserId = &userId

	offset = offset + rl + headerCompressLength
	contentCompressionLen, rl, e := f.buffer.ReadLength32(f.originData, offset)
	if e != nil {
		return e
	}
	f.stack.logContentsCompressLength = contentCompressionLen
	offset = offset + rl

	contentOriginLen, rl, e := f.buffer.ReadLength32(f.originData, offset)
	if e != nil {
		return e
	}
	f.stack.logContentsOriginLength = contentOriginLen
	offset = offset + rl

	contentCompression, rl, e := f.buffer.ReadByteRaw(f.originData, offset)
	if e != nil {
		return e
	}
	f.headerOptionFlow.ContentCompression = &contentCompression

	f.stack.logContentsOffset = offset + rl
	if f.stack.logContentsOffset > len(f.originData) {
		return ContentLengthError
	}
	f.stack.setInit()
	return nil
}

func (f *flowDeserializer) GetOptions() (Header, error) {
	return &f.headerOptionFlow, nil
}

func (f *flowDeserializer) GetTotalLength() (int, error) {
	return f.stack.totalLength, nil
}

func (f *flowDeserializer) GetPatterns() ([]string, error) {
	if f.patterns != nil {
		return f.patterns, nil
	}
	e := f.genPattern()
	return f.patterns, e
}

func (f *flowDeserializer) genPattern() error {
	if e := f.genHeaderStack(); e != nil {
		return e
	}
	patternLength := f.headerStack.patternLength
	if patternLength == 0 {
		return nil
	}
	offset := f.headerStack.patternOffset
	readLength := 0
	var data []byte
	if *f.headerOptionFlow.Compression != compress.None {
		data = f.headerDeCompressData
	} else {
		data = f.originData
	}
	for {
		if readLength >= patternLength {
			break
		}
		v, rl, e := f.buffer.ReadString(data, offset)
		if e != nil {
			return e
		}
		readLength = readLength + rl
		offset = offset + rl
		f.patterns = append(f.patterns, v)
	}
	return nil
}

func (f *flowDeserializer) GetCommonHeaders() ([]KeyValue, error) {
	if f.commonHeaders != nil {
		return f.commonHeaders, nil
	}
	e := f.genCommonHeaders()
	return f.commonHeaders, e
}

func (f *flowDeserializer) genCommonHeaders() error {
	if e := f.genHeaderStack(); e != nil {
		return e
	}
	offset := f.headerStack.commonHeadersOffset
	commonHeadersLength := f.headerStack.commonHeadersLength

	if commonHeadersLength == 0 {
		f.commonHeaders = make([]KeyValue, 0)
		return nil
	}

	var data []byte
	if *f.headerOptionFlow.Compression != compress.None {
		data = f.headerDeCompressData
	} else {
		data = f.originData
	}

	readLength := 0
	// 如果使用字典
	if f.headerStack.patternLength != 0 {
		for {
			if readLength >= commonHeadersLength {
				break
			}
			keyId, value, rl, e := f.buffer.ReadInterfaceValueDic(data, offset)
			if e != nil {
				return e
			}
			key, e := f.dictionary.Decode(keyId)
			if e != nil {
				return e
			}
			readLength = readLength + rl
			offset = offset + rl
			f.commonHeaders = append(f.commonHeaders, KeyValue{
				Key:   key,
				Value: value,
			})
		}
	} else {
		for {
			if readLength >= commonHeadersLength {
				break
			}
			key, value, rl, e := f.buffer.ReadInterfaceValue(data, offset)
			if e != nil {
				return e
			}
			readLength = readLength + rl
			offset = offset + rl
			f.commonHeaders = append(f.commonHeaders, KeyValue{
				Key:   key,
				Value: value,
			})
		}
	}

	return nil
}

func (f *flowDeserializer) GetLogHeaders() (logHeader []LogHeader, err error) {
	if f.logHeaders != nil {
		return f.logHeaders, nil
	}
	e := f.genLogHeaders()
	return f.logHeaders, e
}

func (f *flowDeserializer) genLogHeaders() error {
	if e := f.genHeaderStack(); e != nil {
		return e
	}
	offset := f.headerStack.logHeadersOffset
	logHeadersLength := f.headerStack.logHeadersLength

	if logHeadersLength == 0 {
		f.logHeaders = make([]LogHeader, 0)
		return nil
	}

	var data []byte
	if *f.headerOptionFlow.Compression != compress.None {
		data = f.headerDeCompressData
	} else {
		data = f.originData
	}

	readLength := 0
	for {
		if readLength >= logHeadersLength {
			break
		}
		header := LogHeader{}
		reserveHeader := &LogHeaderOptionFlow{}
		headerLength, rl, e := f.buffer.ReadLength32(data, offset)
		if e != nil {
			return e
		}
		readLength += rl
		// 这里设定headerLength一定不为0
		offset = offset + rl
		readL := 0

		// reserveHeader
		timestamp, rl, e := f.buffer.ReadUint64(data, offset)
		if e != nil {
			return e
		}
		offset = offset + rl
		readL = readL + rl
		reserveHeader.Timestamp = &timestamp

		source, rl, e := f.buffer.ReadString(data, offset)
		if e != nil {
			return e
		}
		offset = offset + rl
		readL = readL + rl
		reserveHeader.Source = &source

		context, rl, e := f.buffer.ReadString(data, offset)
		if e != nil {
			return e
		}
		offset = offset + rl
		readL = readL + rl
		reserveHeader.Context = &context

		logId, rl, e := f.buffer.ReadString(data, offset)
		if e != nil {
			return e
		}
		offset = offset + rl
		readL = readL + rl
		reserveHeader.LogId = &logId
		header.ReserveHeader = reserveHeader
		if f.headerStack.patternLength != 0 {
			for {
				if readL >= headerLength {
					break
				}
				// CustomizeHeader
				keyId, value, rl, e := f.buffer.ReadInterfaceValueDic(data, offset)
				if e != nil {
					return e
				}
				key, e := f.dictionary.Decode(keyId)
				if e != nil {
					return e
				}
				readL = readL + rl
				offset = offset + rl
				header.CustomizeHeader = append(header.CustomizeHeader, KeyValue{
					Key:   key,
					Value: value,
				})
			}
		} else {
			for {
				if readL >= headerLength {
					break
				}
				// CustomizeHeader
				key, value, rl, e := f.buffer.ReadInterfaceValue(data, offset)
				if e != nil {
					return e
				}
				readL = readL + rl
				offset = offset + rl
				header.CustomizeHeader = append(header.CustomizeHeader, KeyValue{
					Key:   key,
					Value: value,
				})
			}
		}

		readLength = readLength + readL
		f.logHeaders = append(f.logHeaders, header)
	}
	return nil
}

func (f *flowDeserializer) GetLogContents() (contents []LogContent, err error) {
	if f.logContents != nil {
		return f.logContents, nil
	}
	e := f.genLogContents()
	return f.logContents, e
}

func (f *flowDeserializer) genLogContents() error {
	if e := f.genHeaderStack(); e != nil {
		return e
	}
	if e := f.genContent(); e != nil {
		return e
	}

	logContentsLength := f.stack.logContentsOriginLength

	if logContentsLength == 0 {
		f.logContents = make([]LogContent, 0)
		return nil
	}

	var data []byte
	offset := 0

	if *f.headerOptionFlow.ContentCompression != compress.None {
		data = f.contentDeCompressData
	} else {
		data = f.originData
		offset = f.stack.logContentsOffset
	}
	if len(data) == 0 {
		return nil
	}

	readLength := 0

	for {
		if readLength >= logContentsLength {
			break
		}
		//todo 池化
		logContent := LogContent{}
		contentLength, rl, e := f.buffer.ReadLength32(data, offset)
		if e != nil {
			return e
		}
		readLength += rl
		offset = offset + rl
		readL := 0
		if f.headerStack.patternLength != 0 {
			for {
				if readL >= contentLength {
					break
				}
				keyId, value, rl, e := f.buffer.ReadInterfaceValueDic(data, offset)
				if e != nil {
					return e
				}
				key, e := f.dictionary.Decode(keyId)
				if e != nil {
					return e
				}
				readL = readL + rl
				offset = offset + rl
				logContent.KeyValues = append(logContent.KeyValues, KeyValue{
					Key:   key,
					Value: value,
				})
			}
		} else {
			for {
				if readL >= contentLength {
					break
				}
				key, value, rl, e := f.buffer.ReadInterfaceValue(data, offset)
				if e != nil {
					return e
				}
				readL = readL + rl
				offset = offset + rl
				logContent.KeyValues = append(logContent.KeyValues, KeyValue{
					Key:   key,
					Value: value,
				})
			}
		}

		readLength = readLength + readL
		f.logContents = append(f.logContents, logContent)
	}
	return nil
}
