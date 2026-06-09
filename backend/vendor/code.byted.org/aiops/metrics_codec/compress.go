package metrics_codec

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"log"
	"sync"
)

type CompressType int8

const (
	_ CompressType = iota // 0 also represents zlib for historical reasons.
	Zlib
	Zstd
	Gzip
	Lz4
	Snappy
	None = -1
)

const (
	codecV3Code        = 3
	codecV4Code        = 4
	CodecVersionOffset = 8
	CompressInfoOffset = 9
)

var (
	zlibCompressPool = sync.Pool{
		New: func() interface{} {
			c, err := zlib.NewWriterLevel(nil, zlib.BestCompression)
			if err != nil {
				log.Printf("failed to get zlib writer: %v", err)
			}
			return c
		},
	}
)

type Compressor interface {
	Reset(w io.Writer)
	io.WriteCloser
}

func getCompressor(compressType CompressType) Compressor {
	switch compressType {
	case Zlib:
		return zlibCompressPool.Get().(*zlib.Writer)
	default:
	}
	return nil
}

func putCompressor(compressType CompressType, c Compressor) {
	c.Reset(nil)

	switch compressType {
	case Zlib:
		if zw, ok := c.(*zlib.Writer); ok {
			zlibCompressPool.Put(zw)
		}
	default:
	}
}

func Compress(dest, src []byte, compressType CompressType) ([]byte, error) {
	if len(dest) != 0 {
		return dest, ErrInvalidDest
	}

	// non-compress
	if compressType == None {
		dest = append(dest, src...)
		return dest, nil
	}

	if len(src) <= CodecVersionOffset {
		return dest, ErrInvalidMessageHeader
	}

	if magicNumber := DecodeUint64(src); magicNumber != MagicCode {
		return dest, ErrUnknownMagicNumber
	}

	codecVersion := DecodeUint8(src[CodecVersionOffset:])
	var isCodecV4 bool
	switch codecVersion {
	case codecV3Code:
		isCodecV4 = false
	case codecV4Code:
		isCodecV4 = true
	default:
		return dest, ErrUnsupportedCodecVersion
	}

	var buf *bytes.Buffer
	var readLength int32 = 0
	var err error

	if isCodecV4 {
		readLength = int32(DecodeUint32(src[CompressInfoOffset:]))
		headerLen := int(readLength)
		if headerLen >= len(src) {
			return dest, fmt.Errorf("invalid header length: %d, raw data length: %d", headerLen, len(src))
		}
		dest = append(dest, src[:headerLen]...)
		buf = bytes.NewBuffer(dest[headerLen:])
	} else {
		buf = bytes.NewBuffer(dest)
	}

	compressor := getCompressor(compressType)
	if compressor == nil {
		return nil, fmt.Errorf("nil zlib writer")
	}
	defer putCompressor(compressType, compressor)
	compressor.Reset(buf)

	if isCodecV4 {
		_, err = compressor.Write(src[readLength:])
	} else {
		_, err = compressor.Write(src)
	}

	if err != nil {
		return dest[:0], err
	}

	if err = compressor.Close(); err != nil {
		return dest[:0], err
	}

	if isCodecV4 {
		bodyOffset := readLength
		bodyLen := len(src) - int(readLength)
		compressedBody := buf.Bytes()
		bodyCompressedLen := len(compressedBody)
		dest = append(dest, compressedBody...)
		FillBackCompressInfo(dest, bodyOffset, int32(bodyLen), int32(bodyCompressedLen), Zlib)
	} else {
		dest = buf.Bytes()
	}
	return dest, nil
}

func FillBackCompressInfo(buf []byte, bodyOffset, bodyLen, bodyCompressedLen int32, compressAlg CompressType) {
	offset := CompressInfoOffset
	FillBackInt32(buf, offset, bodyOffset)
	FillBackInt32(buf, offset+INT32_LEN, bodyLen)
	FillBackInt8(buf, offset+INT32_LEN*2, int8(compressAlg))    // compress alg
	FillBackInt32(buf, offset+INT32_LEN*2+1, bodyCompressedLen) // compress len
}

func GetBodyRange(src []byte) (start int, end int, err error) {
	if len(src) <= CodecVersionOffset {
		err = ErrInvalidMessageHeader
		return
	}

	if magicNumber := DecodeUint64(src); magicNumber != MagicCode {
		err = ErrUnknownMagicNumber
		return
	}

	readLength := int32(DecodeUint32(src[CompressInfoOffset:]))
	headerLen := int(readLength)
	if headerLen >= len(src) {
		err = fmt.Errorf("invalid header length: %d, raw data length: %d", headerLen, len(src))
		return
	}
	start = headerLen
	end = len(src)
	return
}
