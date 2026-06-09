package codec

import (
	"errors"
	"hash/crc32"
)

type globalStack struct {
	init  bool

	ReservedValue
	totalLength                  int
	headerRegionCompressedLength int
	headerRegionOriginLength     int

	logContentsCompressedLength  int
	logContentsOriginLength      int

}

func (gs *globalStack) reset() {
	gs.init = false
}

type headerStack struct {
	init                bool

	patternLength       int
	commonHeadersLength int
	logHeadersLength    int
}

func (hs *headerStack) reset() {
	hs.init = false
}

type ReservedValue struct {
	debugTracingFlag           bool
	commonHeaderNotCompressed  bool
	enableChecksum             bool
}

type DeSerializer struct {
	data                  []byte
	headerDeCompressData  []byte
	contentDeCompressData []byte

	pattern        *ByteLogPattern
	patterns       []string

	stack          globalStack
	headerStack    headerStack

	byteLogHeader  *ByteLogHeader

	commonHeaders  map[string]interface{}
	logHeaders     []*UniversalLogHeader
	logContents    []*UniversalLogContent

}

type UniversalLogHeader struct {
	ReserveHeader   Header
	CustomizeHeader []*KeyValuePair
}

type UniversalLogContent struct {
	KeyValues []*KeyValuePair
}
// KeyValuePair instead of map[string]interface{}
type KeyValuePair struct {
	Key   string
	Value interface{}
}

type Header struct {
	Timestamp  uint64
	Source     string
	Context    string
	LogId      string
}

// Read decode codec ByteLogHeader buf, Getter function can only be used after this.
func (ds *DeSerializer) Read(data []byte) error {
	if ds.stack.init {
		return nil
	}
	// version | protoType | compression | reservedFlag | totalLength | headerRegionCompressedLength | headerRegionOriginLength | checksum | reserved
	ds.data = data
	byteLogHeader, _, err, totalLength, realHeaderLen, originalHeaderLen := DecodeByteLogHeader(ds.data, 0)
	if err != nil {
		return err
	}
	ds.byteLogHeader = byteLogHeader
	ds.stack.debugTracingFlag = byteLogHeader.ReservedFlag& 0x01 != 0
	ds.stack.commonHeaderNotCompressed = byteLogHeader.ReservedFlag& 0x02 != 0 // 0 老版本；1 commonHeader和字典不压缩
	ds.stack.enableChecksum = byteLogHeader.ReservedFlag& 0x08 != 0
	// verify checksum
	if ds.stack.enableChecksum {
		checksum := crc32.ChecksumIEEE(ds.data[:BYTELOG_HEADER_CHECKSUM_POS])
		checksum = crc32.Update(checksum, crc32.IEEETable, emptyFourBytesData)
		checksum = crc32.Update(checksum, crc32.IEEETable, ds.data[BYTELOG_HEADER_RESERVED_POS:])
		if checksum != byteLogHeader.Checksum {
			return ErrChecksumVerifyFailed
		}
	}

	ds.stack.totalLength = totalLength
	ds.stack.headerRegionCompressedLength = realHeaderLen
	ds.stack.headerRegionOriginLength = originalHeaderLen
	// Content info for length and compressionType
	// skip to content
	offset := BYTELOG_HEADER_LEN + ds.stack.headerRegionCompressedLength
	logContentsCompressedLength, readLength, err := decodeUint32(ds.data, offset)
	if err != nil {
		return err
	}
	ds.stack.logContentsCompressedLength = int(logContentsCompressedLength)

	offset += readLength
	logContentsOriginLength, readLength, err := decodeUint32(ds.data, offset)
	if err != nil {
		return err
	}
	ds.stack.logContentsOriginLength = int(logContentsOriginLength)

	offset += readLength
	contentCompression, _, err := decodeByteRaw(ds.data, offset)
	if err != nil {
		return err
	}
	ds.byteLogHeader.ContentCompression = contentCompression

	ds.stack.init = true
	return nil
}

func (ds *DeSerializer) prepareAll() error {
	// 1. HeadersRegion, init ds.headerDeCompressData
	// commonHeader area
	if err := ds.prepareHeadersRegion(); err != nil {
		return err
	}
	// logHeaders area
	headerCompressionType := CompressType(ds.byteLogHeader.Compression)
	if headerCompressionType != None {
		// only logHeaders compressed, decompress logHeaders area.
		if ds.stack.commonHeaderNotCompressed {
			frontLength := ds.headerStack.patternLength + ds.headerStack.commonHeadersLength
			offset := BYTELOG_HEADER_LEN + frontLength
			tmpLogHeaderLength := ds.stack.headerRegionCompressedLength - frontLength
			compressor, err := GetGlobalCompressor(headerCompressionType)
			if err != nil {
				return err
			}
			data, err := sliceByteArray(ds.data, offset, offset+tmpLogHeaderLength)
			if err != nil {
				return err
			}
			var v []byte
			v, err = compressor.Decompress(v, data)
			if err != nil {
				return err
			}
			// check length
			if len(v) != ds.headerStack.logHeadersLength {
				return ErrLogHeadersLengthNotMatch
			}
			//copy data
			ds.headerDeCompressData = ds.headerDeCompressData[:frontLength]
			//ds.headerDeCompressData = append(ds.headerDeCompressData, ds.data[offset:offset+frontLength]...)
			ds.headerDeCompressData = append(ds.headerDeCompressData, v...)
		}
	}
	// 2.ContentsRegion, init ds.contentDeCompressData
	contentCompressionType := CompressType(ds.byteLogHeader.ContentCompression)
	offset := BYTELOG_HEADER_LEN + ds.stack.headerRegionCompressedLength + CONTENT_COMPRESS_INFO_LEN
	data, err := sliceByteArray(ds.data, offset, offset+ds.stack.logContentsCompressedLength)
	if err != nil {
		return err
	}
	if contentCompressionType != None {
		// decompress ContentsRegion
		compressor, err := GetGlobalCompressor(contentCompressionType)
		if err != nil {
			return err
		}
		var v []byte
		v, err = compressor.Decompress(v, data)
		if err != nil {
			return err
		}
		// check length
		if len(v) != ds.stack.logContentsOriginLength {
			return ErrLogContentsLengthNotMatch
		}
		ds.contentDeCompressData = append(ds.contentDeCompressData, v...)
	} else {
		ds.contentDeCompressData = append(ds.contentDeCompressData, data...)
	}
	return nil
}

func (ds *DeSerializer) prepareHeadersRegion() error {
	if ds.headerStack.init {
		return nil
	}
	headerCompressionType := CompressType(ds.byteLogHeader.Compression)
	var headerData []byte
	offset := BYTELOG_HEADER_LEN
	// pattern and commonHeader compressed
	if headerCompressionType != None && !ds.stack.commonHeaderNotCompressed {
		if len(ds.headerDeCompressData) == 0 {
			if compressor, err := GetGlobalCompressor(headerCompressionType); err == nil {
				data, err := sliceByteArray(ds.data, offset, offset+ds.stack.headerRegionCompressedLength)
				if err != nil {
					return err
				}
				var v []byte
				v, err = compressor.Decompress(v, data)
				if err != nil {
					return err
				}
				if len(v) != ds.stack.headerRegionOriginLength {
					return ErrHeadersRegionLengthNotMatch
				}
				ds.headerDeCompressData = v
			} else {
				return ErrCompressTypeNotSupport
			}
		}
		headerData = ds.headerDeCompressData
		offset = 0
	} else {
		data, err := sliceByteArray(ds.data, offset, offset+ds.stack.headerRegionCompressedLength)
		if err != nil {
			return err
		}
		ds.headerDeCompressData = append(ds.headerDeCompressData, data...)
		headerData = ds.data
	}
	ds.headerStack.init = true
	// only decode pattern area and length field.

	// pattern
	patterns, patternAreaLength, err := decodePatterns(headerData, offset)
	if err != nil {
		return err
	}
	ds.patterns = patterns
	ds.headerStack.patternLength = patternAreaLength

	// commonHeaders
	offset += patternAreaLength
	commonHeadersLength, readLength, err := decodeUint32(headerData, offset)
	if err != nil {
		return err
	}
	ds.headerStack.commonHeadersLength = int(commonHeadersLength) + readLength
	// logHeaders
	//offset += LENGTH_BYTES + commonHeadersLength
	//logHeadersLength := int(DecodeUint32(headerData[offset:]))
	logHeadersLength := ds.stack.headerRegionOriginLength - ds.headerStack.patternLength - ds.headerStack.commonHeadersLength
	ds.headerStack.logHeadersLength = logHeadersLength
	return nil
}

func (ds *DeSerializer) GetPattern() ([]string, error) {
	if err := ds.prepareHeadersRegion(); err != nil {
		return nil, err
	}
	return ds.patterns, nil
}

func (ds *DeSerializer) GetCommonHeaders() (map[string]interface{}, error) {
	if err := ds.prepareHeadersRegion(); err != nil {
		return nil, err
	}
	if ds.commonHeaders == nil {
		headerCompressionType := CompressType(ds.byteLogHeader.Compression)
		var headerData []byte
		offset := ds.headerStack.patternLength
		// pattern and commonHeader compressed
		if headerCompressionType != None && !ds.stack.commonHeaderNotCompressed {
			headerData = ds.headerDeCompressData
		} else {
			headerData = ds.data
			offset += BYTELOG_HEADER_LEN
		}

		ds.commonHeaders = make(map[string]interface{})
		offset += LENGTH_BYTES	// skip length field
		// Not use dic
		if ds.headerStack.patternLength == 4 {
			for readLength := LENGTH_BYTES; readLength < ds.headerStack.commonHeadersLength;{
				key, value, rl, err := decodeKeyValue(headerData, offset)
				if err != nil {
					return nil, err
				}
				readLength = readLength + rl
				offset = offset + rl
				ds.commonHeaders[key] = value

			}
		} else {
			for readLength := LENGTH_BYTES; readLength < ds.headerStack.commonHeadersLength;{
				keyId, value, rl, err := decodeKeyIdValue(headerData, offset, true)
				if err != nil {
					return nil, err
				}
				if err = ds.checkKeyId(keyId); err != nil {
					return nil, err
				}
				key := ds.patterns[keyId]
				readLength = readLength + rl
				offset = offset + rl
				ds.commonHeaders[key] = value
			}
		}

	}
	return ds.commonHeaders, nil
}

func decodeKeyValue(data []byte, offset int) (string, interface{}, int, error) {
	pos := offset
	key, l, err := decodeShortStrWithType(data, pos)
	if err != nil {
		return "", nil, 0, err
	}
	pos += l
	v, l, err := decodeValueWithUnknownType(data, pos)
	if err != nil {
		return "", nil, 0, err
	}
	return key, v, pos+l-offset, nil
}

// decodeKeyValueLength decodes key and value length
func decodeKeyValueLength(data []byte, offset int) (key string, keyLength, valueLength int, err error) {
	pos := offset
	key, keyLength, err = decodeShortStrWithType(data, pos)
	if err != nil {
		return "", 0, 0, err
	}
	pos += keyLength
	valueLength, err = decodeValueLengthWithUnknownType(data, pos)
	if err != nil {
		return "", 0, 0, err
	}
	return key, keyLength, valueLength, nil
}

// Notice: keyId length is 1 or 2 byte, which depend on key's number in pattern
func decodeKeyIdValue(data []byte, offset int, isIdOneByteLen bool) (int, interface{}, int, error) {
	pos := offset
	var keyId int
	if isIdOneByteLen {
		id, l, err := decodeUint8(data, pos)
		if err != nil {
			return 0, nil, 0, err
		}
		keyId = int(id)
		pos += l
	} else {
		id, l, err := decodeUint16(data, pos)
		if err != nil {
			return 0, nil, 0, err
		}
		keyId = int(id)
		pos += l
	}
	v, l, err := decodeValueWithUnknownType(data, pos)
	if err != nil {
		return 0, nil, 0, err
	}
	return keyId, v, pos+l-offset, nil
}

func decodeKeyIdValueLength(data []byte, offset int, isIdOneByteLen bool) (keyId, keyLength, valueLength int, err error) {
	pos := offset
	if isIdOneByteLen {
		id, l, err := decodeUint8(data, pos)
		if err != nil {
			return 0, 0, 0, err
		}
		keyId = int(id)
		keyLength = l
		pos += l
	} else {
		id, l, err := decodeUint16(data, pos)
		if err != nil {
			return 0, 0, 0, err
		}
		keyId = int(id)
		keyLength = l
		pos += l
	}
	valueLength, err = decodeValueLengthWithUnknownType(data, pos)
	if err != nil {
		return 0, 0, 0, err
	}
	return keyId, keyLength, valueLength, nil
}

func (ds *DeSerializer) GetLogHeaders() ([]*UniversalLogHeader, error) {
	if err := ds.prepareHeadersRegion(); err != nil {
		return nil, err
	}
	headerCompressionType := CompressType(ds.byteLogHeader.Compression)
	var headerData []byte
	offset := BYTELOG_HEADER_LEN + ds.headerStack.patternLength + ds.headerStack.commonHeadersLength
	if headerCompressionType != None {
		// only logHeaders compressed, decompress logHeaders area.
		if ds.stack.commonHeaderNotCompressed {
			tmpLogHeaderLength := ds.stack.headerRegionCompressedLength - ds.headerStack.patternLength - ds.headerStack.commonHeadersLength
			if compressor, err := GetGlobalCompressor(headerCompressionType); err == nil {
				data, err := sliceByteArray(ds.data, offset, offset+tmpLogHeaderLength)
				if err != nil {
					return nil, err
				}
				var v []byte
				v, err = compressor.Decompress(v, data)
				if err != nil {
					return nil, err
				}
				// check length
				if len(v) != ds.headerStack.logHeadersLength {
					return nil, ErrLogHeadersLengthNotMatch
				}
				headerData = v
				offset = LENGTH_BYTES
			} else {
				return nil, ErrCompressTypeNotSupport
			}
		} else {
			headerData = ds.headerDeCompressData
			offset = offset - BYTELOG_HEADER_LEN + LENGTH_BYTES
		}
	} else {
		headerData = ds.data
		offset += LENGTH_BYTES
	}

	logHeaders := make([]*UniversalLogHeader, 0)
	for readLength := 0; readLength < ds.headerStack.logHeadersLength-4; {
		pos := 0
		// 1. decode length
		l, m, err := decodeUint32(headerData, offset)
		if err != nil {
			return nil, err
		}
		length := int(l)
		readLength += m
		offset += m

		logHeader := &UniversalLogHeader{}
		// 2. decode ReserveHeader
		timestamp, rl, err := decodeUint64WithType(headerData, offset)
		if err != nil {
			return nil, err
		}
		pos += rl
		offset += rl
		logHeader.ReserveHeader.Timestamp = timestamp

		reserveData, err := sliceByteArray(headerData, offset, len(headerData))
		if err != nil {
			return nil, err
		}
		threeFields, rl := bytesToShortStrings(reserveData, 3)
		if len(threeFields) != 3 {
			return nil, errors.New("invalid ReserveHeader")
		}
		pos += rl
		offset += rl
		logHeader.ReserveHeader.Source, logHeader.ReserveHeader.Context, logHeader.ReserveHeader.LogId = threeFields[0], threeFields[1], threeFields[2]
		if err != nil {
			return nil, err
		}
		// 3. decode CustomizeHeader
		if ds.headerStack.patternLength == 4 {
			for pos < length {
				key, value, l, err := decodeKeyValue(headerData, offset)
				if err != nil {
					return nil, err
				}
				logHeader.CustomizeHeader = append(logHeader.CustomizeHeader, &KeyValuePair{Key: key, Value: value})
				pos += l
				offset += l
			}
		} else {
			for pos < length {
				keyId, value, l, err := decodeKeyIdValue(headerData, offset, true)
				if err != nil {
					return nil, err
				}
				if err = ds.checkKeyId(keyId); err != nil {
					return nil, err
				}
				key := ds.patterns[keyId]
				logHeader.CustomizeHeader = append(logHeader.CustomizeHeader, &KeyValuePair{Key: key, Value: value})
				pos += l
				offset += l
			}
		}
		readLength += length
		logHeaders = append(logHeaders, logHeader)
	}
	return logHeaders, nil
}

func (ds *DeSerializer) GetLogContents() ([]*UniversalLogContent, error) {
	if err := ds.prepareHeadersRegion(); err != nil {
		return nil, err
	}
	contentCompressionType := CompressType(ds.byteLogHeader.ContentCompression)
	offset := BYTELOG_HEADER_LEN + ds.stack.headerRegionCompressedLength + CONTENT_COMPRESS_INFO_LEN
	var contentsData []byte
	// decompress, get contentsData
	if contentCompressionType != None {
		if compressor, err := GetGlobalCompressor(contentCompressionType); err == nil {
			data, err := sliceByteArray(ds.data, offset, offset+ds.stack.logContentsCompressedLength)
			if err != nil {
				return nil, err
			}
			var v []byte
			v, err = compressor.Decompress(v, data)
			if err != nil {
				return nil, err
			}
			// check length
			if len(v) != ds.stack.logContentsOriginLength {
				return nil, ErrLogContentsLengthNotMatch
			}
			contentsData = v
			offset = 0
		} else {
			return nil, ErrCompressTypeNotSupport
		}
	} else {
		contentsData = ds.data
	}

	for readLength := 0; readLength < ds.stack.logContentsOriginLength; {
		// decode length
		pos := 0
		l, m, err := decodeUint32(contentsData, offset)
		if err != nil {
			return nil, err
		}
		readLength+=m
		offset+=m
		length := int(l)

		logContent := &UniversalLogContent{}
		// not use dic
		if ds.headerStack.patternLength == 4 {
			for pos < length {
				key, value, l, err := decodeKeyValue(contentsData, offset)
				if err != nil {
					return nil, err
				}
				logContent.KeyValues = append(logContent.KeyValues, &KeyValuePair{Key: key, Value: value})
				pos += l
				offset += l
			}
		} else {
			for pos < length {
				keyId, value, l, err := decodeKeyIdValue(contentsData, offset, true)
				if err != nil {
					return nil, err
				}
				if err = ds.checkKeyId(keyId); err != nil {
					return nil, err
				}
				key := ds.patterns[keyId]
				logContent.KeyValues = append(logContent.KeyValues, &KeyValuePair{Key: key, Value: value})
				pos += l
				offset+=l
			}
		}
		readLength += length
		ds.logContents = append(ds.logContents, logContent)
	}
	return ds.logContents, nil
}

func (ds *DeSerializer) Reset() {
	ds.data = ds.data[:0]
	ds.contentDeCompressData = ds.contentDeCompressData[:0]
	ds.headerDeCompressData = ds.headerDeCompressData[:0]
	ds.stack.reset()
	ds.headerStack.reset()
	ds.byteLogHeader = nil
	ds.commonHeaders = nil
	ds.logHeaders = nil
	ds.logContents = nil
}

func (ds *DeSerializer) checkKeyId(id int) error {
	length := len(ds.patterns)
	if length < 1 {
		return errors.New("no pattern exist")
	}
	if id < 0 || id >= length {
		return errors.New("invalid keyId")
	}
	return nil
}

func decodePatterns(data []byte, offset int) ([]string, int, error) {
	value, l, err := decodeUint32(data, offset)
	if err != nil {
		return nil, 0, err
	}
	length := int(value) + l
	if len(data) < offset+length {
		return nil, 0, ErrNoEnoughBytes
	}
	patterns := make([]string, 0)
	readLength := l
	for readLength < length {
		str, l, err := decodeShortStrWithType(data, offset+readLength)
		if err != nil {
			return nil, 0, err
		}
		readLength += l
		patterns = append(patterns, str)
	}
	return patterns, readLength, nil
}

