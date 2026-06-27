package codec

import (
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"math/rand"
	"strings"
	"time"
)

var (
	sampledTagBytes  []byte
	sampledTagLength int
	emptyMsgBytes    []byte
	emptyMsgLength   int
)

func init() {
	msg, _ := NewKeyValue("_msg", "", false)
	emptyMsgBytes = msg.Encode(emptyMsgBytes)
	emptyMsgLength = len(emptyMsgBytes)

	slog, _ := NewKeyValue("_slog", "1", false)
	sampledTagBytes = slog.Encode(sampledTagBytes)
	sampledTagLength = len(sampledTagBytes)

	rand.Seed(time.Now().UnixNano())
}

type EncodeConfig struct {
	SetChecksum          bool
	CompressCommonHeader bool
	HeaderCompression    CompressType
	ContentCompression   CompressType
}

const (
	QuotaPercent75  = 1
	QuotaPercent100 = 2
)

const (
	UnknownLevel = iota
	TraceLevel
	DebugLevel
	InfoLevel
	NoticeLevel
	WarnLevel
	ErrorLevel
	FatalLevel
)

type logInfo struct {
	level byte
	// origin size
	originHeaderSize  int
	originContentSize int
	isSampled         bool
	// size if sample
	sampledHeaderSize  int
	sampledContentSize int
	// size after sample(or not sample)
	realHeaderSize  int
	realContentSize int
}

func sum(logsInfo []logInfo) (*SampleBatchInfo, map[byte]LevelInfo) {
	res := make(map[byte]LevelInfo)
	sampleInfo := new(SampleBatchInfo)
	for _, info := range logsInfo {
		v := res[info.level]

		if info.isSampled {
			v.SampledLogCount += 1
			v.SampledLogSize += info.realHeaderSize + info.realContentSize
		} else {
			v.SampledLogSize += info.originHeaderSize + info.originContentSize
		}
		v.LogCount += 1
		v.OriginLogSize += info.originHeaderSize + info.originContentSize
		res[info.level] = v

		sampleInfo.LogCount += 1
		sampleInfo.SampleLogHeadersLength += info.sampledHeaderSize
		sampleInfo.SampleLogContentsLength += info.sampledContentSize
		sampleInfo.OriginLogContentsLength += info.originContentSize
		sampleInfo.OriginLogHeadersLength += info.originHeaderSize
	}
	return sampleInfo, res
}

type LevelInfo struct {
	LogCount      int
	OriginLogSize int

	SampledLogCount int
	SampledLogSize  int
}

type SampleBatchInfo struct {
	LogCount      int
	OriginLogSize int // origin batch size after decompress
	LogSize       int // batch size after sample, no compress

	OriginLogHeadersLength  int
	OriginLogContentsLength int
	CommonHeadersLength     int
	// Headers and Contents size if sample batch, just for quota sample percent calculation
	SampleLogHeadersLength  int
	SampleLogContentsLength int
}

func formatLevel(level string) byte {
	level = strings.ToLower(level)
	switch level {
	case "trace":
		return TraceLevel
	case "debug":
		return DebugLevel
	case "info":
		return InfoLevel
	case "notice":
		return NoticeLevel
	case "warn", "warning":
		return WarnLevel
	case "error":
		return ErrorLevel
	case "fatal":
		return FatalLevel
	default:
		return UnknownLevel
	}
}

// warn,error,fatal log
func isErrorLog(level byte) bool {
	return level >= WarnLevel
}

var DefaultEncodeConfig = EncodeConfig{
	SetChecksum:          true,
	CompressCommonHeader: false,
	HeaderCompression:    ZSTD_FAST,
	ContentCompression:   ZSTD_FAST,
}

// NewSampler
// maxSampleLength determines the accuracy of sample, and has a strong correlation with the input parameters of the Sampler.Sample；
// isByteLogBatch means batch contains SDKHeader or not, which sampler need to determine whether it needs to decode SDKHeader first;
func NewSampler(maxSampleLength int, isByteLogBatch bool, config EncodeConfig) *Sampler {
	if maxSampleLength <= 0 {
		maxSampleLength = 100
	}
	return &Sampler{
		maxSampleLength: maxSampleLength,
		isByteLogBatch:  isByteLogBatch,
		config:          config,
	}
}

// AddTenantStreamPair add appLog to errorLog Tenant and Stream relation, so Sample func will rewrite errorLog SDKHeader's
// Tenant and Stream according this mapping relation, otherwise not rewrite.
func (s *Sampler) AddTenantStreamPair(key, value TenantStreamPair) {
	if s.errorLogStreamInfo == nil {
		s.errorLogStreamInfo = make(map[TenantStreamPair]*TenantStreamPair)
	}
	// check value length
	if len(value.TenantName) > math.MaxInt8 {
		value.TenantName = value.TenantName[:math.MaxInt8]
	}
	if len(value.StreamName) > math.MaxInt8 {
		value.StreamName = value.StreamName[:math.MaxInt8]
	}
	s.errorLogStreamInfo[key] = &value
}

func (s *Sampler) SetTenantStreamPair(m map[TenantStreamPair]*TenantStreamPair) {
	s.errorLogStreamInfo = m
}

func (s *Sampler) SetCopyErrorLog(enable bool) {
	s.copyErrorLog = enable
}

func (s *Sampler) SetQuotaPercent(percent int) {
	status := 0
	if percent >= 75 && percent < 100 {
		status = QuotaPercent75
	} else if percent >= 100 {
		status = QuotaPercent100
	}
	s.SetQuotaPercentStatus(status)
}

func (s *Sampler) SetQuotaPercentStatus(status int) {
	s.quotaPercentStatus = status
}

type Sampler struct {
	ds *DeSerializer

	maxSampleLength int
	isByteLogBatch  bool
	sdkHeader       *SDKHeader
	src             []byte
	config          EncodeConfig

	copyErrorLog bool // todo delete
	// AppLog tenant-stream : ErrorLog tenant-stream, use to rewrite errorLog stream field
	errorLogStreamInfo map[TenantStreamPair]*TenantStreamPair

	flagValueIndex     int
	quotaPercentStatus int
}

type TenantStreamPair struct {
	TenantName string
	StreamName string
}

func (s *Sampler) getTenantStream(pair TenantStreamPair) *TenantStreamPair {
	if s.errorLogStreamInfo == nil {
		return nil
	}
	v, ok := s.errorLogStreamInfo[pair]
	if !ok {
		return nil
	}
	return v
}

func (s *Sampler) Read(src []byte) (err error) {
	s.src = src
	pos := 0
	// separate SDKHeader
	if s.isByteLogBatch {
		sdkHeader, readLength, err := DecodeSDKHeader(src)
		if err != nil {
			return err
		}
		s.sdkHeader = sdkHeader
		pos += readLength
	}
	// get headers and contents bytes
	s.ds = &DeSerializer{}

	err = s.ds.Read(src[pos:])
	if err != nil {
		return err
	}
	err = s.ds.prepareAll()
	if err != nil {
		return err
	}
	return nil
}

func (s *Sampler) Reset() {
	if s.ds != nil {
		s.ds.Reset()
	}
	s.src = s.src[:0]
	s.sdkHeader = nil
	s.errorLogStreamInfo = nil
	s.flagValueIndex = 0
	s.quotaPercentStatus = 0
}

func (s *Sampler) GetSDKHeader() *SDKHeader {
	if s.isByteLogBatch {
		return s.sdkHeader
	}
	return nil
}

// GetCommonHeadersValue return a string type value according key,
// and support trans codec.Ipv4 and codec.Ipv6 type to string automatically.
func (s *Sampler) GetCommonHeadersValue(key string) (value string, err error) {
	if s.ds == nil {
		return "", fmt.Errorf("empty data")
	}
	if s.ds.commonHeaders == nil {
		_, err = s.ds.GetCommonHeaders()
		if err != nil {
			return "", err
		}
	}
	v, ok := s.ds.commonHeaders[key]
	if !ok {
		return "", fmt.Errorf("key not exist")
	}
	switch vv := v.(type) {
	case string:
		value = vv
	case Ipv4:
		value = vv.String()
	case Ipv6:
		value = vv.String()
	default:
		return "", fmt.Errorf("unsupport value type")
	}
	return value, nil
}

// if quota >= 75%, rewrite _flag |= 0x02, and if quota >= 100%, rewrite _flag |= 0x06
func (s *Sampler) rewriteSamplePercentFlag() error {
	quotaStatus := s.quotaPercentStatus
	if quotaStatus != QuotaPercent75 && quotaStatus != QuotaPercent100 {
		return nil
	}
	err := s.prepareFlag()
	if err != nil {
		return err
	}
	// rewrite flag
	if s.flagValueIndex > 0 {
		if quotaStatus == QuotaPercent75 {
			s.ds.headerDeCompressData[s.flagValueIndex] |= FlagMaskQuota75Percent
		} else if quotaStatus == QuotaPercent100 {
			s.ds.headerDeCompressData[s.flagValueIndex] |= FlagMaskQuota100Percent
		}
		return nil
	}
	return fmt.Errorf("not exist _flag")
}

func (s *Sampler) prepareFlag() error {
	if s.flagValueIndex > 0 {
		return nil
	}
	// record value location
	commonHeadersBytes := s.ds.headerDeCompressData[s.ds.headerStack.patternLength:]
	length, rl, err := decodeUint32(commonHeadersBytes, 0)
	if err != nil {
		return err
	}
	offset := rl
	index := 0
	for offset < int(length) {
		key, kl, vl, err := decodeKeyValueLength(commonHeadersBytes, offset)
		if err != nil {
			return err
		}
		// find _flag value index
		if key == KEY_FLAGS && vl == 5 {
			// vl = typeByte + uint32_length
			index = offset + kl
			break
		}
		offset += kl + vl
	}
	if index > 0 {
		index += s.ds.headerStack.patternLength + 1
		s.flagValueIndex = index
	}
	return nil
}

// rewriteErrorLogFlag will clear flag byte whether contain errorLog, it only used in sample func.
func (s *Sampler) rewriteErrorLogFlag(isClear bool) error {
	err := s.prepareFlag()
	if err != nil {
		return err
	}
	// rewrite errorLog flag byte
	if s.flagValueIndex > 0 {
		if isClear {
			s.ds.headerDeCompressData[s.flagValueIndex] &= 0xFE
		} else {
			// must contains error log
			s.ds.headerDeCompressData[s.flagValueIndex] |= 0x01
		}
		return nil
	}
	return fmt.Errorf("not exist _flag for errorLog")
}

// Sample batch log according levelSampleMap.
// Keep all CommonHeaders fields; when down sample a log, we keep it's all LogHeader.ReserveHeader fields and
// part LogHeader.CustomizeHeader fields(_level and _location), and add _slog="1" keyValue into LogHeader,
// and keep only LogContent._msg field and clear it.
func (s *Sampler) Sample(levelSamplePercent map[byte]int, defaultPercent int) (
	res, errorLog []byte, sampleBatchInfo *SampleBatchInfo, levelInfos map[byte]LevelInfo, err error) {

	ds := s.ds
	levelQuota := make(map[byte]int)
	logsInfo := make([]logInfo, 0, 4096)

	var sdkHeaderLength, originHeadersLength, originContentsLength int

	frontLength := ds.headerStack.patternLength + ds.headerStack.commonHeadersLength

	if s.copyErrorLog {
		err = s.rewriteErrorLogFlag(true)
		if err != nil {
			return nil, nil, nil, nil, err
		}
	}
	err = s.rewriteSamplePercentFlag()
	if err != nil {
		return nil, nil, nil, nil, err
	}

	logHeadersBytes := ds.headerDeCompressData[frontLength:]
	var errorLogCount int
	errorLogHeadersBytes := make([]byte, LENGTH_BYTES, 100)
	errorLogContentsBytes := make([]byte, 0, 100)
	// use expandLogHeadersBytes if logHeadersBytes not enough to add _slog tag.
	// But it's rare to use expandLogHeaderBytes actually, because LogHeaders has enough buf to fill _slog tag,
	// we only keep _level and _location field, so _spanId space can be used to rewrite _slog.
	var expandLogHeadersBytes []byte
	var useExpandLogHeadersBytes bool

	originHeadersLength = len(ds.headerDeCompressData)
	originContentsLength = len(ds.contentDeCompressData)

	headerPos := 4
	offset := 4
	index := 0

	var newReserveHeaderStart int
	var newCustomizeHeaderStart int
	var levelStart, levelEnd int
	var locationStart, locationEnd int

	var batchLevelList []byte
	var batchSampleList []bool
	var isAllowed bool

	for readLength := 0; readLength < ds.headerStack.logHeadersLength-4; {
		pos := 0
		// 1. decode length
		l, m, err := decodeUint32(logHeadersBytes, offset)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		length := int(l)
		readLength += m
		offset += m
		headerPos += m

		newLogHeaderLength := 0 // ReserveHeader + CustomizeHeader
		//sampledHeaderLength

		// 2. skip ReserveHeader field, copy Timestamp, Source, Context, LogId directly.
		rl, err := decodeMultiValueLength(logHeadersBytes, offset, 4)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		newReserveHeaderStart = offset

		if useExpandLogHeadersBytes {
			// length and ReserveHeader field
			expandLogHeadersBytes = append(expandLogHeadersBytes, logHeadersBytes[offset-LENGTH_BYTES:offset+rl]...)
		} else {
			// headerPos skip length field, only copy ReserveHeader field
			copy(logHeadersBytes[headerPos:headerPos+rl], logHeadersBytes[offset:offset+rl])
			// offset maybe less than headerPos+rl, buf changed, use new index
			newReserveHeaderStart = headerPos
		}

		headerPos += rl
		newLogHeaderLength += rl

		pos += rl
		offset += rl
		// buf changed, use new index
		newCustomizeHeaderStart = newReserveHeaderStart + rl
		//newCustomizeHeaderStart = offset
		start := offset
		// 3. decode CustomizeHeader, count logs level info, record level and location kv field.
		level := ""

		if ds.headerStack.patternLength == 4 {
			for pos < length {
				key, kl, vl, err := decodeKeyValueLength(logHeadersBytes, offset)
				if err != nil {
					return nil, nil, nil, nil, err
				}
				// decode level key field and record kv field
				if key == KEY_LEVEL {
					level, rl, err = decodeShortStrWithType(logHeadersBytes, offset+kl)
					if err != nil {
						return nil, nil, nil, nil, err
					}
					if vl != rl {
						return nil, nil, nil, nil, errors.New("decode _level err")
					}
					levelStart = offset
					levelEnd = offset + kl + vl
				}
				// record kv field
				if key == KEY_LOCATION {
					locationStart = offset
					locationEnd = offset + kl + vl
				}
				pos += kl + vl
				offset += kl + vl
			}
		} else {
			//todo use dic
			return nil, nil, nil, nil, ErrPatternDicNotSupport
		}
		// level sample
		levelValue := formatLevel(level)
		isErrorLogFlag := isErrorLog(levelValue)
		if s.copyErrorLog && isErrorLogFlag {
			errorLogCount++
			// length and ReserveHeader field
			errorLogHeadersBytes = append(errorLogHeadersBytes, emptyFourBytesData...)
			// origin buf changed
			errorLogHeadersBytes = append(errorLogHeadersBytes, logHeadersBytes[newReserveHeaderStart:newCustomizeHeaderStart]...)
		}

		if percent, ok := levelSamplePercent[levelValue]; ok {
			isAllowed = rand.Intn(s.maxSampleLength) < percent
		} else {
			isAllowed = rand.Intn(s.maxSampleLength) < defaultPercent
		}
		sampledHeaderLength := newLogHeaderLength + levelEnd - levelStart + locationEnd - locationStart

		// copy all CustomizeHeader value
		if isAllowed {
			//newCustomizeHeaderLength := offset - oldCustomizeHeaderStart
			//newCustomizeHeaderLength := offset - newCustomizeHeaderStart
			newCustomizeHeaderLength := offset - start
			if useExpandLogHeadersBytes {
				expandLogHeadersBytes = append(expandLogHeadersBytes, logHeadersBytes[newCustomizeHeaderStart:offset]...)
				if s.copyErrorLog && isErrorLogFlag {
					errorLogHeadersBytes = append(errorLogHeadersBytes, logHeadersBytes[newCustomizeHeaderStart:offset]...)
				}
			} else {
				copy(logHeadersBytes[headerPos:headerPos+newCustomizeHeaderLength], logHeadersBytes[start:offset])
				//copy(logHeadersBytes[headerPos:headerPos+newCustomizeHeaderLength], logHeadersBytes[newCustomizeHeaderStart:offset])
				//oldCustomizeHeaderStart < headerPos+newCustomizeHeaderLength????
				if s.copyErrorLog && isErrorLogFlag {
					//errorLogHeadersBytes = append(errorLogHeadersBytes, logHeadersBytes[newCustomizeHeaderStart:offset]...)
					errorLogHeadersBytes = append(errorLogHeadersBytes, logHeadersBytes[headerPos:headerPos+newCustomizeHeaderLength]...)
				}
				headerPos += newCustomizeHeaderLength
			}
			newLogHeaderLength += newCustomizeHeaderLength
		} else {
			// only _level and _location, and _slog tag
			levelLength := levelEnd - levelStart
			locationLength := locationEnd - locationStart
			newLength := levelLength + locationLength + sampledTagLength + newReserveHeaderStart - newCustomizeHeaderStart

			if useExpandLogHeadersBytes || length < newLength {
				// need use expandLogHeaderBytes util decode this logHeader, copy previous part data from logHeadersBytes first
				if !useExpandLogHeadersBytes {
					expandLogHeadersBytes = append(expandLogHeadersBytes, logHeadersBytes[:headerPos]...)
					useExpandLogHeadersBytes = true
				}
				expandLogHeadersBytes = append(expandLogHeadersBytes, logHeadersBytes[levelStart:levelEnd]...)
				expandLogHeadersBytes = append(expandLogHeadersBytes, logHeadersBytes[locationStart:locationEnd]...)
				expandLogHeadersBytes = append(expandLogHeadersBytes, sampledTagBytes...)
				if s.copyErrorLog && isErrorLogFlag {
					errorLogHeadersBytes = append(errorLogHeadersBytes, logHeadersBytes[levelStart:levelEnd]...)
					errorLogHeadersBytes = append(errorLogHeadersBytes, logHeadersBytes[locationStart:locationEnd]...)
					errorLogHeadersBytes = append(errorLogHeadersBytes, sampledTagBytes...)
				}
			} else {
				copy(logHeadersBytes[headerPos:headerPos+levelLength], logHeadersBytes[levelStart:levelEnd])
				headerPos += levelLength
				copy(logHeadersBytes[headerPos:headerPos+locationLength], logHeadersBytes[locationStart:locationEnd])
				headerPos += locationLength
				copy(logHeadersBytes[headerPos:headerPos+sampledTagLength], sampledTagBytes)

				headerPos += sampledTagLength
				if s.copyErrorLog && isErrorLogFlag {
					// logHeadersBytes changed, use new index
					errorLogHeadersBytes = append(errorLogHeadersBytes, logHeadersBytes[headerPos-levelLength-locationLength-sampledTagLength:headerPos]...)
				}
			}
			newLogHeaderLength += levelLength + locationLength + sampledTagLength
		}

		logsInfo = append(logsInfo, logInfo{
			level:            levelValue,
			originHeaderSize: length,
			//originContentSize:  0,
			isSampled:         !isAllowed,
			sampledHeaderSize: sampledHeaderLength,
			realHeaderSize:    newLogHeaderLength,
			//sampledContentSize: 0,
		})
		batchSampleList = append(batchSampleList, isAllowed)
		batchLevelList = append(batchLevelList, levelValue)
		// logHeader length
		if useExpandLogHeadersBytes {
			WriteUint32(expandLogHeadersBytes, len(expandLogHeadersBytes)-LENGTH_BYTES-newLogHeaderLength, uint32(newLogHeaderLength))
		} else {
			WriteUint32(logHeadersBytes, headerPos-LENGTH_BYTES-newLogHeaderLength, uint32(newLogHeaderLength))
		}
		if s.copyErrorLog && isErrorLogFlag {
			WriteUint32(errorLogHeadersBytes, len(errorLogHeadersBytes)-LENGTH_BYTES-newLogHeaderLength, uint32(newLogHeaderLength))
		}

		index++
		readLength += length
		levelQuota[levelValue] += length

	}
	// LogHeaders length
	if useExpandLogHeadersBytes {
		WriteUint32(expandLogHeadersBytes, 0, uint32(len(expandLogHeadersBytes)-LENGTH_BYTES))
	} else {
		WriteUint32(logHeadersBytes, 0, uint32(headerPos-LENGTH_BYTES))
	}
	WriteUint32(errorLogHeadersBytes, 0, uint32(len(errorLogHeadersBytes)-LENGTH_BYTES))

	// sample LogContents, need sample result according LogHeaders
	// keep all LogContent field, or only keep LogContent._msg and clear its value
	var contentPos int
	logContentsBytes := ds.contentDeCompressData
	offset = 0
	index = 0
	var newContentLength int
	for readLength := 0; readLength < ds.stack.logContentsOriginLength; {
		// decode length
		l, m, err := decodeUint32(logContentsBytes, offset)
		if err != nil {
			return nil, nil, nil, nil, err
		}

		readLength += m
		offset += m
		start := contentPos
		contentPos += m
		length := int(l)

		isAllowed = true
		isErrorLogFlag := false
		if index < len(batchSampleList) {
			isAllowed = batchSampleList[index]
			level := batchLevelList[index]
			isErrorLogFlag = isErrorLog(level)
			logsInfo[index].originContentSize = length
			logsInfo[index].sampledContentSize = sampledTagLength
			if isAllowed {
				logsInfo[index].realContentSize = length
			} else {
				logsInfo[index].realContentSize = sampledTagLength
			}
			index++
			levelQuota[level] += length
		}
		if s.copyErrorLog && isErrorLogFlag {
			errorLogContentsBytes = append(errorLogContentsBytes, emptyFourBytesData...)
		}
		if isAllowed {
			// copy all content values
			newContentLength = length
			if s.copyErrorLog && isErrorLogFlag {
				errorLogContentsBytes = append(errorLogContentsBytes, logContentsBytes[offset:offset+length]...)
			}
			copy(logContentsBytes[contentPos:contentPos+length], logContentsBytes[offset:offset+length])
		} else {
			// no enough space, it's rare to happen
			if length < emptyMsgLength {
				newContentLength = length
				copy(logContentsBytes[contentPos:contentPos+length], logContentsBytes[offset:offset+length])
				if s.copyErrorLog && isErrorLogFlag {
					errorLogContentsBytes = append(errorLogContentsBytes, logContentsBytes[offset:offset+length]...)
				}
			} else {
				// delete all values and clear msg value
				newContentLength = emptyMsgLength
				copy(logContentsBytes[contentPos:contentPos+newContentLength], emptyMsgBytes)
				if s.copyErrorLog && isErrorLogFlag {
					errorLogContentsBytes = append(errorLogContentsBytes, emptyMsgBytes...)
				}
			}
		}
		contentPos += newContentLength

		// logContent length
		WriteUint32(logContentsBytes, start, uint32(newContentLength))
		if s.copyErrorLog && isErrorLogFlag {
			WriteUint32(errorLogContentsBytes, len(errorLogContentsBytes)-LENGTH_BYTES-newContentLength, uint32(newContentLength))
		}
		offset += length
		readLength += length

	}

	// get logContentsBytes and logHeadersBytes, begin to encode new data
	logContentsBytes = logContentsBytes[:contentPos]
	if useExpandLogHeadersBytes {
		logHeadersBytes = expandLogHeadersBytes
	} else {
		logHeadersBytes = logHeadersBytes[:headerPos]
	}

	// totalLength := BYTELOG_HEADER_LEN + frontLength + headerPos + CONTENT_COMPRESS_INFO_LEN + contentPos
	d := make([]byte, 0, len(s.src))
	errorLogBytes := make([]byte, 0, len(s.src))

	// decode SDKHeader if need
	if s.isByteLogBatch {
		d = s.sdkHeader.Encode(d)
		sdkHeaderLength = len(d)
		// update tenant and stream for errorLog
		if errorLogCount > 0 {
			tenant := s.sdkHeader.GetTenant()
			stream := s.sdkHeader.GetLogStream()
			errLogTenantStream := s.getTenantStream(TenantStreamPair{tenant, stream})
			if errLogTenantStream == nil {
				// use origin tenant and stream
				errorLogBytes = append(errorLogBytes, d[:sdkHeaderLength]...)
			} else {
				errorLogBytes = append(errorLogBytes, d[:SDK_HEADER_FIXED_LEN]...)
				errorLogBytes = EncodeUint8(errorLogBytes, uint8(len(errLogTenantStream.TenantName)))
				errorLogBytes = append(errorLogBytes, errLogTenantStream.TenantName...)
				errorLogBytes = EncodeUint8(errorLogBytes, uint8(len(errLogTenantStream.StreamName)))
				errorLogBytes = append(errorLogBytes, errLogTenantStream.StreamName...)
				//errorLogBytes = encodeVarStr(errorLogBytes, VarString{})
			}
		}
	}
	startByteLogHeader := len(d)
	errorLogStartByteLogHeader := len(errorLogBytes)
	// decode byteLogHeader
	config := s.config
	ds.byteLogHeader.CompressCommonHeaders(config.CompressCommonHeader)
	ds.byteLogHeader.Compression = byte(config.HeaderCompression)
	ds.byteLogHeader.Checksum = 0
	ds.byteLogHeader.SetChecksum(config.SetChecksum)
	d = ds.byteLogHeader.Encode(d)
	startHeadersRegion := len(d)

	// copy byteLogHeader bytes
	if errorLogCount > 0 {
		errorLogBytes = append(errorLogBytes, d[startByteLogHeader:startHeadersRegion]...)
	}
	// compress HeaderRegion
	if config.HeaderCompression != None {
		headerCompressor, err := GetGlobalCompressor(config.HeaderCompression)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if !config.CompressCommonHeader {
			d = append(d, ds.headerDeCompressData[:frontLength]...)
			d, err = headerCompressor.Compress(d, logHeadersBytes)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			if errorLogCount > 0 {
				err = s.rewriteErrorLogFlag(false)
				if err != nil {
					return nil, nil, nil, nil, err
				}
				errorLogBytes = append(errorLogBytes, ds.headerDeCompressData[:frontLength]...)
				errorLogBytes, err = headerCompressor.Compress(errorLogBytes, errorLogHeadersBytes)
				if err != nil {
					return nil, nil, nil, nil, err
				}
			}
		} else {
			headersLength := frontLength + len(logHeadersBytes)
			allHeadersData := make([]byte, headersLength)
			copy(allHeadersData[:frontLength], ds.headerDeCompressData[:frontLength])
			copy(allHeadersData[frontLength:], logHeadersBytes)
			d, err = headerCompressor.Compress(d, allHeadersData)
			if err != nil {
				return nil, nil, nil, nil, err
			}

			if errorLogCount > 0 {
				err = s.rewriteErrorLogFlag(false)
				if err != nil {
					return nil, nil, nil, nil, err
				}
				headersLength = frontLength + len(errorLogHeadersBytes)
				allErrorLogHeadersData := make([]byte, headersLength)
				copy(allErrorLogHeadersData[:frontLength], ds.headerDeCompressData[:frontLength])
				copy(allErrorLogHeadersData[frontLength:], errorLogHeadersBytes)
				errorLogBytes, err = headerCompressor.Compress(errorLogBytes, allErrorLogHeadersData)
				if err != nil {
					return nil, nil, nil, nil, err
				}
			}
		}
	} else {
		d = append(d, ds.headerDeCompressData[:frontLength]...)
		d = append(d, logHeadersBytes...)

		if errorLogCount > 0 {
			err = s.rewriteErrorLogFlag(false)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			errorLogBytes = append(errorLogBytes, ds.headerDeCompressData[:frontLength]...)
			errorLogBytes = append(errorLogBytes, errorLogHeadersBytes...)
		}
	}
	endHeadersRegion := len(d)
	errorLogEndHeadersRegion := 0
	errorLogStartLogContents := 0
	// reserve ContentRegion length field and write compression type
	d = encodeContentCompressInfo(d, byte(config.ContentCompression))
	startLogContents := len(d)

	if errorLogCount > 0 {
		errorLogEndHeadersRegion = len(errorLogBytes)
		errorLogBytes = encodeContentCompressInfo(errorLogBytes, byte(config.ContentCompression))
		errorLogStartLogContents = len(errorLogBytes)
	}
	// compress Contents
	if config.ContentCompression != None {
		contentCompressor, err := GetGlobalCompressor(config.ContentCompression)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		d, err = contentCompressor.Compress(d, logContentsBytes)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if errorLogCount > 0 {
			errorLogBytes, err = contentCompressor.Compress(errorLogBytes, errorLogContentsBytes)
			if err != nil {
				return nil, nil, nil, nil, err
			}
		}
	} else {
		d = append(d, logContentsBytes...)
		if errorLogCount > 0 {
			errorLogBytes = append(errorLogBytes, errorLogContentsBytes...)
		}
	}
	endLogContents := len(d)
	errorLogEndLogContents := len(errorLogBytes)

	// Total length
	WriteUint32(d, startByteLogHeader+BYTELOG_HEADER_LEN-DISTANCE_BTW_BODY_LEN_POS_AND_BODY, uint32(endLogContents-startHeadersRegion))
	// HeaderRegion compressed length
	WriteUint32(d, startByteLogHeader+BYTELOG_HEADER_COMP_HEADER_LENGTH_POS, uint32(endHeadersRegion-startHeadersRegion))
	// HeaderRegion origin length
	WriteUint32(d, startByteLogHeader+BYTELOG_HEADER_ORIN_HEADER_LENGTH_POS, uint32(len(logHeadersBytes)+frontLength))
	// ContentRegion compressed length
	WriteUint32(d, endHeadersRegion, uint32(endLogContents-startLogContents))
	// ContentRegion origin length
	WriteUint32(d, endHeadersRegion+LENGTH_BYTES, uint32(contentPos))

	if errorLogCount > 0 {
		errorLogStartHeadersRegion := errorLogStartByteLogHeader + startHeadersRegion - startByteLogHeader
		WriteUint32(errorLogBytes, errorLogStartByteLogHeader+BYTELOG_HEADER_LEN-DISTANCE_BTW_BODY_LEN_POS_AND_BODY, uint32(errorLogEndLogContents-errorLogStartHeadersRegion))
		WriteUint32(errorLogBytes, errorLogStartByteLogHeader+BYTELOG_HEADER_COMP_HEADER_LENGTH_POS, uint32(errorLogEndHeadersRegion-errorLogStartHeadersRegion))
		WriteUint32(errorLogBytes, errorLogStartByteLogHeader+BYTELOG_HEADER_ORIN_HEADER_LENGTH_POS, uint32(len(errorLogHeadersBytes)+frontLength))
		WriteUint32(errorLogBytes, errorLogEndHeadersRegion, uint32(errorLogEndLogContents-errorLogStartLogContents))
		WriteUint32(errorLogBytes, errorLogEndHeadersRegion+LENGTH_BYTES, uint32(len(errorLogContentsBytes)))
	}

	// update checksum
	if s.config.SetChecksum {
		crc := crc32.ChecksumIEEE(d[startByteLogHeader:])
		WriteUint32(d, startByteLogHeader+BYTELOG_HEADER_CHECKSUM_POS, crc)
		if errorLogCount > 0 {
			crc = crc32.ChecksumIEEE(errorLogBytes[errorLogStartByteLogHeader:])
			WriteUint32(errorLogBytes, errorLogStartByteLogHeader+BYTELOG_HEADER_CHECKSUM_POS, crc)
		}
	}

	// rewrite SDKHeader length
	if s.isByteLogBatch {
		WriteUint32(d, SDK_HEADER_LENGTH_POS, uint32(len(d)-LENGTH_BYTES))
		if errorLogCount > 0 {
			WriteUint32(errorLogBytes, SDK_HEADER_LENGTH_POS, uint32(len(errorLogBytes)-LENGTH_BYTES))
			WriteUint32(errorLogBytes, SDK_HEADER_LOG_COUNT_POS, uint32(errorLogCount))
		}
	}

	sampleBatchInfo, levelInfos = sum(logsInfo)
	sampleBatchInfo.CommonHeadersLength = ds.headerStack.commonHeadersLength
	sampleBatchInfo.OriginLogSize = sdkHeaderLength + originHeadersLength + originContentsLength + BYTELOG_HEADER_LEN + CONTENT_COMPRESS_INFO_LEN
	sampleBatchInfo.LogSize = sdkHeaderLength + frontLength + headerPos + contentPos + BYTELOG_HEADER_LEN + CONTENT_COMPRESS_INFO_LEN

	return d, errorLogBytes, sampleBatchInfo, levelInfos, nil
}

// Deprecated: use Sampler
func Sample(src []byte, isByteLogBatch bool, levelSamplePercent map[byte]int, defaultPercent int, config EncodeConfig) (res []byte, levelInfos map[byte]LevelInfo, err error) {
	if defaultPercent < 0 {
		defaultPercent = 0
	} else if defaultPercent > 100 {
		defaultPercent = 100
	}
	levelQuota := make(map[byte]int)
	logsInfo := make([]logInfo, 0, 4096)

	pos := 0
	// separate SDKHeader
	var sdkHeader *SDKHeader
	var readLength int
	if isByteLogBatch {
		sdkHeader, readLength, err = DecodeSDKHeader(src)
		if err != nil {
			return nil, nil, err
		}
		pos += readLength
	}
	// get headers and contents bytes
	ds := &DeSerializer{}
	defer ds.Reset()
	err = ds.Read(src[pos:])
	if err != nil {
		return nil, nil, err
	}
	err = ds.prepareAll()
	if err != nil {
		return nil, nil, err
	}
	// sample LogHeaders
	// skip pattern and commonHeaders
	frontLength := ds.headerStack.patternLength + ds.headerStack.commonHeadersLength
	logHeadersBytes := ds.headerDeCompressData[frontLength:]

	headerPos := 4
	offset := 4
	index := 0

	var oldCustomizeHeaderStart int
	var levelStart, levelEnd int
	var locationStart, locationEnd int

	var batchLevelList []byte
	var batchSampleList []bool
	var isAllowed bool

	for readLength := 0; readLength < ds.headerStack.logHeadersLength-4; {
		pos := 0
		// 1. decode length
		l, m, err := decodeUint32(logHeadersBytes, offset)
		if err != nil {
			return nil, nil, err
		}
		length := int(l)
		readLength += m
		offset += m
		headerPos += m

		newLogHeaderLength := 0 // ReserveHeader + CustomizeHeader

		// 2. skip ReserveHeader field, copy Timestamp, Source, Context, LogId directly.
		rl, err := decodeMultiValueLength(logHeadersBytes, offset, 4)
		if err != nil {
			return nil, nil, err
		}
		copy(logHeadersBytes[headerPos:headerPos+rl], logHeadersBytes[offset:offset+rl])

		headerPos += rl
		newLogHeaderLength += rl

		pos += rl
		offset += rl

		oldCustomizeHeaderStart = offset
		// 3. decode CustomizeHeader, count logs level info, record level and location kv field.
		level := Debug

		if ds.headerStack.patternLength == 4 {
			for pos < length {
				key, kl, vl, err := decodeKeyValueLength(logHeadersBytes, offset)
				if err != nil {
					return nil, nil, err
				}
				// decode level key field and record kv field
				if key == KEY_LEVEL {
					level, rl, err = decodeShortStrWithType(logHeadersBytes, offset+kl)
					if err != nil {
						return nil, nil, err
					}
					if vl != rl {
						return nil, nil, errors.New("decode _level err")
					}
					levelStart = offset
					levelEnd = offset + kl + vl
				}
				// record kv field
				if key == KEY_LOCATION {
					locationStart = offset
					locationEnd = offset + kl + vl
				}
				pos += kl + vl
				offset += kl + vl
			}
		} else {
			//todo use dic
			return nil, nil, ErrPatternDicNotSupport
		}
		// level sample
		levelValue := formatLevel(level)
		if percent, ok := levelSamplePercent[levelValue]; ok {
			isAllowed = rand.Intn(100) < percent
		} else {
			isAllowed = rand.Intn(100) < defaultPercent
		}
		// copy all CustomizeHeader value
		if isAllowed {
			newCustomizeHeaderLength := offset - oldCustomizeHeaderStart
			copy(logHeadersBytes[headerPos:headerPos+newCustomizeHeaderLength], logHeadersBytes[oldCustomizeHeaderStart:offset])
			headerPos += newCustomizeHeaderLength
			newLogHeaderLength += newCustomizeHeaderLength
		} else {
			// only level and location
			levelLength := levelEnd - levelStart
			locationLength := locationEnd - locationStart
			copy(logHeadersBytes[headerPos:headerPos+levelLength], logHeadersBytes[levelStart:levelEnd])
			headerPos += levelLength
			copy(logHeadersBytes[headerPos:headerPos+locationLength], logHeadersBytes[locationStart:locationEnd])
			headerPos += locationLength
			newLogHeaderLength += levelLength + locationLength
		}

		logsInfo = append(logsInfo, logInfo{
			level:            levelValue,
			originHeaderSize: length,
			//originContentSize:  0,
			isSampled:         !isAllowed,
			sampledHeaderSize: newLogHeaderLength,
			//sampledContentSize: 0,
		})
		batchSampleList = append(batchSampleList, isAllowed)
		batchLevelList = append(batchLevelList, levelValue)
		// logHeader length
		WriteUint32(logHeadersBytes, headerPos-4-newLogHeaderLength, uint32(newLogHeaderLength))

		index++
		readLength += length
		levelQuota[levelValue] += length

	}
	// LogHeaders length
	WriteUint32(logHeadersBytes, 0, uint32(headerPos-4))

	// sample LogContents, need sample result according LogHeaders
	var contentPos int
	logContentsBytes := ds.contentDeCompressData
	offset = 0
	index = 0
	var newContentLength int
	for readLength := 0; readLength < ds.stack.logContentsOriginLength; {
		// decode length
		l, m, err := decodeUint32(logContentsBytes, offset)
		if err != nil {
			return nil, nil, err
		}

		readLength += m
		offset += m
		start := contentPos
		contentPos += m
		length := int(l)

		isAllowed := true
		if index < len(batchSampleList) {
			isAllowed = batchSampleList[index]
			level := batchLevelList[index]
			logsInfo[index].originContentSize = length
			logsInfo[index].sampledContentSize = sampledTagLength
			index++
			levelQuota[level] += length
		}
		if isAllowed {
			// copy all content values
			newContentLength = length
			copy(logContentsBytes[contentPos:contentPos+length], logContentsBytes[offset:offset+length])
		} else {
			// no enough space for sample tag
			if length < sampledTagLength {
				newContentLength = length
				copy(logContentsBytes[contentPos:contentPos+length], logContentsBytes[offset:offset+length])
			} else {
				// delete all values and only add sampled tag
				newContentLength = sampledTagLength
				copy(logContentsBytes[contentPos:contentPos+newContentLength], sampledTagBytes)
			}
		}
		contentPos += newContentLength

		WriteUint32(logContentsBytes, start, uint32(newContentLength))
		offset += length
		readLength += length

	}

	// Encode new data
	logContentsBytes = logContentsBytes[:contentPos]
	logHeadersBytes = logHeadersBytes[:headerPos]

	//totalLength := BYTELOG_HEADER_LEN + frontLength + headerPos + CONTENT_COMPRESS_INFO_LEN + contentPos
	d := make([]byte, 0, len(src))

	if isByteLogBatch {
		d = sdkHeader.Encode(d)
	}
	startByteLogHeader := len(d)

	ds.byteLogHeader.CompressCommonHeaders(config.CompressCommonHeader)
	ds.byteLogHeader.Compression = byte(config.HeaderCompression)
	ds.byteLogHeader.Checksum = 0
	ds.byteLogHeader.SetChecksum(config.SetChecksum)
	d = ds.byteLogHeader.Encode(d)
	startHeadersRegion := len(d)

	if config.HeaderCompression != None {
		headerCompressor, err := GetGlobalCompressor(config.HeaderCompression)
		if err != nil {
			return nil, nil, err
		}
		if !config.CompressCommonHeader {
			d = append(d, ds.headerDeCompressData[:frontLength]...)
			d, err = headerCompressor.Compress(d, logHeadersBytes)
			if err != nil {
				return nil, nil, err
			}
		} else {
			allHeadersData := ds.headerDeCompressData[:frontLength+headerPos]
			d, err = headerCompressor.Compress(d, allHeadersData)
			if err != nil {
				return nil, nil, err
			}
		}
	} else {
		d = append(d, ds.headerDeCompressData[:frontLength+headerPos]...)
	}
	endHeadersRegion := len(d)
	// reserve ContentRegion length field and write compression type
	d = encodeContentCompressInfo(d, byte(config.ContentCompression))
	startLogContents := len(d)
	if config.ContentCompression != None {
		contentCompressor, err := GetGlobalCompressor(config.ContentCompression)
		if err != nil {
			return nil, nil, err
		}
		d, err = contentCompressor.Compress(d, logContentsBytes)
		if err != nil {
			return nil, nil, err
		}
	} else {
		d = append(d, logContentsBytes...)
	}
	endLogContents := len(d)

	// Total length
	WriteUint32(d, startByteLogHeader+BYTELOG_HEADER_LEN-DISTANCE_BTW_BODY_LEN_POS_AND_BODY, uint32(endLogContents-startHeadersRegion))
	// HeaderRegion compressed length
	WriteUint32(d, startByteLogHeader+BYTELOG_HEADER_COMP_HEADER_LENGTH_POS, uint32(endHeadersRegion-startHeadersRegion))
	// HeaderRegion origin length
	WriteUint32(d, startByteLogHeader+BYTELOG_HEADER_ORIN_HEADER_LENGTH_POS, uint32(headerPos+frontLength))
	// ContentRegion compressed length
	WriteUint32(d, endHeadersRegion, uint32(endLogContents-startLogContents))
	// ContentRegion origin length
	WriteUint32(d, endHeadersRegion+LENGTH_BYTES, uint32(contentPos))

	// update checksum
	if config.SetChecksum {
		crc := crc32.ChecksumIEEE(d[startByteLogHeader:])
		WriteUint32(d, startByteLogHeader+BYTELOG_HEADER_CHECKSUM_POS, crc)
	}

	// rewrite SDKHeader length
	if isByteLogBatch {
		WriteUint32(d, SDK_HEADER_LENGTH_POS, uint32(len(d)-LENGTH_BYTES))
	}

	_, levelInfos = sum(logsInfo)

	return d, levelInfos, nil
}
