package serializer

import (
	"code.byted.org/bytedtrace/serializer-go/compress"
	"errors"
)

const (
	FlowSerializerType   = "flow"
	FlowDeSerializerType = "flow"
)

var (
	DefaultBigEndian                       = false
	DefaultUseDic                          = true
	emptyLengthBytes                       = []byte{0, 0, 0, 0}
	emptyString                            = ""
	DefaultVersion                  byte   = 0
	DefaultProtoType                byte   = 0
	DefaultCompression                     = compress.None
	DefaultReserved                 byte   = 0
	DefaultUserId                   uint64 = 0
	DefaultContentCompression              = compress.None
	EmptyHeaderError                       = errors.New("log header  is empty")
	EmptyReserveLogHeaderError             = errors.New("reserve header is empty")
	IncompleteReserveLogHeaderError        = errors.New("timestamp is require for reserve header")
	NotHeaderOptionFlowError               = errors.New("not headerOptionFlow")
)

type HeaderOptionFlow struct {
	Version   *byte
	ProtoType *byte
	// Header区域是否压缩
	Compression *byte
	Reserved    *byte
	UserId      *uint64
	// content区域是否压缩
	ContentCompression *byte
}

func (f *HeaderOptionFlow) Flatten() []ValueType {
	// version,protoType,compression,reserved,totalLength,headerRegionCompressedLength,headerRegionOriginLength,userId
	return []ValueType{ByteRaw, ByteRaw, ByteRaw, ByteRaw, IntRaw, IntRaw, IntRaw, Uint64Raw}
}

// 定义了logHeader中保留字段的含义及数据类型
type LogHeaderOptionFlow struct {
	Timestamp *uint64
	Source    *string
	Context   *string
	LogId     *string
}

func (l *LogHeaderOptionFlow) Flatten() []ValueType {
	// Timestamp,Source,Context,LogId
	return []ValueType{Uint64, String, String, String}
}

type positionRecorder struct {
	totalLengthPos                  int
	headerRegionCompressedLengthPos int
	headerRegionOriginLengthPos     int
	patternLengthPos                int
	logHeaderLengthPos              int
	logContentCompressedLengthPos   int
	logContentOriginLengthPos       int
}

// 序列化器的配置.
type SOption struct {
	// 是否为大端存储,默认为否
	BigEndian *bool
	// 是否使用字典
	UseDic *bool
}

type flowSerializer struct {
	positionRecorder
	bigEndian             bool
	useDic                bool
	headerOption          *HeaderOptionFlow
	buffer                WriterBuffer
	tempBuffer            WriterBuffer
	contentSize           int
	compression           bool
	logContentCompression bool
	commonHeaders         []KeyValue
	dataPack              []*DataPack
	header                []byte
	commonHeaderByte      []byte
	contentHeader         []byte

	// 字典
	dictionary  Dictionary
	compressors *compress.Compressors
}

/**
//	serializerType:序列化器类型.
	bigEndian:大端存储/小端存储.
	compression:header部分是否压缩.
	logContentCompression:logContent部分是否压缩.
*/
func NewSerializer(serializerType string, opt SOption) Serializer {
	if serializerType == FlowSerializerType {
		var bigEndian bool
		var useDic bool
		if opt.BigEndian == nil {
			bigEndian = DefaultBigEndian
		} else {
			bigEndian = *opt.BigEndian
		}
		if opt.UseDic == nil {
			useDic = DefaultUseDic
		} else {
			useDic = *opt.UseDic
		}

		f := &flowSerializer{
			bigEndian:   bigEndian,
			useDic:      useDic,
			buffer:      NewByteBuffer(bigEndian),
			tempBuffer:  NewByteBuffer(bigEndian),
			contentSize: 0,
			dictionary:  NewDictionary(),
			compressors: compress.GetCompressors(),
		}
		f.totalLengthPos = 4
		f.headerRegionCompressedLengthPos = 8
		f.headerRegionOriginLengthPos = 12
		return f
	}
	return nil
}

func (f *flowSerializer) SetHeader(opt Header) {
	option := fixHeader(opt)
	f.contentHeader = []byte{0, 0, 0, 0, 0, 0, 0, 0, *option.ContentCompression}
	f.headerOption = option
	f.genHeader(true)
}

func fixHeader(opt Header) *HeaderOptionFlow {
	option, ok := opt.(*HeaderOptionFlow)
	if !ok {
		option = &HeaderOptionFlow{}
	}
	if option.Version == nil {
		option.Version = &DefaultVersion
	}
	if option.ProtoType == nil {
		option.ProtoType = &DefaultProtoType
	}
	if option.Compression == nil {
		option.Compression = &DefaultCompression
	}
	if option.Reserved == nil {
		option.Reserved = &DefaultReserved
	}
	if option.ContentCompression == nil {
		option.ContentCompression = &DefaultContentCompression
	}
	if option.UserId == nil {
		option.UserId = &DefaultUserId
	}
	return option
}

func (f *flowSerializer) SetCommonHeaders(headers []KeyValue) error {
	f.commonHeaders = headers
	return f.genCommonHeader(true)
}

// 清除LogHeaders和LogContents的数据.
func (f *flowSerializer) Clear() {
	f.dataPack = nil
	f.dictionary.Clear()
}

func (f *flowSerializer) Reset() {
	f.dataPack = nil
	f.commonHeaderByte = nil
	f.commonHeaders = nil
	f.dictionary.Reset()
}

// 添加数据.
func (f *flowSerializer) Feed(dataPack *DataPack) error {
	if f.useDic {
		if dataPack.LogHeader != nil && dataPack.LogHeader.CustomizeHeader != nil {
			for _, header := range dataPack.LogHeader.CustomizeHeader {
				e := f.dictionary.AddKey(header.Key)
				if e != nil {
					return e
				}
			}
		}
		if dataPack.LogContent != nil && dataPack.LogContent.KeyValues != nil {
			for _, content := range dataPack.LogContent.KeyValues {
				e := f.dictionary.AddKey(content.Key)
				if e != nil {
					return e
				}
			}
		}
	}
	f.dataPack = append(f.dataPack, dataPack)
	return nil
}

// 获取编码数据.
func (f *flowSerializer) Serialize() ([]byte, error) {
	if f.dataPack == nil {
		return nil, nil
	}
	if f.headerOption == nil {
		return nil, EmptyHeaderError
	}
	f.buffer.Reset()
	f.buffer.Write(f.header)

	// 头部区域
	pos := f.buffer.Len()

	var buffer WriterBuffer

	if *f.headerOption.Compression != compress.None {
		buffer = f.tempBuffer
		buffer.Reset()
	} else {
		buffer = f.buffer
	}

	// 写patterns todo 对string进行截断
	// 写入pattern长度占位
	f.patternLengthPos = buffer.Len()
	buffer.Write(emptyLengthBytes)
	if f.useDic {
		dictionary, e := f.dictionary.Flatten()
		if e != nil {
			return nil, e
		}

		patternLength := 0
		for i := 0; i < len(dictionary); i++ {
			l, err := buffer.WriteStringKey(dictionary[i])
			if err != nil {
				return nil, err
			}
			patternLength = patternLength + l
		}
		if e = buffer.SetPosValue(f.patternLengthPos, patternLength); e != nil {
			return nil, e
		}
	}

	// 写commonHeader
	if err := f.genCommonHeader(false); err != nil {
		return nil, err
	}

	buffer.Write(f.commonHeaderByte)

	// 写LogHeaders
	f.logHeaderLengthPos = buffer.Len()
	// 写入logHeaders长度占位
	buffer.Write(emptyLengthBytes)

	totalHeaderLength := 0
	headerWriteLength := 0
	for _, dataPack := range f.dataPack {
		headers := dataPack.LogHeader
		if headers != nil {
			headerWriteLength = 0
			pos := buffer.Len()
			wl := buffer.Write(emptyLengthBytes)

			if headers.ReserveHeader == nil {
				return nil, EmptyReserveLogHeaderError
			}
			// 写入默认头
			h, ok := headers.ReserveHeader.(*LogHeaderOptionFlow)
			if ok {
				if h.Timestamp == nil {
					return nil, IncompleteReserveLogHeaderError
				}
				if h.Source == nil {
					h.Source = &emptyString
				}
				if h.Context == nil {
					h.Context = &emptyString
				}
				if h.LogId == nil {
					h.LogId = &emptyString
				}
				//todo 不用writeValue
				ll, e := buffer.WriteValue(*h.Timestamp)
				if e != nil {
					return nil, e
				}
				headerWriteLength = headerWriteLength + ll
				ll, e = buffer.WriteValue(*h.Source)
				if e != nil {
					return nil, e
				}
				headerWriteLength = headerWriteLength + ll
				ll, e = buffer.WriteValue(*h.Context)
				if e != nil {
					return nil, e
				}
				headerWriteLength = headerWriteLength + ll
				ll, e = buffer.WriteValue(*h.LogId)
				if e != nil {
					return nil, e
				}
				headerWriteLength = headerWriteLength + ll
			} else {
				return nil, NotHeaderOptionFlowError
			}
			if headers.CustomizeHeader != nil {
				if f.useDic {
					for _, kv := range headers.CustomizeHeader {
						id, e := f.dictionary.Coding(kv.Key)
						if e != nil {
							return nil, e
						}
						ll, e := buffer.WriteDicKeyValue(id, kv.Value)
						if e != nil {
							return nil, e
						}
						headerWriteLength = headerWriteLength + ll
					}
				} else {
					for _, kv := range headers.CustomizeHeader {
						if ll, e := buffer.WriteKeyValue(kv.Key, kv.Value); e != nil {
							return nil, e
						} else {
							headerWriteLength = headerWriteLength + ll
						}
					}
				}
			}
			if e := buffer.SetPosValue(pos, headerWriteLength); e != nil {
				return nil, e
			}
			totalHeaderLength = totalHeaderLength + wl + headerWriteLength
		}
	}
	if e := buffer.SetPosValue(f.logHeaderLengthPos, totalHeaderLength); e != nil {
		return nil, e
	}

	// 头部区域写完后,处理头部压缩
	if *f.headerOption.Compression != compress.None {
		headerData := buffer.BytesCopy()
		rs, e := f.compressors.Compress(*f.headerOption.Compression, headerData)
		if e != nil {
			return nil, e
		}
		wl := f.buffer.Write(rs)

		if e = f.buffer.SetPosValue(f.headerRegionCompressedLengthPos, wl); e != nil {
			return nil, e
		}
		if e = f.buffer.SetPosValue(f.headerRegionOriginLengthPos, len(headerData)); e != nil {
			return nil, e
		}
	} else {
		headerLength := f.buffer.Len() - pos
		if e := f.buffer.SetPosValue(f.headerRegionCompressedLengthPos, headerLength); e != nil {
			return nil, e
		}
		if e := f.buffer.SetPosValue(f.headerRegionOriginLengthPos, headerLength); e != nil {
			return nil, e
		}
	}

	// content区域

	// 写LogContent
	f.logContentCompressedLengthPos = f.buffer.Len()
	f.logContentOriginLengthPos = f.logContentCompressedLengthPos + 4
	// 写入content长度占位
	f.buffer.Write(f.contentHeader)

	contentPos := f.buffer.Len()

	if *f.headerOption.ContentCompression != compress.None {
		buffer = f.tempBuffer
		buffer.Reset()
	} else {
		buffer = f.buffer
	}

	contentWriteLength := 0
	for _, dataPack := range f.dataPack {
		content := dataPack.LogContent
		if content != nil {
			contentWriteLength = 0
			pos := buffer.Len()
			buffer.Write(emptyLengthBytes)

			if f.useDic {
				for _, kv := range content.KeyValues {
					id, e := f.dictionary.Coding(kv.Key)
					if e != nil {
						return nil, e
					}
					ll, e := buffer.WriteDicKeyValue(id, kv.Value)
					if e != nil {
						return nil, e
					}
					contentWriteLength = contentWriteLength + ll
				}
			} else {
				for _, kv := range content.KeyValues {
					ll, e := buffer.WriteKeyValue(kv.Key, kv.Value)
					if e != nil {
						return nil, e
					}
					contentWriteLength = contentWriteLength + ll
				}
			}

			if e := buffer.SetPosValue(pos, contentWriteLength); e != nil {
				return nil, e
			}
		}
	}
	// 处理压缩
	if *f.headerOption.ContentCompression != compress.None {
		contentData := buffer.BytesCopy()
		rs, e := f.compressors.Compress(*f.headerOption.ContentCompression, contentData)
		if e != nil {
			return nil, e
		}
		wl := f.buffer.Write(rs)
		if e = f.buffer.SetPosValue(f.logContentCompressedLengthPos, wl); e != nil {
			return nil, e
		}
		if e = f.buffer.SetPosValue(f.logContentOriginLengthPos, len(contentData)); e != nil {
			return nil, e
		}
	} else {
		contentLength := f.buffer.Len() - contentPos
		if e := f.buffer.SetPosValue(f.logContentCompressedLengthPos, contentLength); e != nil {
			return nil, e
		}
		if e := f.buffer.SetPosValue(f.logContentOriginLengthPos, contentLength); e != nil {
			return nil, e
		}
	}
	// 设置总长度
	totalLength := f.buffer.Len() - pos
	if e := f.buffer.SetPosValue(f.totalLengthPos, totalLength); e != nil {
		return nil, e
	}
	// 为了避免序列化后被修改,需要copy一份
	return f.buffer.BytesCopy(), nil
}

func (f *flowSerializer) genHeader(force bool) {
	if f.headerOption == nil {
		return
	}
	if force || f.header == nil {
		header := make([]byte, 24)
		header[0] = *f.headerOption.Version
		header[1] = *f.headerOption.ProtoType
		header[2] = *f.headerOption.Compression
		header[3] = *f.headerOption.Reserved
		ids := Uint64ToBytes(*f.headerOption.UserId, f.bigEndian)
		header[16] = ids[0]
		header[17] = ids[1]
		header[18] = ids[2]
		header[19] = ids[3]
		header[20] = ids[4]
		header[21] = ids[5]
		header[22] = ids[6]
		header[23] = ids[7]
		f.header = header
	}
}

func (f *flowSerializer) genCommonHeader(force bool) error {
	if f.commonHeaderByte == nil || force {
		if f.commonHeaders == nil {
			f.commonHeaderByte = emptyLengthBytes
		} else {
			bf := f.tempBuffer
			bf.Reset()
			size := 0
			bf.Write(emptyLengthBytes)
			// 使用字典
			if f.useDic {
				for _, kv := range f.commonHeaders {
					if err := f.dictionary.AddCommonKey(kv.Key); err != nil {
						return err
					}
				}
				for _, kv := range f.commonHeaders {
					id, e := f.dictionary.Coding(kv.Key)
					if e == nil {
						l, _ := bf.WriteDicKeyValue(id, kv.Value)
						size = size + l
					}
				}
			} else {
				for _, kv := range f.commonHeaders {
					l, _ := bf.WriteKeyValue(kv.Key, kv.Value)
					size = size + l
				}
			}
			if err := bf.SetPosValue(0, size); err != nil {
				return err
			}
			f.commonHeaderByte = bf.BytesCopy()
		}
	}
	return nil
}
