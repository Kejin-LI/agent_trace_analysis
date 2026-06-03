package codec

import (
	"encoding/hex"
	"sync/atomic"

	"github.com/google/uuid"
)

type FlatStructure []byte

// FlatByteLogBatch is an implementation of LogBatch. It is a replacement of ByteLogBatch.
type FlatByteLogBatch struct {
	sdkHeader  FlatStructure
	logMessage FlatByteLogMessage
}

// NewDefaultFlatByteLogBatch creates a FlatByteLogBatch with <tenant, logstream> = <"argos", "argos">
func NewDefaultFlatByteLogBatch() *FlatByteLogBatch {
	return NewFlatByteLogBatch(DefaultTenant, DefaultLogStreamName)
}

// NewFlatByteLogBatch creates a ByteLogBatch with specified tenant and logstream.
func NewFlatByteLogBatch(tenant, logStreamName string) *FlatByteLogBatch {
	sdkHeader := newSDKHeaderBuf(tenant, logStreamName)
	byteLogHeader := newByteLogHeaderBuf()
	pattern := make([]byte, 4)
	commonHeaders := newCommonHeadersBuf(nil)
	f := newFlatByteLogBatch(sdkHeader, byteLogHeader, pattern, commonHeaders,
		DEFAULT_CONTENT_COMPRESSION, uuid.New())
	return f
}

func newFlatByteLogBatch(sdkHeader, byteLogHeader, pattern, commonHeaders FlatStructure,
	contentCompression byte, uuid uuid.UUID) *FlatByteLogBatch {
	fb := &FlatByteLogBatch{
		sdkHeader:  sdkHeader,
		logMessage: *newFlatByteLogMessage(byteLogHeader, pattern, commonHeaders, contentCompression, uuid),
	}
	return fb
}

// Encode encodes LogBatch and append the content to the buf.
func (f *FlatByteLogBatch) Encode(buf []byte) ([]byte, error) {
	if f.LogNumber() == 0 {
		return buf, ErrEmptyBatch
	}
	buf = f.finalize(buf)
	return buf, nil
}

func (f *FlatByteLogBatch) finalize(buf []byte) []byte {
	f.prep()
	buf = append(buf, f.sdkHeader...)
	buf = f.logMessage.finalize(buf)
	return buf
}

// AppendLog trys to append a data pack to ByteLogMessage. It may fail in cases like:
// 1. The log is invalid, for example_for_agent, it has no timestamp or no file location.
// 2. The log doesn't have a log header.
// 3. The log doesn't have a log content.
// 4. The number of logs exceeds 4096.
// 5. The size of the log batch exceeds 128k.
// 6. The log is not in the same time window (the same minute).
func (f *FlatByteLogBatch) AppendLog(log *DataPack) error {
	err := log.Validate()
	if err != nil {
		return err
	}

	if f.Size()+log.Size() > oneMessageLimitByte {
		return ErrExceedOneMessageSizeLimit
	}

	return f.logMessage.AppendLog(log)
}

// SetCommonHeaders sets the CommonHeaders of the log batch.
// It is usually called for only once.
func (f *FlatByteLogBatch) SetCommonHeaders(commonHeaders *CommonHeaders) {
	f.logMessage.SetCommonHeaders(commonHeaders)
}

func (f *FlatByteLogBatch) SetSDKHeader(sdkHeader *SDKHeader) {
	buf := make([]byte, 0, sdkHeader.Size())
	buf = sdkHeader.Encode(buf)
	f.sdkHeader = buf
}

// SetUserId updates the user id in ByteLogMessage header.
func (f *FlatByteLogBatch) SetUserId(userId uint64) {
	f.logMessage.SetUserId(userId)
}

// LogNumber returns the number of logs in the batch.
func (f *FlatByteLogBatch) LogNumber() int {
	return f.logMessage.LogNumber()
}

// Size returns the size of the byte slice after encoding.
func (f *FlatByteLogBatch) Size() int {
	if f.LogNumber() == 0 {
		return 0
	}
	return len(f.sdkHeader) + f.logMessage.Size()
}

// Clear removes all logs in ByteLogMessage. This function will NOT Clear the common headers.
func (f *FlatByteLogBatch) Clear() {
	f.logMessage.Clear()
	WriteUint16(f.sdkHeader, SDK_HEADER_LOG_COUNT_POS, 0)
	WriteUint64(f.sdkHeader, SDK_HEADER_TIMESTAMP_POS, 0)
	WriteUint32(f.sdkHeader, SDK_HEADER_LENGTH_POS, 0)
}

// Reset removes all logs in ByteLogMessage. This function will Clear the common headers and buffers.
func (f *FlatByteLogBatch) Reset() {
	f.Clear()
	f.logMessage.Reset()
}

// EnableDebug will set a bit in ByteLogMessage's reserved field.
func (f *FlatByteLogBatch) EnableDebug(isEnabled bool) {
	f.logMessage.EnableDebug(isEnabled)
}

// FirstTimeStamp returns the earliest log timestamp in the batch.
func (f *FlatByteLogBatch) FirstTimeStamp() uint64 {
	return f.logMessage.FirstTimeStamp()
}

// LastTimeStamp returns the last log timestamp in the batch.
func (f *FlatByteLogBatch) LastTimeStamp() uint64 {
	return f.logMessage.LastTimeStamp()
}

// prep sets SDKHeaders and LogMessage
func (f *FlatByteLogBatch) prep() {
	f.logMessage.prep()
	WriteUint32(f.sdkHeader, SDK_HEADER_LENGTH_POS, uint32(f.Size()-LENGTH_BYTES))
	WriteUint16(f.sdkHeader, SDK_HEADER_LOG_COUNT_POS, uint16(f.LogNumber()))
	WriteUint64(f.sdkHeader, SDK_HEADER_TIMESTAMP_POS, f.FirstTimeStamp())
}

func (f *FlatByteLogBatch) SizeOfByteLog() int {
	return f.logMessage.Size()
}

// ContainsErrorLog indicates whether there is a log of which the level is WARN, ERROR, or FATAL.
func (f *FlatByteLogBatch) ContainsErrorLog() bool {
	return f.logMessage.ContainsErrorLog()
}

// IncrId increases the id of the log batch. This is function is public since the outer sender may send one batch
// for several times, and it is responsible for increasing the id.
func (f *FlatByteLogBatch) IncrId() {
	f.logMessage.IncrId()
}

// IncrId increases the id of the log batch. This is function is public since the outer sender may send one batch
// for several times, and it is responsible for increasing the id.
func (f *FlatByteLogMessage) IncrId() {
	f.seqId++
}

// FlatByteLogMessage is an implementation of StreamLogBatch. It is a replacement of ByteLogMessage.
type FlatByteLogMessage struct {
	byteLogHeader       FlatStructure
	pattern             FlatStructure
	commonHeaders       FlatStructure
	logHeaders          FlatStructure
	contentCompressInfo FlatStructure
	logContents         FlatStructure

	firstTimeStamp uint64
	lastTimeStamp  uint64
	logNumber      int32
	seqId          uint64
	uuid           uuid.UUID
	flags          uint32
}

func newFlatByteLogMessage(byteLogHeader, pattern, commonHeaders FlatStructure,
	contentCompression byte, uuid uuid.UUID) *FlatByteLogMessage {
	fm := &FlatByteLogMessage{
		byteLogHeader:       byteLogHeader,
		pattern:             pattern,
		commonHeaders:       commonHeaders,
		logHeaders:          make([]byte, 4, 1024),
		contentCompressInfo: newCompressionInfoBuf(contentCompression),
		logContents:         make([]byte, 0, 1024),
		seqId:               0,
		uuid:                uuid,
		flags:               0,
	}
	fm.SetUUID(uuid)
	return fm
}

func NewFlatByteLogMessage() *FlatByteLogMessage {
	byteLogHeader := newByteLogHeaderBuf()
	pattern := make([]byte, 4)
	commonHeaders := newCommonHeadersBuf(nil)
	f := newFlatByteLogMessage(byteLogHeader, pattern, commonHeaders,
		DEFAULT_CONTENT_COMPRESSION, uuid.New())
	return f
}

// Encode encodes LogBatch and append the content to the buf.
func (f *FlatByteLogMessage) Encode(buf []byte) ([]byte, error) {
	if f.LogNumber() == 0 {
		return buf, ErrEmptyBatch
	}
	buf = f.finalize(buf)
	return buf, nil
}

func (f *FlatByteLogMessage) finalize(buf []byte) []byte {
	f.prep()
	buf = append(buf, f.byteLogHeader...)
	buf = append(buf, f.pattern...)
	buf = append(buf, f.commonHeaders...)
	buf = append(buf, f.logHeaders...)
	buf = append(buf, f.contentCompressInfo...)
	buf = append(buf, f.logContents...)
	return buf
}

// prep sets SDKHeaders, ByteLogHeader, LogHeadersArea, contentCompressInfo
func (f *FlatByteLogMessage) prep() {
	WriteUint32(f.contentCompressInfo, CONTENT_COMPRESSED_LEN_POS, uint32(len(f.logContents)))
	WriteUint32(f.contentCompressInfo, CONTENT_ORIGIN_LEN_POS, uint32(len(f.logContents)))

	WriteUint32(f.byteLogHeader, BYTELOG_HEADER_TOTAL_LENGTH_POS, uint32(f.Size()-BYTELOG_HEADER_LEN))
	WriteUint32(f.byteLogHeader, BYTELOG_HEADER_COMP_HEADER_LENGTH_POS, uint32(f.SizeOfByteLogHeaderArea()))
	WriteUint32(f.byteLogHeader, BYTELOG_HEADER_ORIN_HEADER_LENGTH_POS, uint32(f.SizeOfByteLogHeaderArea()))

	WriteUint32(f.logHeaders, 0, uint32(len(f.logHeaders)-LENGTH_BYTES))
	f.prepareCommonHeaders()
}

// AppendLog trys to append a data pack to ByteLogMessage. It may fail in cases like:
// 1. The log is invalid, for example_for_agent, it has no timestamp or no file location.
// 2. The log doesn't have a log header.
// 3. The log doesn't have a log content.
// 4. The number of logs exceeds 4096.
// 5. The size of the log batch exceeds 128k.
// 6. The log is not in the same time window (the same minute).
func (f *FlatByteLogMessage) AppendLog(log *DataPack) error {
	err := log.Validate()
	if err != nil {
		return err
	}

	if f.LogNumber() >= oneMessageLimitLogNumber {
		return ErrTooManyLogs
	}

	if f.Size()+log.Size() > oneMessageLimitByte {
		return ErrExceedOneMessageSizeLimit
	}

	if f.LogNumber() == 0 {
		f.firstTimeStamp = log.Time()
		f.lastTimeStamp = log.Time()
	} else {
		if usToMinute(log.Time()) != usToMinute(f.firstTimeStamp) {
			return ErrDifferentTimeWindows
		}
		if log.Time() < f.firstTimeStamp {
			f.firstTimeStamp = log.Time()
		}
		if log.Time() > f.lastTimeStamp {
			f.lastTimeStamp = log.Time()
		}
	}

	if !f.ContainsErrorLog() && log.isError() {
		f.markErrorLog()
	}

	f.logHeaders = log.EncodeHeader(f.logHeaders)
	f.logContents = log.EncodeContent(f.logContents)
	f.increaseLogNum()
	defer log.Recycle()
	return nil
}

// LogNumber returns the number of logs in the batch.
func (f *FlatByteLogMessage) LogNumber() int {
	val := atomic.LoadInt32(&f.logNumber)
	return int(val)
}

// SetCommonHeaders sets the CommonHeaders of the log batch.
// It is usually called for only once.
func (f *FlatByteLogMessage) SetCommonHeaders(commonHeaders *CommonHeaders) {
	f.commonHeaders = newCommonHeadersBuf(commonHeaders)
	if len(f.commonHeaders) > LENGTH_BYTES {
		hex.Encode(f.commonHeaders[LENGTH_BYTES+EncodedStringSize(KEY_BATCH_ID)+1+1:], f.uuid[:])
	}
}

// FirstTimeStamp returns the earliest log timestamp in the batch.
func (f *FlatByteLogMessage) FirstTimeStamp() uint64 {
	return f.firstTimeStamp
}

// LastTimeStamp returns the last log timestamp in the batch.
func (f *FlatByteLogMessage) LastTimeStamp() uint64 {
	return f.lastTimeStamp
}

// SetUserId updates the user id in ByteLogMessage header.
func (f *FlatByteLogMessage) SetUserId(userId uint64) {
	WriteUint64(f.byteLogHeader, BYTELOG_HEADER_USER_ID_POS, userId)
}

// Clear removes all logs in ByteLogMessage. This function will NOT Clear the common headers.
func (f *FlatByteLogMessage) Clear() {
	WriteUint32(f.logHeaders, 0, 0)
	f.logHeaders = f.logHeaders[:LENGTH_BYTES]
	WriteUint32(f.logHeaders, 0, 0)

	WriteUint64(f.contentCompressInfo, CONTENT_COMPRESSED_LEN_POS, 0)
	f.logContents = f.logContents[:0]

	WriteUint32(f.byteLogHeader, BYTELOG_HEADER_TOTAL_LENGTH_POS, 0)
	WriteUint32(f.byteLogHeader, BYTELOG_HEADER_COMP_HEADER_LENGTH_POS, 0)
	WriteUint32(f.byteLogHeader, BYTELOG_HEADER_ORIN_HEADER_LENGTH_POS, 0)

	f.flags = 0
	atomic.StoreInt32(&f.logNumber, 0)
	f.firstTimeStamp, f.lastTimeStamp = 0, 0
}

// Reset removes all logs in ByteLogMessage. This function will Clear the common headers and buffers.
func (f *FlatByteLogMessage) Reset() {
	f.Clear()
	f.commonHeaders = newCommonHeadersBuf(nil)
}

// EnableDebug will set a bit in ByteLogMessage's reserved field.
func (f *FlatByteLogMessage) EnableDebug(isEnabled bool) {
	reserved := DecodeUint8(f.byteLogHeader[BYTELOG_HEADER_RESERVED_POS:])
	if isEnabled {
		reserved |= 0x01
	} else {
		reserved &= 0xFE
	}
	WriteUint8(f.byteLogHeader, BYTELOG_HEADER_RESERVED_POS, reserved)
}

// Size returns the size of the message of encoding.
func (f *FlatByteLogMessage) Size() int {
	if f.LogNumber() == 0 {
		return 0
	}
	return len(f.byteLogHeader) + len(f.pattern) + len(f.commonHeaders) +
		len(f.logHeaders) + len(f.contentCompressInfo) + len(f.logContents)
}

func (f *FlatByteLogMessage) SizeOfByteLogHeaderArea() int {
	return len(f.pattern) + len(f.commonHeaders) +
		len(f.logHeaders)
}

// ContainsErrorLog indicates whether there is a log of which the level is WARN, ERROR, or FATAL.
func (f *FlatByteLogMessage) ContainsErrorLog() bool {
	return (f.flags & 0x01) != 0
}

func (f *FlatByteLogMessage) SetUUID(newUUID uuid.UUID) {
	f.uuid = newUUID
	if len(f.commonHeaders) > LENGTH_BYTES {
		hex.Encode(f.commonHeaders[LENGTH_BYTES+EncodedStringSize(KEY_BATCH_ID)+1+1:], f.uuid[:])
	}
}

func (f *FlatByteLogMessage) markErrorLog() {
	f.flags |= 0x01
}

func (f *FlatByteLogMessage) increaseLogNum() {
	atomic.AddInt32(&f.logNumber, 1)
}

func (f *FlatByteLogMessage) prepareCommonHeaders() {
	if len(f.commonHeaders) > LENGTH_BYTES {
		WriteUint64Hex(f.commonHeaders, COMMONHEADER_SEQID_POS, f.seqId)
		WriteUint32(f.commonHeaders, COMMONHEADER_FLAG_POS, f.flags)
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
	WriteUint8(buf, 3, BYTELOG_RESERVED)
	WriteUint64(buf, BYTELOG_HEADER_USER_ID_POS, DEFAULT_USER_ID)
	return buf
}

func newCommonHeadersBuf(commonHeaders *CommonHeaders) []byte {
	buf := make([]byte, 0, commonHeaders.GetEncodedSize())
	buf = commonHeaders.Encode(buf)
	return buf
}

func newCompressionInfoBuf(contentCompression byte) []byte {
	buf := make([]byte, CONTENT_COMPRESS_INFO_LEN)
	WriteUint8(buf, CONTENT_ORIGIN_LEN_POS+LENGTH_BYTES, contentCompression)
	return buf
}
