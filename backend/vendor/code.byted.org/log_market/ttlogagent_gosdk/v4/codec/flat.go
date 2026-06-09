package codec

import (
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"sync/atomic"

	"github.com/google/uuid"
)

type FlatStructure []byte

// FlatByteLogBatch is an implementation of LogBatch.
// It is a replacement of ByteLogBatch.
type FlatByteLogBatch struct {
	sdkHeader  FlatStructure
	logMessage FlatByteLogMessage

	//meta info
	tenant    string
	logStream string
}

// NewDefaultFlatByteLogBatch creates a FlatByteLogBatch with <tenant, logStream> = <"argos", "argos">
func NewDefaultFlatByteLogBatch() *FlatByteLogBatch {
	return NewFlatByteLogBatch(DefaultTenant, DefaultLogStreamName)
}

// NewFlatByteLogBatch creates a ByteLogBatch with specified tenant and logStream.
func NewFlatByteLogBatch(tenant, logStreamName string, ops ...ByteLogOption) *FlatByteLogBatch {
	sdkHeader := newSDKHeaderBuf(tenant, logStreamName)
	byteLogHeader := newByteLogHeaderBuf()
	pattern := make([]byte, 4)
	commonHeaders := newCommonHeadersBuf(NewDefaultCommonHeaders())
	f := newFlatByteLogBatch(sdkHeader, byteLogHeader, pattern, commonHeaders,
		DEFAULT_CONTENT_COMPRESSION, ops...)
	f.tenant = tenant
	f.logStream = logStreamName
	return f
}

func newFlatByteLogBatch(sdkHeader, byteLogHeader, pattern, commonHeaders FlatStructure,
	contentCompression byte, ops ...ByteLogOption) *FlatByteLogBatch {
	logMessage := newFlatByteLogMessage(byteLogHeader, pattern, commonHeaders, contentCompression, ops...)

	fb := &FlatByteLogBatch{
		sdkHeader:  sdkHeader,
		logMessage: *logMessage,
	}
	return fb
}

func (fb *FlatByteLogBatch) GetTenant() string {
	return fb.tenant
}

func (fb *FlatByteLogBatch) GetLogStream() string {
	return fb.logStream
}

// Encode encodes LogBatch and append the bytes to the buf.
func (fb *FlatByteLogBatch) Encode(buf []byte) ([]byte, error) {
	if fb.LogNumber() == 0 {
		return buf, ErrEmptyBatch
	}

	start := len(buf)
	var err error
	buf = append(buf, fb.sdkHeader...)
	buf, err = fb.logMessage.Encode(buf)
	if err != nil {
		buf = buf[:start]
		return buf, err
	}

	fb.updateSDKHeader(buf[start:], start, buf)
	return buf, nil
}

// AppendLog trys to append a data pack to ByteLogMessage. It may fail in cases like:
// 1. The log is invalid, for example_for_agent, it has no timestamp or no file location.
// 2. The log doesn't have a log header.
// 3. The log doesn't have a log content.
// 4. The number of logs exceeds 4096.
// 5. The size of the log batch exceeds 128k.
// 6. The log is not in the same time window (the same minute).
func (fb *FlatByteLogBatch) AppendLog(log *DataPack) error {
	return fb.logMessage.AppendLog(log)
}

// SetCommonHeaders sets the CommonHeaders of the log batch. It also copies the uuid.
func (fb *FlatByteLogBatch) SetCommonHeaders(commonHeaders *CommonHeaders) {
	fb.logMessage.SetCommonHeaders(commonHeaders)
}

// SetNewCommonHeaders sets the CommonHeaders of the log batch.
// It is usually called for only once.
func (fb *FlatByteLogBatch) SetNewCommonHeaders(commonHeaders *CommonHeaders) {
	fb.logMessage.SetNewCommonHeaders(commonHeaders)
}

// SetSDKHeader updates the sdkHeader.
// This function can update the tenant name or log stream name.
func (fb *FlatByteLogBatch) SetSDKHeader(sdkHeader *SDKHeader) {
	buf := make([]byte, 0, sdkHeader.Size())
	buf = sdkHeader.Encode(buf)
	fb.sdkHeader = buf
}

// SetUUID uses an uuid to compose the batchId in the CommonHeaders.
func (fb *FlatByteLogBatch) SetUUID(newUUId uuid.UUID) {
	fb.logMessage.SetUUID(newUUId)
}

// SetLogStreamId uses a string to compose the batchId in the CommonHeaders.
func (fb *FlatByteLogBatch) SetLogStreamId(logStreamId string) error {
	return fb.logMessage.SetLogStreamId(logStreamId)
}

// SetHeaderCompression sets the header compression option.
func (fb *FlatByteLogBatch) SetHeaderCompression(compressType CompressType, useGlobalCompress ...bool) {
	fb.logMessage.SetHeaderCompression(compressType, useGlobalCompress...)
}

// SetContentCompression sets the content compression option.
func (fb *FlatByteLogBatch) SetContentCompression(compressType CompressType, useGlobalCompress ...bool) {
	fb.logMessage.SetContentCompression(compressType, useGlobalCompress...)

}

// LogNumber returns the number of logs in the batch.
func (fb *FlatByteLogBatch) LogNumber() int {
	return fb.logMessage.LogNumber()
}

// Size returns the size of the byte slice after encoding without compression.
func (fb *FlatByteLogBatch) Size() int {
	if fb.LogNumber() == 0 {
		return 0
	}
	return len(fb.sdkHeader) + fb.logMessage.Size()
}

// Clear removes all logs in ByteLogMessage. This function will NOT Clear the common headers.
func (fb *FlatByteLogBatch) Clear() {
	fb.logMessage.Clear()
	WriteUint16(fb.sdkHeader, SDK_HEADER_LOG_COUNT_POS, 0)
	WriteUint64(fb.sdkHeader, SDK_HEADER_TIMESTAMP_POS, 0)
	WriteUint32(fb.sdkHeader, SDK_HEADER_LENGTH_POS, 0)
}

// Reset removes all logs in ByteLogMessage. This function will Clear the common headers and buffers.
func (fb *FlatByteLogBatch) Reset() {
	fb.Clear()
	fb.logMessage.Reset()
}

// EnableDebug sets the LSB in ByteLogMessage's header's reserved field.
func (fb *FlatByteLogBatch) EnableDebug(isEnabled bool) {
	fb.logMessage.EnableDebug(isEnabled)
}

// FirstTimeStamp returns the earliest log timestamp in the batch.
func (fb *FlatByteLogBatch) FirstTimeStamp() uint64 {
	return fb.logMessage.FirstTimeStamp()
}

// LastTimeStamp returns the latest log timestamp in the batch.
func (fb *FlatByteLogBatch) LastTimeStamp() uint64 {
	return fb.logMessage.LastTimeStamp()
}

// updateSDKHeader sets the lengths in the SDKHeaders
func (fb *FlatByteLogBatch) updateSDKHeader(sdkHeader []byte, start int, buf []byte) {
	WriteUint32(sdkHeader, SDK_HEADER_LENGTH_POS, uint32(len(buf)-start-LENGTH_BYTES))
	WriteUint16(sdkHeader, SDK_HEADER_LOG_COUNT_POS, uint16(fb.LogNumber()))
	WriteUint64(sdkHeader, SDK_HEADER_TIMESTAMP_POS, fb.FirstTimeStamp())
}

// SizeOfByteLog returns the length of the internal ByteLogMessage.
// Users may use this function to directly get the bytes of the ByteLogMessage without SDK Headers.
func (fb *FlatByteLogBatch) SizeOfByteLog() int {
	return fb.logMessage.compressedSize()
}

// ContainsErrorLog indicates whether there is a log of which the level is WARN, ERROR, or FATAL.
func (fb *FlatByteLogBatch) ContainsErrorLog() bool {
	return fb.logMessage.ContainsErrorLog()
}

// IncrId increases the seq id of the log batch. This is function is public since the outer sender may send one batch
// for several times, and it is responsible for increasing the id.
func (fb *FlatByteLogBatch) IncrId() {
	fb.logMessage.IncrId()
}

// IncrId increases the seq id of the log batch. This is function is public since the outer sender may send one batch
// for several times, and it is responsible for increasing the id.
func (fm *FlatByteLogMessage) IncrId() {
	fm.seqId++
}

// SetSeqId sets the seqId of the internal byteLogMessage.
// It is dangerous to use this function. Please understand what you are doing.
func (fb *FlatByteLogBatch) SetSeqId(newSeqId uint64) {
	fb.logMessage.SetSeqId(newSeqId)
}

// SetSeqId sets the seqId of the byteLogMessage.
// It is dangerous to use this function. Please understand what you are doing.
func (fm *FlatByteLogMessage) SetSeqId(newSeqId uint64) {
	fm.seqId = newSeqId
}

// GetSeqId returns the internal ByteLogMessage.
func (fb *FlatByteLogBatch) GetSeqId() uint64 {
	return fb.logMessage.GetSeqId()
}

// GetSeqId returns the internal ByteLogMessage.
func (fm *FlatByteLogMessage) GetSeqId() uint64 {
	return fm.seqId
}

func (fm *FlatByteLogBatch) SetTruncateLargeLogs(isEnabled bool) {
	fm.logMessage.SetTruncateLargeLogs(isEnabled)
}

func (fm *FlatByteLogMessage) SetTruncateLargeLogs(isEnabled bool) {
	fm.notTruncateLargeLog = !isEnabled
}

func (fb *FlatByteLogBatch) SetMessageSizeLimit(n int) {
	fb.logMessage.SetMessageSizeLimit(n)
}

func (fb *FlatByteLogBatch) GetMessageSizeLimit() int {
	return fb.logMessage.GetMessageSizeLimit()
}

func (fb *FlatByteLogBatch) SetLargeMessage() {
	fb.logMessage.SetLargeMessage()
}

func (fb *FlatByteLogBatch) SetChecksum(isEnabled bool) {
	fb.logMessage.SetChecksum(isEnabled)
}

func (fb *FlatByteLogBatch) SetErrorLogSeparated(isEnabled bool) {
	fb.logMessage.SetErrorLogSeparated(isEnabled)
}

func (fb *FlatByteLogBatch) GetLogMessage() LogMessage {
	return &fb.logMessage
}

func (fm *FlatByteLogMessage) SetMessageSizeLimit(n int) {
	if n <= 1000 {
		return
	}
	fm.oneMessageSizeLimit = n
}

func (fm *FlatByteLogMessage) GetMessageSizeLimit() int {
	return fm.oneMessageSizeLimit
}

func (fm *FlatByteLogMessage) SetLargeMessage() {
	fm.oneMessageSizeLimit = largeMessageLimitByte
}

func (fm *FlatByteLogMessage) SetChecksum(isEnabled bool) {
	if isEnabled {
		fm.byteLogHeader[BYTELOG_HEADER_RESERVED_FLAG_POS] |= 0x08
	} else {
		fm.byteLogHeader[BYTELOG_HEADER_RESERVED_FLAG_POS] &= 0xF7
	}
}

func (fm *FlatByteLogMessage) SetErrorLogSeparated(isEnabled bool) {
	fm.isSeparateErrorLog = isEnabled
}

// FlatByteLogMessage is an implementation of StreamLogBatch. It is a replacement of ByteLogMessage.
type FlatByteLogMessage struct {
	byteLogHeader       FlatStructure
	pattern             FlatStructure
	commonHeaders       FlatStructure // Note that this CommonHeaders has extra 8 bytes (2 uint32) for seqIdOffset and flagOffset.
	logHeaders          FlatStructure
	contentCompressInfo FlatStructure
	logContents         FlatStructure

	compressedHeaderArea  FlatStructure
	compressedLogContents FlatStructure

	firstTimeStamp uint64
	lastTimeStamp  uint64
	logNumber      int32
	seqId          uint64
	flags          uint32

	headerCompressor  Compressor
	contentCompressor Compressor

	oneMessageSizeLimit int
	// this is temp for separate error log
	isSeparateErrorLog bool

	// truncate large log
	notTruncateLargeLog bool
}

func newFlatByteLogMessage(byteLogHeader, pattern, commonHeaders FlatStructure,
	contentCompression byte, ops ...ByteLogOption) *FlatByteLogMessage {
	fm := &FlatByteLogMessage{
		byteLogHeader:       byteLogHeader,
		pattern:             pattern,
		commonHeaders:       commonHeaders,
		logHeaders:          make([]byte, 4, 1024),
		contentCompressInfo: newCompressionInfoBuf(contentCompression),
		logContents:         make([]byte, 0, 1024),
		seqId:               0,
		flags:               0,
		oneMessageSizeLimit: oneMessageLimitByte,
	}

	for _, op := range ops {
		op(fm)
	}
	return fm
}

func NewFlatByteLogMessage(ops ...ByteLogOption) *FlatByteLogMessage {
	byteLogHeader := newByteLogHeaderBuf()
	pattern := make([]byte, 4)
	commonHeaders := newCommonHeadersBuf(NewDefaultCommonHeaders())
	f := newFlatByteLogMessage(byteLogHeader, pattern, commonHeaders,
		DEFAULT_CONTENT_COMPRESSION, ops...)
	return f
}

// Encode encodes ByteLogMessage and appends the content to the buf.
func (fm *FlatByteLogMessage) Encode(buf []byte) ([]byte, error) {
	if fm.LogNumber() == 0 {
		return buf, ErrEmptyBatch
	}
	start := len(buf)
	var err error
	buf, err = fm.finalize(buf)
	if err != nil {
		return buf[:start], err
	}
	if fm.byteLogHeader[BYTELOG_HEADER_RESERVED_FLAG_POS]&0x08 != 0 {
		crc := crc32.ChecksumIEEE(buf[start:])
		WriteUint32(buf, start+BYTELOG_HEADER_CHECKSUM_POS, crc)
	}
	return buf, nil
}

// finalize update the necessary fields and copy the data to the buf
func (fm *FlatByteLogMessage) finalize(buf []byte) ([]byte, error) {
	var err error
	err = fm.prepare()

	if err != nil {
		return buf, err
	}
	buf = append(buf, fm.byteLogHeader...)

	if fm.headerCompressor != nil {
		buf = append(buf, fm.compressedHeaderArea...)
	} else {
		buf = append(buf, fm.pattern...)
		buf = append(buf, getRealCommonHeaders(fm.commonHeaders)...)
		buf = append(buf, fm.logHeaders...)
	}

	buf = append(buf, fm.contentCompressInfo...)

	if fm.contentCompressor != nil {
		buf = append(buf, fm.compressedLogContents...)
	} else {
		buf = append(buf, fm.logContents...)
	}

	return buf, nil
}

// prepare sets the CommonHeaders, SDKHeaders, ByteLogHeader, LogHeadersArea, contentCompressInfo
// It also compresses the header area and content area if necessary.
func (fm *FlatByteLogMessage) prepare() error {
	var err error
	fm.prepareCommonHeaders() // Update seqId and flag in the CommonHeaders.

	// Update the length of the logHeader before compression.
	WriteUint32(fm.logHeaders, 0, uint32(len(fm.logHeaders)-LENGTH_BYTES))

	if fm.headerCompressor != nil {
		// If the header area needs to be compressed, we compress the pattern,  common headers, and log headers.
		fm.compressedHeaderArea = fm.compressedHeaderArea[:0] // Clear the compressed bytes first.
		tempBufForOriginalHeaderArea := NewPacket(0)
		defer PutPacket(tempBufForOriginalHeaderArea)

		// Not compress common headers
		if fm.byteLogHeader[BYTELOG_HEADER_RESERVED_FLAG_POS]&0x02 != 0 {
			fm.compressedHeaderArea = append(fm.compressedHeaderArea, fm.pattern...)
			fm.compressedHeaderArea = append(fm.compressedHeaderArea, getRealCommonHeaders(fm.commonHeaders)...)
			*tempBufForOriginalHeaderArea = append(*tempBufForOriginalHeaderArea, fm.logHeaders...)
		} else {
			*tempBufForOriginalHeaderArea = append(*tempBufForOriginalHeaderArea, fm.pattern...)
			*tempBufForOriginalHeaderArea = append(*tempBufForOriginalHeaderArea, getRealCommonHeaders(fm.commonHeaders)...)
			*tempBufForOriginalHeaderArea = append(*tempBufForOriginalHeaderArea, fm.logHeaders...)
		}

		fm.compressedHeaderArea, err = fm.headerCompressor.Compress(fm.compressedHeaderArea, *tempBufForOriginalHeaderArea)
		if err != nil {
			fm.compressedHeaderArea = fm.compressedHeaderArea[:0]
			return err
		}
		// Don't forget to update the compressed header length in the ByteLog Header.
		WriteUint32(fm.byteLogHeader, BYTELOG_HEADER_COMP_HEADER_LENGTH_POS, uint32(len(fm.compressedHeaderArea)))
	} else {
		// Else we just use the actual header length as the compressed header length in the ByteLog Header.
		WriteUint32(fm.byteLogHeader, BYTELOG_HEADER_COMP_HEADER_LENGTH_POS, uint32(fm.SizeOfByteLogHeaderArea()))
	}

	// Update the original header length in the ByteLog Header.
	WriteUint32(fm.byteLogHeader, BYTELOG_HEADER_ORIN_HEADER_LENGTH_POS, uint32(fm.SizeOfByteLogHeaderArea()))

	// Similar logic to the header area.
	if fm.contentCompressor != nil {
		fm.compressedLogContents = fm.compressedLogContents[:0] // Clear the compressed bytes first.
		fm.compressedLogContents, err = fm.contentCompressor.Compress(fm.compressedLogContents, fm.logContents)
		if err != nil {
			fm.compressedLogContents = fm.compressedLogContents[:0]
			return err
		}
		WriteUint32(fm.contentCompressInfo, CONTENT_COMPRESSED_LEN_POS, uint32(len(fm.compressedLogContents)))
	} else {
		WriteUint32(fm.contentCompressInfo, CONTENT_COMPRESSED_LEN_POS, uint32(len(fm.logContents)))
	}

	// Update the original content length in the content compress info.
	WriteUint32(fm.contentCompressInfo, CONTENT_ORIGIN_LEN_POS, uint32(len(fm.logContents)))

	// Update the total length in the ByteLog header.
	WriteUint32(fm.byteLogHeader, BYTELOG_HEADER_TOTAL_LENGTH_POS, uint32(fm.compressedSize()-BYTELOG_HEADER_LEN))
	return nil
}

// AppendLog trys to append a data pack to ByteLogMessage. It may fail in cases like:
// 1. The log is invalid, for example_for_agent, it has no timestamp or no file location.
// 2. The log doesn't have a log header.
// 3. The log doesn't have a log content.
// 4. The number of logs exceeds 4096.
// 5. The size of the log batch exceeds 128k.
// 6. The log is not in the same time window (the same minute).
func (fm *FlatByteLogMessage) AppendLog(log *DataPack) error {
	err := log.Validate(fm.oneMessageSizeLimit)

	if !fm.notTruncateLargeLog && errors.Is(err, ErrTooLargeDataPack) {
		err = log.Truncate(fm.GetMessageSizeLimit())
	}

	if err != nil {
		return err
	}

	if fm.LogNumber() >= oneMessageLimitLogNumber {
		return ErrTooManyLogs
	}

	if fm.Size()+log.Size() > fm.oneMessageSizeLimit {
		return ErrExceedOneMessageSizeLimit
	}

	if fm.LogNumber() == 0 {
		fm.firstTimeStamp = log.Time()
		fm.lastTimeStamp = log.Time()
	} else {
		// we separate error log here, this is temp.
		if fm.isSeparateErrorLog && (fm.ContainsErrorLog() != log.isError()) {
			return ErrDifferentLevelForErrorLog
		}

		if usToMinute(log.Time()) != usToMinute(fm.firstTimeStamp) {
			return ErrDifferentTimeWindows
		}
		if log.Time() < fm.firstTimeStamp {
			fm.firstTimeStamp = log.Time()
		}
		if log.Time() > fm.lastTimeStamp {
			fm.lastTimeStamp = log.Time()
		}
	}

	if !fm.ContainsErrorLog() && log.isError() {
		fm.markErrorLog()
	}

	fm.logHeaders = log.EncodeHeader(fm.logHeaders)
	fm.logContents = log.EncodeContent(fm.logContents)
	fm.increaseLogNum()
	defer log.Recycle()
	return nil
}

// LogNumber returns the number of logs in the batch.
func (fm *FlatByteLogMessage) LogNumber() int {
	val := atomic.LoadInt32(&fm.logNumber)
	return int(val)
}

// SetCommonHeaders sets the CommonHeaders of the log batch.
// It is usually called for only once.
func (fm *FlatByteLogMessage) SetCommonHeaders(commonHeaders *CommonHeaders) {
	newCommonHeaders := newCommonHeadersBuf(commonHeaders)
	if len(fm.commonHeaders) > LENGTH_BYTES+8 {
		flagOffset, _ := DecodeUint32(fm.commonHeaders[len(fm.commonHeaders)-4:])
		oldFlags, _ := DecodeUint32(fm.commonHeaders[flagOffset:])
		newFlagOffset, _ := DecodeUint32(newCommonHeaders[len(newCommonHeaders)-4:])
		WriteUint32(newCommonHeaders, int(newFlagOffset), oldFlags)
	}

	fm.commonHeaders = newCommonHeaders
}

// SetNewCommonHeaders sets the CommonHeaders of the log batch with new uuid.
// It is usually called for only once.
func (fm *FlatByteLogMessage) SetNewCommonHeaders(commonHeaders *CommonHeaders) {
	newCH := commonHeaders.Copy()
	newCommonHeaders := newCommonHeadersBuf(newCH)
	if len(fm.commonHeaders) > LENGTH_BYTES+8 {
		flagOffset, _ := DecodeUint32(fm.commonHeaders[len(fm.commonHeaders)-4:])
		oldFlags, _ := DecodeUint32(fm.commonHeaders[flagOffset:])
		newFlagOffset, _ := DecodeUint32(newCommonHeaders[len(newCommonHeaders)-4:])
		WriteUint32(newCommonHeaders, int(newFlagOffset), oldFlags)
	}

	fm.commonHeaders = newCommonHeaders
}

// FirstTimeStamp returns the earliest log timestamp in the batch.
func (fm *FlatByteLogMessage) FirstTimeStamp() uint64 {
	return fm.firstTimeStamp
}

// LastTimeStamp returns the last log timestamp in the batch.
func (fm *FlatByteLogMessage) LastTimeStamp() uint64 {
	return fm.lastTimeStamp
}

func (fm *FlatByteLogMessage) SetHeaderCompression(compressType CompressType, extraParameters ...bool) {
	fm.byteLogHeader[BYTELOG_HEADER_COMPRESSION_TYPE_POS] = byte(compressType)
	useGlobalComp := false
	if len(extraParameters) > 0 {
		useGlobalComp = extraParameters[0]
	}
	if useGlobalComp {
		if compressor, err := GetGlobalCompressor(compressType); err == nil {
			fm.headerCompressor = compressor
		}
	} else {
		fm.headerCompressor = NewCompressor(compressType)
	}

	notCompressCommonHeader := false
	if len(extraParameters) > 1 {
		notCompressCommonHeader = extraParameters[1]
	}

	if compressType != None {
		if notCompressCommonHeader {
			fm.byteLogHeader[BYTELOG_HEADER_RESERVED_FLAG_POS] |= 0x02
		} else {
			fm.byteLogHeader[BYTELOG_HEADER_RESERVED_FLAG_POS] &= 0xFD
		}
	}
}

func (fm *FlatByteLogMessage) SetContentCompression(compressType CompressType, useGlobalCompress ...bool) {
	fm.contentCompressInfo[CONTENT_COMPRESSION_TYPE_POS] = byte(compressType)

	useGlobalComp := false
	if len(useGlobalCompress) > 0 {
		useGlobalComp = useGlobalCompress[0]
	}
	if useGlobalComp {
		if compressor, err := GetGlobalCompressor(compressType); err == nil {
			fm.contentCompressor = compressor
		}
	} else {
		fm.contentCompressor = NewCompressor(compressType)
	}
}

// Clear removes all logs in ByteLogMessage. This function will NOT Clear the common headers.
func (fm *FlatByteLogMessage) Clear() {
	WriteUint32(fm.logHeaders, 0, 0)
	fm.logHeaders = fm.logHeaders[:LENGTH_BYTES]

	WriteUint32(fm.logHeaders, 0, 0)

	WriteUint64(fm.contentCompressInfo, CONTENT_COMPRESSED_LEN_POS, 0)
	fm.logContents = fm.logContents[:0]

	WriteUint32(fm.byteLogHeader, BYTELOG_HEADER_TOTAL_LENGTH_POS, 0)
	WriteUint32(fm.byteLogHeader, BYTELOG_HEADER_COMP_HEADER_LENGTH_POS, 0)
	WriteUint32(fm.byteLogHeader, BYTELOG_HEADER_ORIN_HEADER_LENGTH_POS, 0)

	fm.flags = 0
	atomic.StoreInt32(&fm.logNumber, 0)
	fm.firstTimeStamp, fm.lastTimeStamp = 0, 0

	fm.compressedHeaderArea = fm.compressedHeaderArea[:0]
	fm.compressedLogContents = fm.compressedLogContents[:0]
}

// Reset removes all logs in ByteLogMessage. This function will Clear the common headers and buffers.
func (fm *FlatByteLogMessage) Reset() {
	fm.Clear()
	fm.commonHeaders = newCommonHeadersBuf(nil)
}

// EnableDebug will set a bit in ByteLogMessage's reserved field.
func (fm *FlatByteLogMessage) EnableDebug(isEnabled bool) {
	reservedFlag, _ := DecodeUint8(fm.byteLogHeader[BYTELOG_HEADER_RESERVED_FLAG_POS:])
	if isEnabled {
		reservedFlag |= 0x01
	} else {
		reservedFlag &= 0xFE
	}
	WriteUint8(fm.byteLogHeader, BYTELOG_HEADER_RESERVED_FLAG_POS, reservedFlag)
}

// Size returns the size of the message of encoding without compression
func (fm *FlatByteLogMessage) Size() int {
	if fm.LogNumber() == 0 {
		return 0
	}
	totalLength := len(fm.byteLogHeader) + len(fm.contentCompressInfo)
	totalLength += len(fm.logHeaders) + len(fm.pattern) + len(getRealCommonHeaders(fm.commonHeaders))
	totalLength += len(fm.logContents)
	return totalLength
}

// compressedSize returns the size of the batch after compression.
func (fm *FlatByteLogMessage) compressedSize() int {
	if fm.LogNumber() == 0 {
		return 0
	}
	totalLength := len(fm.byteLogHeader) + len(fm.contentCompressInfo)

	if len(fm.compressedHeaderArea) > 4 {
		totalLength += len(fm.compressedHeaderArea)
	} else {
		totalLength += len(fm.logHeaders) + len(fm.pattern) + len(getRealCommonHeaders(fm.commonHeaders))
	}

	if len(fm.compressedLogContents) > 0 {
		totalLength += len(fm.compressedLogContents)
	} else {
		totalLength += len(fm.logContents)
	}
	return totalLength
}

func (fm *FlatByteLogMessage) SizeOfByteLogHeaderArea() int {
	return len(fm.pattern) + len(getRealCommonHeaders(fm.commonHeaders)) +
		len(fm.logHeaders)
}

// ContainsErrorLog indicates whether there is a log of which the level is WARN, ERROR, or FATAL.
func (fm *FlatByteLogMessage) ContainsErrorLog() bool {
	return (fm.flags & 0x01) != 0
}

// SetUUID uses an uuid to compose the batchId in the CommonHeaders.
func (fm *FlatByteLogMessage) SetUUID(newUUID uuid.UUID) {
	newCommonHeaders, _ := setUUID(fm.commonHeaders, newUUID)
	fm.commonHeaders = newCommonHeaders
}

// SetLogStreamId uses a string to compose the batchId in the CommonHeaders.
func (fm *FlatByteLogMessage) SetLogStreamId(logStreamId string) error {
	if len(logStreamId) > SHORT_STRING_MAX_LEN-SEQ_ID_LENGTH {
		return ErrTooLongLogStreamId
	}

	newCommonHeaders, err := setLogStreamId(fm.commonHeaders, logStreamId)
	if err != nil {
		return err
	}

	fm.commonHeaders = newCommonHeaders
	return nil
}

func (fm *FlatByteLogMessage) markErrorLog() {
	fm.flags |= 0x01
}

func (fm *FlatByteLogMessage) increaseLogNum() {
	atomic.AddInt32(&fm.logNumber, 1)
}

// prepareCommonHeaders sets the flag and sequence id.
func (fm *FlatByteLogMessage) prepareCommonHeaders() {
	if len(fm.commonHeaders) > LENGTH_BYTES+8 {
		seqOffset, _ := DecodeUint32(fm.commonHeaders[len(fm.commonHeaders)-8:])
		flagOffset, _ := DecodeUint32(fm.commonHeaders[len(fm.commonHeaders)-4:])
		WriteUint64Hex(fm.commonHeaders, int(seqOffset), fm.seqId)
		WriteUint32(fm.commonHeaders, int(flagOffset), fm.flags)
	}
}

func newSDKHeaderBuf(tenant, logStreamName string) []byte {
	buf := make([]byte, SDK_HEADER_FIXED_LEN+2+len(tenant)+len(logStreamName))
	WriteBytes(buf, LENGTH_BYTES, StringToSliceByte(SDK_MAGIC_NUM_STR), 4) // Magic Number
	WriteUint8(buf, SDK_HEADER_FIXED_LEN, uint8(len(tenant)))
	WriteBytes(buf, SDK_HEADER_FIXED_LEN+1, StringToSliceByte(tenant), len(tenant))
	WriteUint8(buf, SDK_HEADER_FIXED_LEN+1+len(tenant), uint8(len(logStreamName)))
	WriteBytes(buf, SDK_HEADER_FIXED_LEN+2+len(tenant), StringToSliceByte(logStreamName), len(logStreamName))
	return buf
}

func newByteLogHeaderBuf() []byte {
	buf := make([]byte, BYTELOG_HEADER_LEN)
	WriteUint8(buf, 0, BYTELOG_VERSION)
	WriteUint8(buf, 1, BYTELOG_PROTOTYPE)
	WriteUint8(buf, 2, DEFAULT_HEADER_COMPRESSION)
	WriteUint8(buf, 3, BYTELOG_RESERVED_FLAG)
	WriteUint32(buf, BYTELOG_HEADER_CHECKSUM_POS, BYTELOG_CHECKSUM)
	WriteUint32(buf, BYTELOG_HEADER_RESERVED_POS, BYTELOG_RESERVED)
	return buf
}

func getRealCommonHeaders(fs FlatStructure) FlatStructure {
	length, _ := DecodeUint32(fs)
	return fs[:length+LENGTH_BYTES]
}

// newCommonHeadersBuf not only encodes the common headers,
// but it adds 8 extra bytes at the end of the buf.
// 1 for seqIdOffset, 2 for flagOffset
func newCommonHeadersBuf(commonHeaders *CommonHeaders) []byte {
	buf := make([]byte, 0, commonHeaders.Size()+8)
	buf, seqIdOffset, flagOffset := commonHeaders.Encode(buf)
	buf = EncodeUint32(buf, uint32(seqIdOffset))
	buf = EncodeUint32(buf, uint32(flagOffset))
	return buf
}

func newCompressionInfoBuf(contentCompression byte) []byte {
	buf := make([]byte, CONTENT_COMPRESS_INFO_LEN)
	WriteUint8(buf, CONTENT_ORIGIN_LEN_POS+LENGTH_BYTES, contentCompression)
	return buf
}

// update the LogStreamId in the bytes.
func setUUID(fs FlatStructure, newUUID uuid.UUID) (FlatStructure, error) {
	uuidBuf := NewPacket(UUID_LENGTH)
	defer PutPacket(uuidBuf)
	hex.Encode(*uuidBuf, newUUID[:])

	return setLogStreamId(fs, string(*uuidBuf))
}

// update the LogStreamId in the bytes.
func setLogStreamId(fs FlatStructure, logStreamId string) (FlatStructure, error) {
	pos := 0
	commonHeadersLen, err := DecodeUint32(fs[pos:])
	if err != nil {
		return fs, err
	}
	if commonHeadersLen == 0 {
		return fs, ErrNilCommonHeaders
	}
	pos += 4
	batchIDKV, readLength := bytesToShortStrings(fs[pos:], 2)
	if batchIDKV[0] != KEY_BATCH_ID {
		return fs, fmt.Errorf("failed to parse original batchid")
	}
	pos += readLength
	remainingData := fs[pos : commonHeadersLen+LENGTH_BYTES]

	newCommonHeaderBuf := make([]byte, 0, len(fs))
	newCommonHeaderBuf = EncodeUint32(newCommonHeaderBuf, 0)

	packet := NewPacket(0)
	defer PutPacket(packet)

	*packet = append(*packet, logStreamId...)
	*packet = append(*packet, make([]byte, SEQ_ID_LENGTH)...)

	if len(*packet) > SHORT_STRING_MAX_LEN {
		*packet = (*packet)[:SHORT_STRING_MAX_LEN]
	}

	newCommonHeaderBuf = EncodeKeyValue(newCommonHeaderBuf, KEY_BATCH_ID, *packet, StringType)
	seqIdOffset := len(newCommonHeaderBuf) - 1 - SEQ_ID_LENGTH
	flagOffset := len(newCommonHeaderBuf) + EncodedStringSize(KEY_FLAGS) + 1
	newCommonHeaderBuf = append(newCommonHeaderBuf, remainingData...)
	WriteUint32(newCommonHeaderBuf, 0, uint32(len(newCommonHeaderBuf)-LENGTH_BYTES))

	newCommonHeaderBuf = EncodeUint32(newCommonHeaderBuf, uint32(seqIdOffset))
	newCommonHeaderBuf = EncodeUint32(newCommonHeaderBuf, uint32(flagOffset))
	return newCommonHeaderBuf, nil
}
