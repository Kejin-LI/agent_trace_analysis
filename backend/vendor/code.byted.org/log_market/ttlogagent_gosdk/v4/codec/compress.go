package codec

import (
	"fmt"
	"sync"

	"github.com/klauspost/compress/zstd"
)

type CompressType byte

const (
	None      CompressType = 0
	Gzip      CompressType = 1 // Not supported
	ZSTD_FAST CompressType = 2 // Supported. Level is 1
	ZSTD      CompressType = 3 // Supported. Level is 3
	ZSTD_HIGH CompressType = 4 // Supported. Level is 10
	Snappy    CompressType = 5 // Not supported
	LZ4       CompressType = 6 // Not supported
)

var (
	mutex       sync.RWMutex
	compressors map[CompressType]Compressor
	onceMap     map[CompressType]sync.Once
)

type Compressor interface {
	Compress(dist, raw []byte) (output []byte, err error)
	Decompress(dst, raw []byte) (output []byte, err error)
}

func init() {
	allType := []CompressType{ZSTD_FAST, ZSTD, ZSTD_HIGH}
	compressors = make(map[CompressType]Compressor)
	onceMap = make(map[CompressType]sync.Once)
	for _, t := range allType {
		onceMap[t] = sync.Once{}
	}
}

func GetGlobalCompressor(t CompressType) (compressor Compressor, err error) {
	switch t {
	case ZSTD_FAST, ZSTD, ZSTD_HIGH:
		mutex.RLock()
		compressor = compressors[t]
		mutex.RUnlock()
		if compressor == nil {
			if once, ok := onceMap[t]; ok {
				once.Do(func() {
					compressor = NewCompressor(t)
					mutex.Lock()
					compressors[t] = compressor
					mutex.Unlock()
				})
			} else {
				return nil, ErrCompressTypeNotSupport
			}
		}
	default:
		return nil, ErrCompressTypeNotSupport
	}
	return compressor, nil
}

type ZstdCompressor struct {
	encoder *zstd.Encoder
	decoder *zstd.Decoder
}

func NewZstdCompressor(level int) *ZstdCompressor {
	writer, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level)))
	if err != nil {
		return nil
	}
	reader, err := zstd.NewReader(nil)
	if err != nil {
		return nil
	}
	return &ZstdCompressor{writer, reader}
}

func (c *ZstdCompressor) Compress(dst, raw []byte) (output []byte, err error) {
	return c.encoder.EncodeAll(raw, dst), nil
}

func (c *ZstdCompressor) Decompress(dst, raw []byte) (output []byte, err error) {
	return c.decoder.DecodeAll(raw, dst)
}

// NewCompressor create a compressor based on the type.
// It only supports ZSTD currently.
func NewCompressor(compressType CompressType) Compressor {
	switch compressType {
	case ZSTD_FAST:
		return NewZstdCompressor(1)
	case ZSTD:
		return NewZstdCompressor(3)
	case ZSTD_HIGH:
		return NewZstdCompressor(10)
	default:
	}
	return nil
}

// CompressBytes compress raw data bytes to compressed data,
// make sure that source data must uncompressed, including header and content region, otherwise will return an error.
func CompressBytes(dst []byte, src []byte, headerCompression CompressType, notCompressCommonHeader bool, contentCompression CompressType, sizeLimit int) (res []byte, err error) {
	if len(src) < BYTELOG_HEADER_LEN {
		return dst, ErrNoEnoughBytes
	}

	// No compression at all.
	if headerCompression == None && contentCompression == None {
		return dst, fmt.Errorf("bytes are not compressed")
	}

	headerCompressor, err := GetGlobalCompressor(headerCompression)
	if err != nil && headerCompression != None {
		return dst, fmt.Errorf("unsupported header compressor")
	}

	contentCompressor, err := GetGlobalCompressor(contentCompression)
	if err != nil && contentCompression != None {
		return dst, fmt.Errorf("unsupported content compressor")
	}

	originalDstLen := len(dst)
	srcLen := len(src)
	isByteLogBatch := false
	pos := 0

	sdkHeader, readLength, err := DecodeSDKHeader(src)
	if err == nil {
		isByteLogBatch = true // These bytes are from a ByteLogBatch
		pos += readLength
	} else {
		isByteLogBatch = false // These bytes are from a ByteLogMessage
	}

	byteLogHeader, readLength, err, totalLength, realHeaderLen, originalHeaderLen := DecodeByteLogHeader(src, pos)
	if err != nil {
		return dst[:originalDstLen], err
	}

	if totalLength > sizeLimit || realHeaderLen > sizeLimit || originalHeaderLen > sizeLimit {
		return dst[:originalDstLen], fmt.Errorf("bytelog message is too long")
	}

	pos += readLength
	totalLengthStart := pos
	// split header region data
	end := pos+realHeaderLen
	if end > srcLen {
		return dst[:originalDstLen], ErrNoEnoughBytes
	}
	headerData := src[pos: end]
	pos += realHeaderLen

	// split content region data
	contentRealLen, contentOriginalLen, contentCompressType, readLength, err := decodeContentCompressionInfo(src, pos)
	if err != nil {
		return dst[:originalDstLen], err
	}
	pos += readLength
	end = pos+contentRealLen
	if end > srcLen {
		return dst[:originalDstLen], ErrNoEnoughBytes
	}
	contentData := src[pos: end]

	pos += contentRealLen
	if pos-totalLengthStart != totalLength {
		return dst[:originalDstLen], fmt.Errorf("total length not match: %d, %d", totalLength, pos-totalLengthStart)
	}

	// not support compressed data
	if (CompressType(byteLogHeader.Compression) != None || CompressType(contentCompressType) != None) ||
		(realHeaderLen != originalHeaderLen) ||
		(contentRealLen != contentOriginalLen) {
		return dst[:originalDstLen], fmt.Errorf("this batch is already compressed")
	}

	if isByteLogBatch {
		dst = sdkHeader.Encode(dst)
	}

	// Begin to compress the data
	startOfByteLogMessage := len(dst)
	byteLogHeader.Compression = byte(headerCompression)
	if notCompressCommonHeader {
		// TODO: check with @Yan Tang how to mark not compress CommonHeader
		byteLogHeader.CompressCommonHeaders(!notCompressCommonHeader)
	}
	dst = byteLogHeader.Encode(dst)

	if headerCompressor != nil {
		if notCompressCommonHeader {
			// headerCompressor.
			headerPos := 0
			patternLen, readLength, err := decodeUint32(headerData, headerPos)
			if err != nil {
				return dst[:originalDstLen], err
			}
			headerPos += readLength + int(patternLen)

			commonHeaderLen, readLength, err := decodeUint32(headerData, headerPos)
			if err != nil {
				return dst[:originalDstLen], err
			}
			headerPos += readLength + int(commonHeaderLen)
			// copy pattern and commonHeaders area directly
			dst = append(dst, headerData[:headerPos]...)

			logHeaders := headerData[headerPos:]
			dst, err = headerCompressor.Compress(dst, logHeaders)
			if err != nil {
				return dst[:originalDstLen], err
			}
		} else {
			dst, err = headerCompressor.Compress(dst, headerData)
			if err != nil {
				return dst[:originalDstLen], err
			}
		}
	} else {
		dst = append(dst, headerData...)
	}

	endOfByteLogHeaderArea := len(dst)
	contentCompressType = byte(contentCompression)
	dst = encodeContentCompressInfo(dst, contentCompressType)
	startOfByteLogContentArea := len(dst)

	if contentCompressor != nil {
		dst, err = contentCompressor.Compress(dst, contentData)
		if err != nil {
			return dst[:originalDstLen], err
		}
	} else {
		dst = append(dst, contentData...)
	}
	// TODO update lengths in sdk header, ByteLogHeader, ContentCompressInfo
	endOfLogContentArea := len(dst)
	{ // Update Content Compression Info
		WriteUint32(dst, startOfByteLogContentArea-CONTENT_COMPRESS_INFO_LEN, uint32(endOfLogContentArea-startOfByteLogContentArea)) // actual content length.
		WriteUint32(dst, startOfByteLogContentArea-CONTENT_COMPRESS_INFO_LEN+LENGTH_BYTES, uint32(contentRealLen))                   // original content length.
	}

	{
		WriteUint32(dst, startOfByteLogMessage+BYTELOG_HEADER_LEN-DISTANCE_BTW_BODY_LEN_POS_AND_BODY,
			uint32(endOfLogContentArea-BYTELOG_HEADER_LEN-startOfByteLogMessage))

		// Update the Compressed Header Length.
		WriteUint32(dst, startOfByteLogMessage+BYTELOG_HEADER_LEN-DISTANCE_BTW_HEADER_COMP_LEN_POS_AND_HEADER,
			uint32(endOfByteLogHeaderArea-BYTELOG_HEADER_LEN-startOfByteLogMessage))

		// Update the Original Header Length.
		WriteUint32(dst, startOfByteLogMessage+BYTELOG_HEADER_LEN-DISTANCE_BTW_HEADER_ORIN_LEN_POS_AND_HEADER, uint32(realHeaderLen))
	}

	if isByteLogBatch {
		WriteUint32(dst[originalDstLen:], +SDK_HEADER_LENGTH_POS, uint32(endOfLogContentArea-originalDstLen-LENGTH_BYTES))
	}

	return dst, nil
}

/*

Do not use DataDog ZSTD Implementation. Avoid using CGO!!!

type DataDogZstdCompressor struct {
	level int
}

func (c *DataDogZstdCompressor) Compress(dst, raw []byte) (output []byte, err error) {
	res, err := datadog.CompressLevel(nil, raw, c.level)
	if err != nil {
		return dst, err
	}
	dst = append(dst, res...)
	return dst, err
}

func (c *DataDogZstdCompressor) Decompress(dst, raw []byte) (output []byte, err error) {
	res := make([]byte, 0, 10240)
	res, err = datadog.Decompress(res, raw)
	if err != nil {
		return dst, err
	}
	dst = append(dst, res...)
	return dst, err
}

func NewDataDogZstdCompressor(level int) Compressor {
	return &DataDogZstdCompressor{
		level: level,
	}
}
*/
