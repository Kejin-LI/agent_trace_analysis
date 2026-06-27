package codec

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"strings"
	"sync"

	"code.byted.org/gopkg/env"

	"github.com/google/uuid"
)

// ByteLogBatch
//+------------+-----------------+
//| SDK header | ByteLog Message |
//+------------+-----------------+
type ByteLogBatch struct {
	header     *SDKHeader
	logMessage *ByteLogMessage

	sdkHeaderBuf []byte
}

// NewDefaultByteLogBatch creates a ByteLogBatch with <tenant, logStream> = <"argos", "argos">
func NewDefaultByteLogBatch() *ByteLogBatch {
	return NewByteLogBatch(DefaultTenant, DefaultLogStreamName)
}

// NewByteLogBatch creates a ByteLogBatch with specified tenant and logStream.
func NewByteLogBatch(tenant, logStream string, ops ...ByteLogOption) *ByteLogBatch {
	return newByteLogBatch(NewSDKHeader(tenant, logStream), NewByteLogMessage(ops...))
}

func newByteLogBatch(header *SDKHeader, logMessage *ByteLogMessage) *ByteLogBatch {
	logBatch := &ByteLogBatch{
		header:       header,
		logMessage:   logMessage,
		sdkHeaderBuf: make([]byte, 0, 64),
	}
	return logBatch
}

func (bb *ByteLogBatch) GetTenant() string {
	return bb.header.GetTenant()
}

func (bb *ByteLogBatch) GetLogStream() string {
	return bb.header.GetLogStream()
}

// Encode encodes the ByteLogBatch and append the bytes to a buf.
// It first encodes the sdk header and then encodes the ByteLogMessage.
func (bb *ByteLogBatch) Encode(buf []byte) ([]byte, error) {
	if bb.LogNumber() == 0 {
		return buf, ErrEmptyBatch
	}
	originalBufEnd := len(buf)
	var err error

	if len(bb.sdkHeaderBuf) == 0 {
		bb.sdkHeaderBuf = bb.header.Encode(bb.sdkHeaderBuf)
	}

	startOfByteLogBatch := len(buf)
	buf = append(buf, bb.sdkHeaderBuf...)
	buf, err = bb.logMessage.Encode(buf)

	if err != nil {
		return buf[:originalBufEnd], err // Restore the buf if err happened.
	}

	endOfByteLogMessage := len(buf)
	length := endOfByteLogMessage - LENGTH_BYTES - startOfByteLogBatch

	// Update the length, timestamp and log count in sdk header.
	WriteUint32(buf, startOfByteLogBatch+SDK_HEADER_LENGTH_POS, uint32(length))
	WriteUint64(buf, startOfByteLogBatch+SDK_HEADER_TIMESTAMP_POS, bb.FirstTimeStamp())
	WriteUint16(buf, startOfByteLogBatch+SDK_HEADER_LOG_COUNT_POS, uint16(bb.LogNumber()))
	return buf, nil
}

// AppendLog trys to append a data pack to ByteLogMessage. It may fail in cases like:
// 1. The log is invalid, for example_for_agent, it has no timestamp or no file location.
// 2. The log doesn't have a log header.
// 3. The log doesn't have a log content.
// 4. The number of logs exceeds 4096.
// 5. The size of the log batch exceeds 128k.
// 6. The log is not in the same time window (the same minute).
func (bb *ByteLogBatch) AppendLog(log *DataPack) error {
	return bb.logMessage.AppendLog(log)
}

// SetCommonHeaders updates the CommonHeaders filed in the internal ByteLogMessage.
// It needs to clear the outdated CommonHeaders' cache.
func (bb *ByteLogBatch) SetCommonHeaders(headers *CommonHeaders) {
	if bb.logMessage == nil {
		bb.logMessage = NewByteLogMessage()
	}
	bb.logMessage.SetCommonHeaders(headers)
}

// SetNewCommonHeaders updates the CommonHeaders filed in the internal ByteLogMessage.
// It needs to clear the outdated CommonHeaders' cache.
func (bb *ByteLogBatch) SetNewCommonHeaders(headers *CommonHeaders) {
	if bb.logMessage == nil {
		bb.logMessage = NewByteLogMessage()
	}
	bb.logMessage.SetNewCommonHeaders(headers)
}

// GetCommonHeaders returns the internal ByteLogMessage's CommonHeaders.
// Please avoid using this function to get the CommonHeaders and modify it.
func (bb *ByteLogBatch) GetCommonHeaders() *CommonHeaders {
	if bb.logMessage == nil {
		return nil
	}
	return bb.logMessage.GetCommonHeaders()
}

// SetSDKHeader updates the SDKHeader of ByteLogBatch.
func (bb *ByteLogBatch) SetSDKHeader(sdkHeader *SDKHeader) {
	bb.header = sdkHeader
	bb.sdkHeaderBuf = bb.sdkHeaderBuf[:0]
}

// SetHeaderCompression sets the header compression option.
func (bb *ByteLogBatch) SetHeaderCompression(compressType CompressType, useGlobalCompress ...bool) {
	bb.logMessage.SetHeaderCompression(compressType, useGlobalCompress...)
}

// SetContentCompression sets the content compression option.
func (bb *ByteLogBatch) SetContentCompression(compressType CompressType, useGlobalCompress ...bool) {
	bb.logMessage.SetContentCompression(compressType, useGlobalCompress...)
}

// Size returns the length of the byte slice after encoding without compression.
func (bb *ByteLogBatch) Size() int {
	if bb.LogNumber() == 0 {
		return 0
	}
	if len(bb.sdkHeaderBuf) == 0 {
		bb.sdkHeaderBuf = bb.header.Encode(bb.sdkHeaderBuf)
	}

	return len(bb.sdkHeaderBuf) + bb.logMessage.Size()
}

// LogNumber returns the number of data packs in the ByteLogMessage.
func (bb *ByteLogBatch) LogNumber() int {
	return bb.logMessage.LogNumber()
}

// Clear clears the sdk header and the ByteLogMessage.
func (bb *ByteLogBatch) Clear() {
	bb.header.clear()
	bb.logMessage.Clear()
}

// Reset clears the sdk header and the ByteLogMessage.
// It also resets the ByteLog header and CommonHeaders in the ByteLogMessage.
func (bb *ByteLogBatch) Reset() {
	bb.Clear()
	bb.sdkHeaderBuf = bb.sdkHeaderBuf[:0]
	bb.logMessage.Reset()
}

// EnableDebug sets the LSB in the reserved field in ByteLogMessage's header.
func (bb *ByteLogBatch) EnableDebug(isEnabled bool) {
	bb.logMessage.EnableDebug(isEnabled)
}

// ContainsErrorLog returns whether the ByteLogMessage contains warn, error, or fatal logs.
func (bb *ByteLogBatch) ContainsErrorLog() bool {
	return bb.logMessage.ContainsErrorLog()
}

func (bb *ByteLogBatch) GetLogMessage() LogMessage {
	return bb.logMessage
}

// IncrId increases the sequenceId.
// This function needs to be called after sending. Note that don't use this func after encoding.
func (bb *ByteLogBatch) IncrId() {
	bb.logMessage.IncrId()
}

// IncrId increases the sequenceId.
// This function needs to be called after sending. Note that don't use this func after encoding.
func (bm *ByteLogMessage) IncrId() {
	bm.seqId++
}

// SetSeqId sets the seqId of the internal byteLogMessage.
// It is dangerous to use this function. Please understand what you are doing.
func (bb *ByteLogBatch) SetSeqId(newSeqId uint64) {
	bb.logMessage.SetSeqId(newSeqId)
}

// SetSeqId sets the seqId of the byteLogMessage.
// It is dangerous to use this function. Please understand what you are doing.
func (bm *ByteLogMessage) SetSeqId(newSeqId uint64) {
	bm.seqId = newSeqId
}

func (bb *ByteLogBatch) GetSeqId() uint64 {
	return bb.logMessage.GetSeqId()
}

func (bm *ByteLogMessage) GetSeqId() uint64 {
	return bm.seqId
}

// SetUUID uses an uuid to compose the batchId in the CommonHeaders.
func (bb *ByteLogBatch) SetUUID(newUUID uuid.UUID) {
	bb.logMessage.SetUUID(newUUID)
}

// SetLogStreamId uses a string to compose the batchId in the CommonHeaders.
func (bb *ByteLogBatch) SetLogStreamId(logStreamId string) error {
	return bb.logMessage.SetLogStreamId(logStreamId)
}

// FirstTimeStamp returns the earliest log timestamp in the batch.
func (bb *ByteLogBatch) FirstTimeStamp() uint64 {
	return bb.logMessage.FirstTimeStamp()
}

// LastTimeStamp returns the last log timestamp in the batch.
func (bb *ByteLogBatch) LastTimeStamp() uint64 {
	return bb.logMessage.LastTimeStamp()
}

func (bb *ByteLogBatch) SetTruncateLargeLogs(isEnabled bool) {
	bb.logMessage.SetTruncateLargeLogs(isEnabled)
}

func (bb *ByteLogBatch) SetMessageSizeLimit(n int) {
	bb.logMessage.SetMessageSizeLimit(n)
}

func (bb *ByteLogBatch) GetMessageSizeLimit() int {
	return bb.logMessage.oneMessageSizeLimit
}

func (bb *ByteLogBatch) SetLargeMessage() {
	bb.logMessage.SetLargeMessage()
}

func (bb *ByteLogBatch) SetChecksum(isEnabled bool) {
	bb.logMessage.SetChecksum(isEnabled)
}

func (bb *ByteLogBatch) SetErrorLogSeparated(isEnabled bool) {
	bb.logMessage.SetErrorLogSeparated(isEnabled)
}

//SDKHeader
//+--------+--------------+-----------+------------+---------+----------+-------------+-----------------+
//|   4B   |      4B      |    8B     |    2B      |   1B    |    1B    |  VarString  |    VarString    |
//+--------+--------------+-----------+------------+---------+----------+-------------+-----------------+
//| length | magic number | timestamp | logs count | version | reserved | tenant name | log stream name |
//+--------+--------------+-----------+------------+---------+----------+-------------+-----------------+
//- length：为后续报文的长度，包括SDK header的剩余部分以及ByteLog message；
//- magic number：暂定"SLOG"？主要为方便用户debug（strace用户进程 + grep关键字）；
//- timestamp：表示本包内所有日志时间戳的最小值（即最早的那条日志），精确到微秒，小端表示；
//- logs count：为ByteLog Message中日志的条数，小端表示；
//- version：表示SDK的版本号，当前填充0；
//- reserved：保留字段，暂置为0；
//- VarString：1字节长度 + string内容（不包含结尾的'\0'）；
type SDKHeader struct {
	length        uint32
	magicNumber   uint32
	version       uint8
	logCount      uint16
	timestamp     uint64
	reserved      uint8
	tenant        VarString
	logStreamName VarString
}

func NewSDKHeader(tenant, logStreamName string) *SDKHeader {
	mNum, _ := StringToUint32(SDK_MAGIC_NUM_STR)
	header := &SDKHeader{
		length:        0,
		magicNumber:   mNum,
		timestamp:     0,
		version:       SDK_VERSION_NUM,
		logCount:      0,
		reserved:      0,
		tenant:        VarString{uint8(len(tenant)), tenant},
		logStreamName: VarString{uint8(len(logStreamName)), logStreamName},
	}
	return header
}

func (h SDKHeader) Size() int {
	return int(SDK_HEADER_FIXED_LEN + 2 + h.tenant.length + h.logStreamName.length)
}

func (h *SDKHeader) Encode(buf []byte) []byte {
	buf = EncodeUint32(buf, h.length)
	buf = EncodeUint32(buf, h.magicNumber)
	buf = EncodeUint64(buf, h.timestamp)
	buf = EncodeUint16(buf, h.logCount)
	buf = EncodeUint8(buf, h.version)
	buf = EncodeUint8(buf, h.reserved)
	buf = encodeVarStr(buf, h.tenant)
	buf = encodeVarStr(buf, h.logStreamName)
	return buf
}

func (h *SDKHeader) GetTenant() string {
	return h.tenant.content
}

func (h *SDKHeader) GetLogStream() string {
	return h.logStreamName.content
}

func (h *SDKHeader) GetTimestamp() uint64 {
	return h.timestamp
}

func (h *SDKHeader) GetLogNumber() int {
	return int(h.logCount)
}

func (h *SDKHeader) clear() {
	h.length = 0
	h.logCount = 0
	h.timestamp = 0
}

// ByteLogMessage is an implementation of LogMessage. It is the core component of ByteLog.
// One ByteLogMessage contains multi data packs (logs).
type ByteLogMessage struct {
	byteLogHeader *ByteLogHeader
	pattern       *ByteLogPattern
	commonHeaders *CommonHeaders
	dataPacks     []*DataPack

	byteLogHeadersBuf []byte
	commonHeadersBuf  []byte

	firstTimeStamp          uint64
	lastTimeStamp           uint64
	currSize                int
	seqId                   uint64
	endOfActualHeaderArea   int
	endOfOriginalHeaderArea int

	headerCompressor  Compressor
	contentCompressor Compressor

	oneMessageSizeLimit int
	// this is temp for separate error log
	isSeparateErrorLog bool

	notTruncateLargeLog bool
}

//ByteLogHeader
//+-------+------------+-------------+----------+--------------+-------------------------+---------------------+---------+----------+
//|  1B   |     1B     |     1B      |   1B     |      4B      |          4B             |          4B         |   4B    |    4B    |
//+-------+------------+-------------+----------+--------------+-------------------------+---------------------+---------+----------+
//|version| prototype  | compression | reserved | total Length |header compressed Length |header origin Length | checksum| reserved |
//+-------+------------+-------------+----------+--------------+-------------------------+---------------------+---------+----------+
type ByteLogHeader struct {
	Version      byte
	ProtoType    byte
	Compression  byte
	ReservedFlag byte
	Checksum     uint32
	Reserved     uint32

	ContentCompression byte
}

func NewByteLogHeader() *ByteLogHeader {
	return newByteLogHeader(BYTELOG_VERSION, BYTELOG_PROTOTYPE, DEFAULT_HEADER_COMPRESSION, DEFAULT_CONTENT_COMPRESSION, BYTELOG_RESERVED_FLAG, BYTELOG_CHECKSUM, BYTELOG_RESERVED)
}

func newByteLogHeader(version, protoType, compression, contentCompression, reservedFlag byte, checksum, reserved uint32) *ByteLogHeader {
	return &ByteLogHeader{
		Version:            version,
		ProtoType:          protoType,
		Compression:        compression,
		ReservedFlag:       reservedFlag,
		Checksum:           checksum,
		Reserved:           reserved,
		ContentCompression: contentCompression,
	}
}

func (h *ByteLogHeader) Encode(buf []byte) []byte {
	buf = EncodeUint8(buf, h.Version)
	buf = EncodeUint8(buf, h.ProtoType)
	buf = EncodeUint8(buf, h.Compression)
	buf = EncodeUint8(buf, h.ReservedFlag)
	buf = EncodeUint32(buf, 0)                // ByteLogMessage Total Length
	buf = EncodeUint32(buf, 0)                // ByteLogMessage Header Area Compressed Length
	buf = EncodeUint32(buf, 0)                // ByteLogMessage Header Area Original Length
	buf = EncodeUint32(buf, BYTELOG_CHECKSUM) // after ByteLogMessage.Encode then write checksum
	buf = EncodeUint32(buf, BYTELOG_RESERVED)
	return buf
}

func (h *ByteLogHeader) CompressCommonHeaders(compress bool) {
	if !compress {
		h.ReservedFlag |= 0x02
	} else {
		h.ReservedFlag &= 0xFD
	}
}

func (h *ByteLogHeader) SetChecksum(isEnabled bool) {
	if isEnabled {
		h.ReservedFlag |= 0x08
	} else {
		h.ReservedFlag &= 0xF7
	}
}

type ByteLogPattern []byte

// NewByteLogMessage creates a default ByteLogMessage. Most bytes are 0 by default.
func NewByteLogMessage(ops ...ByteLogOption) *ByteLogMessage {
	logMessage := &ByteLogMessage{
		byteLogHeader:       NewByteLogHeader(),
		pattern:             nil,
		commonHeaders:       NewDefaultCommonHeaders(),
		dataPacks:           make([]*DataPack, 0, 128),
		byteLogHeadersBuf:   make([]byte, 0, 32),
		commonHeadersBuf:    make([]byte, 0, 128),
		oneMessageSizeLimit: oneMessageLimitByte,
	}

	for _, op := range ops {
		op(logMessage)
	}

	return logMessage
}

// Encode encodes the ByteLogMessage appends the bytes to the buf.
// It first encodes the ByteLog header, then the CommonHeaders, then log headers,  and the log contents in the end.
func (bm *ByteLogMessage) Encode(buf []byte) ([]byte, error) {
	if bm.LogNumber() == 0 {
		return buf, ErrEmptyBatch
	}

	// In log scenario, CommonHeaders is necessary since it contains the batchId and _flags.
	if bm.commonHeaders == nil {
		return buf, ErrNilCommonHeaders
	}

	var err error
	startOfByteLogMessage := len(buf)

	if len(bm.byteLogHeadersBuf) == 0 {
		bm.byteLogHeadersBuf = bm.byteLogHeader.Encode(bm.byteLogHeadersBuf) // cache the byteLogHeader.
	}

	if len(bm.commonHeadersBuf) == 0 {
		bm.commonHeadersBuf, _, _ = bm.commonHeaders.Encode(bm.commonHeadersBuf) // cache the CommonHeaders.
	}

	buf = append(buf, bm.byteLogHeadersBuf...)

	var tempBufForOriginalHeaderArea Packet

	// If the encoder needs to compress the header area (pattern, common headers, and log headers),
	// create a temp buf for original header area.
	if bm.headerCompressor != nil {
		tempBufForOriginalHeaderArea = NewPacket(0)

		if bm.byteLogHeader.ReservedFlag&0x02 != 0 {
			buf = bm.pattern.Encode(buf) // pattern
			startOfCommonHeaders := len(buf)
			buf = append(buf, bm.commonHeadersBuf...)

			// Update the seqId and _flags in the CommonHeaders (int the buf).
			if len(bm.commonHeadersBuf) > LENGTH_BYTES {
				WriteUint64Hex(buf, startOfCommonHeaders+bm.commonHeaders.SeqIdOffset(), bm.seqId)
				WriteUint32(buf, startOfCommonHeaders+bm.commonHeaders.FlagOffset(), bm.commonHeaders._flags)
			}
		} else {
			*tempBufForOriginalHeaderArea = bm.pattern.Encode(*tempBufForOriginalHeaderArea) // pattern

			startOfCommonHeaders := len(*tempBufForOriginalHeaderArea)
			*tempBufForOriginalHeaderArea = append(*tempBufForOriginalHeaderArea, bm.commonHeadersBuf...) // CommonHeaders

			// Update the seqId and _flags in the CommonHeaders (in the temp buf).
			if len(bm.commonHeadersBuf) > LENGTH_BYTES {
				WriteUint64Hex(*tempBufForOriginalHeaderArea, startOfCommonHeaders+bm.commonHeaders.SeqIdOffset(), bm.seqId)
				WriteUint32(*tempBufForOriginalHeaderArea, startOfCommonHeaders+bm.commonHeaders.FlagOffset(), bm.commonHeaders._flags)
			}
		}
	} else {
		// If header is not compressed, directly copy the data to the buf.
		buf = bm.pattern.Encode(buf) // pattern
		startOfCommonHeaders := len(buf)
		buf = append(buf, bm.commonHeadersBuf...)

		// Update the seqId and _flags in the CommonHeaders (int the buf).
		if len(bm.commonHeadersBuf) > LENGTH_BYTES {
			WriteUint64Hex(buf, startOfCommonHeaders+bm.commonHeaders.SeqIdOffset(), bm.seqId)
			WriteUint32(buf, startOfCommonHeaders+bm.commonHeaders.FlagOffset(), bm.commonHeaders._flags)
		}
	}

	// Begin to encode the data packs.
	// Note that if the headers are required to be compressed, we still need to write the data to the temp buf.
	buf, err = bm.encodeDataPacks(buf, bm, tempBufForOriginalHeaderArea, bm.headerCompressor, bm.contentCompressor)

	if err != nil {
		return buf[:startOfByteLogMessage], err
	}

	endOfByteLogMessage := len(buf)

	// Set Length in ByteLog message header
	// Update the Total Length.
	WriteUint32(buf, startOfByteLogMessage+BYTELOG_HEADER_LEN-DISTANCE_BTW_BODY_LEN_POS_AND_BODY,
		uint32(endOfByteLogMessage-BYTELOG_HEADER_LEN-startOfByteLogMessage))

	// Update the Compressed Header Length.
	WriteUint32(buf, startOfByteLogMessage+BYTELOG_HEADER_LEN-DISTANCE_BTW_HEADER_COMP_LEN_POS_AND_HEADER,
		uint32(bm.endOfActualHeaderArea-BYTELOG_HEADER_LEN-startOfByteLogMessage))

	// Update the Original Header Length.
	WriteUint32(buf, startOfByteLogMessage+BYTELOG_HEADER_LEN-DISTANCE_BTW_HEADER_ORIN_LEN_POS_AND_HEADER,
		uint32(bm.endOfOriginalHeaderArea-BYTELOG_HEADER_LEN-startOfByteLogMessage))
	// Set checksum
	if bm.byteLogHeader.ReservedFlag&0x08 != 0 {
		crc := crc32.ChecksumIEEE(buf[startOfByteLogMessage:])
		WriteUint32(buf, startOfByteLogMessage+BYTELOG_HEADER_CHECKSUM_POS, crc)
	}
	return buf, err
}

// AppendLog trys to append a data pack to ByteLogMessage. It may fail in cases like:
// 1. The log is invalid, for example_for_agent, it has no timestamp or no file location.
// 2. The log doesn't have a log header.
// 3. The log doesn't have a log content.
// 4. The number of logs exceeds 4096.
// 5. The size of the log batch exceeds 128k.
// 6. The log is not in the same time window (the same minute).
func (bm *ByteLogMessage) AppendLog(log *DataPack) error {
	err := log.Validate(bm.oneMessageSizeLimit)

	if !bm.notTruncateLargeLog && errors.Is(err, ErrTooLargeDataPack) {
		err = log.Truncate(bm.GetMessageSizeLimit())
	}

	if err != nil {
		return err
	}

	if bm.LogNumber() >= oneMessageLimitLogNumber {
		return ErrTooManyLogs
	}

	if bm.currSize == 0 {
		bm.currSize = bm.Size()
	}
	if bm.currSize+log.Size() > bm.oneMessageSizeLimit {
		return ErrExceedOneMessageSizeLimit
	}

	if bm.LogNumber() == 0 {
		bm.firstTimeStamp = log.Time()
		bm.lastTimeStamp = log.Time()
	} else {
		// this is temp for separate error log
		if bm.isSeparateErrorLog && (bm.ContainsErrorLog() != log.isError()) {
			return ErrDifferentLevelForErrorLog
		}

		if usToMinute(log.Time()) != usToMinute(bm.firstTimeStamp) {
			return ErrDifferentTimeWindows
		}

		if log.Time() < bm.firstTimeStamp {
			bm.firstTimeStamp = log.Time()
		}
		if log.Time() > bm.lastTimeStamp {
			bm.lastTimeStamp = log.Time()
		}
	}
	if !bm.ContainsErrorLog() && log.isError() { // check and write is slightly faster than write everytime.
		bm.markErrorLog()
	}

	bm.appendLog(log)
	bm.currSize += log.Size()
	return nil
}

// LogNumber returns the number of data packs in the ByteLogMessage.
func (bm *ByteLogMessage) LogNumber() int {
	return len(bm.dataPacks)
}

// FirstTimeStamp returns the earliest log's timestamp in the batch.
func (bm *ByteLogMessage) FirstTimeStamp() uint64 {
	return bm.firstTimeStamp
}

// LastTimeStamp returns the latest log's timestamp in the batch.
func (bm *ByteLogMessage) LastTimeStamp() uint64 {
	return bm.lastTimeStamp
}

// Size returns the size of the byte slice of encoding without compression.
func (bm *ByteLogMessage) Size() int {
	if bm.LogNumber() == 0 {
		return 0
	}
	if bm.currSize == 0 {
		bm.currSize = bm.actualSize()
	}

	return bm.currSize
}

// ContainsErrorLog returns whether the ByteLogMessage contains warn, error, or fatal logs.
func (bm *ByteLogMessage) ContainsErrorLog() bool {
	return (bm.commonHeaders._flags & 0x01) != 0
}

// EnableDebug will set a bit in ByteLogMessage's reserved field.
func (bm *ByteLogMessage) EnableDebug(isEnabled bool) {
	reservedFlag := bm.byteLogHeader.ReservedFlag
	if isEnabled {
		reservedFlag |= 0x01
	} else {
		reservedFlag &= 0xFE
	}
	bm.byteLogHeader = newByteLogHeader(
		bm.byteLogHeader.Version,
		bm.byteLogHeader.ProtoType,
		bm.byteLogHeader.Compression,
		bm.byteLogHeader.ContentCompression,
		reservedFlag,
		bm.byteLogHeader.Checksum,
		bm.byteLogHeader.Reserved,
	)
	bm.byteLogHeadersBuf = bm.byteLogHeadersBuf[:0]
}

func (bm *ByteLogMessage) Clear() {
	bm.commonHeaders._flags = 0
	bm.firstTimeStamp = 0
	bm.lastTimeStamp = 0
	bm.currSize = 0
	for _, dp := range bm.dataPacks {
		dp.Recycle()
	}
	bm.dataPacks = bm.dataPacks[:0]
}

func (bm *ByteLogMessage) Reset() {
	bm.Clear()
	bm.commonHeaders = nil
	bm.byteLogHeadersBuf = bm.byteLogHeadersBuf[:0]
	bm.commonHeadersBuf = bm.commonHeadersBuf[:0]
}

func (bm *ByteLogMessage) actualSize() int {
	headerAreaSize := BYTELOG_HEADER_LEN + bm.pattern.Size() + bm.commonHeaders.Size()
	dataPackSize := LENGTH_BYTES + 9 // Log Header Length + Content Compressed Length + Original Length + Compression
	for i := range bm.dataPacks {
		dataPackSize += bm.dataPacks[i].Size()
	}

	return headerAreaSize + dataPackSize
}

func (bm *ByteLogMessage) appendLog(log *DataPack) {
	bm.dataPacks = append(bm.dataPacks, log)
}

func (bm *ByteLogMessage) markErrorLog() {
	bm.commonHeaders._flags |= 0x01
}

func (bm *ByteLogMessage) encodeDataPacks(buf []byte, logMessage *ByteLogMessage, tempBufForOriginalHeaderArea Packet, headerCompressor Compressor, contentCompressor Compressor) ([]byte, error) {
	var err error
	posOfLengthOriginalLogHeaders := len(buf) // The position of the length field (uint32) of the log headers.

	if headerCompressor != nil {
		// If log headers need to be compressed, the tempBufForOriginalHeaderArea contains the pattern and commonHeaders now.
		// We need to
		// 1. record the original length of log headers,
		// 2. encode the original data of log headers
		// 3. compress the whole header bytes and append them to the buf.
		defer PutPacket(tempBufForOriginalHeaderArea) // we can recycle the packet after compression.

		posOfLengthOriginalLogHeaders = len(*tempBufForOriginalHeaderArea)             // update the length position to the position in the temp buf.
		*tempBufForOriginalHeaderArea = EncodeUint32(*tempBufForOriginalHeaderArea, 0) // occupy an uint32 for the length

		for i := range logMessage.dataPacks {
			*tempBufForOriginalHeaderArea = logMessage.dataPacks[i].EncodeHeader(*tempBufForOriginalHeaderArea)
		}
		WriteUint32(*tempBufForOriginalHeaderArea, posOfLengthOriginalLogHeaders, uint32(len(*tempBufForOriginalHeaderArea)-posOfLengthOriginalLogHeaders-LENGTH_BYTES))

		// Store the end of the original header area.
		// At this point, the buf should only contain sdk header and byteLog header.
		// So the original end should be len(tempBuf) + len(buf)
		bm.endOfOriginalHeaderArea = len(*tempBufForOriginalHeaderArea) + len(buf)
		buf, err = headerCompressor.Compress(buf, *tempBufForOriginalHeaderArea)
		if err != nil {
			return buf, err
		}
	} else {
		buf = EncodeUint32(buf, 0) // occupy an uint32 for the length of log headers.

		startOfLogHeaders := len(buf)
		for i := range logMessage.dataPacks {
			buf = logMessage.dataPacks[i].EncodeHeader(buf)
		}
		bm.endOfOriginalHeaderArea = len(buf)
		WriteUint32(buf, posOfLengthOriginalLogHeaders, uint32(len(buf)-startOfLogHeaders))
	}

	// Store the end of the header area. We need it to update the lengths in ByteLogHeaders
	bm.endOfActualHeaderArea = len(buf)

	posOfLogContentLength := len(buf)
	buf = encodeContentCompressInfo(buf, logMessage.byteLogHeader.ContentCompression)

	startOfLogContentArea := len(buf)
	var endOfOriginalContentArea int

	// Similar logic to the header area.
	if contentCompressor != nil {
		tempBufForOriginalContent := NewPacket(0)
		defer PutPacket(tempBufForOriginalContent)

		for i := range logMessage.dataPacks {
			*tempBufForOriginalContent = logMessage.dataPacks[i].EncodeContent(*tempBufForOriginalContent)
		}
		endOfOriginalContentArea = len(*tempBufForOriginalContent) + startOfLogContentArea
		buf, err = contentCompressor.Compress(buf, *tempBufForOriginalContent)
		if err != nil {
			return buf, err
		}
	} else {
		for i := range logMessage.dataPacks {
			buf = logMessage.dataPacks[i].EncodeContent(buf)
		}
		endOfOriginalContentArea = len(buf)
	}

	endOfLogContentArea := len(buf)
	WriteUint32(buf, posOfLogContentLength, uint32(endOfLogContentArea-startOfLogContentArea))                   // actual content length.
	WriteUint32(buf, posOfLogContentLength+LENGTH_BYTES, uint32(endOfOriginalContentArea-startOfLogContentArea)) // original content length.
	return buf, nil
}

// SetCommonHeaders sets the CommonHeaders of the ByteLogMessage. It also copies the uuid.
func (bm *ByteLogMessage) SetCommonHeaders(headers *CommonHeaders) {
	bm.commonHeadersBuf = bm.commonHeadersBuf[:0]
	if bm.commonHeaders != nil {
		headers.SetFlags(bm.commonHeaders.GetFlags())
	}
	bm.commonHeaders = headers
	bm.commonHeadersBuf, _, _ = headers.Encode(bm.commonHeadersBuf)
	bm.currSize = bm.actualSize()
}

// SetNewCommonHeaders sets the CommonHeaders of the ByteLogMessage.
func (bm *ByteLogMessage) SetNewCommonHeaders(headers *CommonHeaders) {
	bm.commonHeadersBuf = bm.commonHeadersBuf[:0]
	if bm.commonHeaders != nil {
		headers.SetFlags(bm.commonHeaders.GetFlags())
	}
	bm.commonHeaders = headers.Copy()
	bm.commonHeadersBuf, _, _ = headers.Encode(bm.commonHeadersBuf)
	bm.currSize = bm.actualSize()
}

// SetUUID uses an uuid to compose the batchId in the CommonHeaders.
func (bm *ByteLogMessage) SetUUID(newUUID uuid.UUID) {
	bm.commonHeadersBuf = bm.commonHeadersBuf[:0]
	bm.commonHeaders.SetUUID(newUUID)
	bm.currSize = bm.actualSize()
}

// SetLogStreamId uses a string to compose the batchId in the CommonHeaders.
func (bm *ByteLogMessage) SetLogStreamId(logStreamId string) error {
	if len(logStreamId) > SHORT_STRING_MAX_LEN-SEQ_ID_LENGTH {
		return ErrTooLongLogStreamId
	}
	bm.commonHeadersBuf = bm.commonHeadersBuf[:0]
	bm.commonHeaders.SetLogStreamId(logStreamId)
	bm.currSize = bm.actualSize()
	return nil
}

// SetHeaderCompression sets the header compression option.
func (bm *ByteLogMessage) SetHeaderCompression(compressType CompressType, extraParameters ...bool) {
	bm.byteLogHeader.Compression = byte(compressType)
	bm.byteLogHeadersBuf = bm.byteLogHeadersBuf[:0]

	useGlobalComp := false
	if len(extraParameters) > 0 {
		useGlobalComp = extraParameters[0]
	}
	if useGlobalComp {
		if compressor, err := GetGlobalCompressor(compressType); err == nil {
			bm.headerCompressor = compressor
		}
	} else {
		bm.headerCompressor = NewCompressor(compressType)
	}

	notCompressCommonHeader := false
	if len(extraParameters) > 1 {
		notCompressCommonHeader = extraParameters[1]
	}
	if compressType != None {
		bm.byteLogHeader.CompressCommonHeaders(!notCompressCommonHeader)
	}
}

// SetContentCompression sets the content compression option.
func (bm *ByteLogMessage) SetContentCompression(compressType CompressType, useGlobalCompress ...bool) {
	bm.byteLogHeader.ContentCompression = byte(compressType)
	bm.byteLogHeadersBuf = bm.byteLogHeadersBuf[:0]
	useGlobalComp := false
	if len(useGlobalCompress) > 0 {
		useGlobalComp = useGlobalCompress[0]
	}
	if useGlobalComp {
		if compressor, err := GetGlobalCompressor(compressType); err == nil {
			bm.contentCompressor = compressor
		}
	} else {
		bm.contentCompressor = NewCompressor(compressType)
	}

}

// GetCommonHeaders returns the internal ByteLogMessage's CommonHeaders.
// It is dangerous to use this function. Please avoid using it to update the CommonHeaders.
func (bm *ByteLogMessage) GetCommonHeaders() *CommonHeaders {
	return bm.commonHeaders
}

// GetDataPacks return the internal data packs in the ByteLogMessage.
// It is dangerous to use this function. Please avoid using it.
func (bm *ByteLogMessage) GetDataPacks() []*DataPack {
	return bm.dataPacks
}

func (bm *ByteLogMessage) SetTruncateLargeLogs(isEnabled bool) {
	bm.notTruncateLargeLog = !isEnabled
}

func (bm *ByteLogMessage) GetMessageSizeLimit() int {
	return bm.oneMessageSizeLimit
}

func (bm *ByteLogMessage) SetLargeMessage() {
	bm.oneMessageSizeLimit = largeMessageLimitByte
}

func (bm *ByteLogMessage) SetMessageSizeLimit(n int) {
	if n <= 1000 {
		return
	}
	bm.oneMessageSizeLimit = n
}

func (bm *ByteLogMessage) SetChecksum(isEnabled bool) {
	bm.byteLogHeader.SetChecksum(isEnabled)
	bm.byteLogHeadersBuf = bm.byteLogHeadersBuf[:0]
}

func (bm *ByteLogMessage) SetErrorLogSeparated(isEnabled bool) {
	bm.isSeparateErrorLog = isEnabled
}

func (p *ByteLogPattern) Encode(buf []byte) []byte {
	if p == nil {
		buf = EncodeUint32(buf, 0)
		return buf
	}
	// TODO: support pattern in the future.
	return EncodeUint32(buf, 0)
}

func (p *ByteLogPattern) Size() int {
	return 4
}

type LogHeader struct {
	// reserved fields
	Timestamp uint64
	Source    string
	Context   string
	LogId     string

	// customized fields
	Level    string
	Location string
	SpanId   uint64

	ExtraKVs []*KeyValue
}

func (lh *LogHeader) Size() int {
	if lh == nil {
		return 0
	}
	totalLength := 4 // Length of LogHeader
	totalLength += 9 // Timestamp (type + uint64)
	totalLength += EncodedStringSize(lh.Source)
	totalLength += EncodedStringSize(lh.Context)
	totalLength += EncodedStringSize(lh.LogId)
	totalLength += EncodedKVSizeStr(KEY_LEVEL, lh.Level)
	totalLength += EncodedKVSizeStr(KEY_LOCATION, lh.Location)
	totalLength += EncodedStringSize(KEY_SPANID)
	totalLength += 9 // span_id (type + uint64)

	// TODO: check whether long string is allowed here.
	for _, kv := range lh.ExtraKVs {
		totalLength += kv.Size()
	}
	return totalLength
}

func (lh *LogHeader) Encode(buf []byte) []byte {
	if lh == nil {
		return buf
	}
	posOfLengthOfLogHeader := len(buf)
	buf = EncodeUint32(buf, 0) // length
	startOfLogHeader := len(buf)
	buf = append(buf, Uint64Type)
	buf = EncodeUint64(buf, lh.Timestamp)
	buf = encodeShortStr(buf, lh.Source)
	buf = encodeShortStr(buf, lh.Context)
	buf = encodeShortStr(buf, lh.LogId)
	buf = EncodeKeyValue(buf, KEY_LEVEL, StringToSliceByte(lh.Level), StringType)
	buf = EncodeKeyValue(buf, KEY_LOCATION, StringToSliceByte(lh.Location), StringType)
	buf = EncodeKeyValueUint64(buf, KEY_SPANID, lh.SpanId)

	for _, kv := range lh.ExtraKVs {
		buf = kv.Encode(buf)
	}

	WriteUint32(buf, posOfLengthOfLogHeader, uint32(len(buf)-startOfLogHeader))
	return buf
}

// AddExtraKV creates a short Key-Value pair and store this kv.
// The key and value may be trimmed.
func (lh *LogHeader) AddExtraKV(key, value interface{}) {
	if lh == nil {
		return
	}
	kv, err := NewKeyValue(key, value) // This is a short KV.
	if err != nil {
		return
	}

	lh.ExtraKVs = append(lh.ExtraKVs, kv)
}

// AddExtraKVs creates several Key-Value pairs. The keys and values may be trimmed.
func (lh *LogHeader) AddExtraKVs(kvlist ...interface{}) {
	if lh == nil {
		return
	}
	if len(kvlist) == 0 || (len(kvlist)&1 == 1) { // ignore odd kvlist
		return
	}

	for i := 0; i+1 < len(kvlist); i += 2 {
		lh.AddExtraKV(kvlist[i], kvlist[i+1])
	}
}

// AddExtraKeyValue directly add the KeyValue to the slice. It does NOT check whether the KV is long.
// TODO: check whether this is allowed.
// Ideally, ByteLog should not allows long kvs to be in the log headers. But ByteLog Store actually supports it.
func (lh *LogHeader) AddExtraKeyValue(kv *KeyValue) error {
	if lh == nil {
		return ErrNilObject
	}

	lh.ExtraKVs = append(lh.ExtraKVs, kv)
	return nil
}

func (lh *LogHeader) ResetExtraKVs() {
	lh.ExtraKVs = lh.ExtraKVs[:0]
}

type LogContent struct {
	Msg      string
	ExtraKVs []*KeyValue
}

func (lc *LogContent) Encode(buf []byte) []byte {
	if lc == nil {
		return buf
	}
	buf = EncodeUint32(buf, 0)
	start := len(buf)
	buf = EncodeKeyValueText(buf, KEY_MSG, lc.Msg)
	for _, kv := range lc.ExtraKVs {
		buf = kv.Encode(buf)
	}
	end := len(buf)
	WriteUint32(buf, start-4, uint32(end-start))
	return buf
}

func (lc *LogContent) Size() int {
	if lc == nil {
		return 4
	}
	totalLength := 4
	totalLength += EncodedKVSizeText(KEY_MSG, lc.Msg)

	for _, kv := range lc.ExtraKVs {
		totalLength += kv.Size()
	}
	return totalLength
}

// AddExtraKV creates a long Key-Value pair and store this kv in content area.
func (lc *LogContent) AddExtraKV(key, value interface{}) {
	if lc == nil {
		return
	}
	kv, err := NewKeyValue(key, value, true)
	if err != nil {
		return
	}

	lc.ExtraKVs = append(lc.ExtraKVs, kv)
}

// AddExtraKVs adds several Key-Value pairs.
func (lc *LogContent) AddExtraKVs(kvlist ...interface{}) {
	if lc == nil {
		return
	}
	if len(kvlist) == 0 || (len(kvlist)&1 == 1) { // ignore odd kvlist
		return
	}

	for i := 0; i+1 < len(kvlist); i += 2 {
		lc.AddExtraKV(kvlist[i], kvlist[i+1])
	}
}

// AddExtraKeyValue directly add the KeyValue to the slice. It does NOT check whether the KV is long.
func (lc *LogContent) AddExtraKeyValue(kv *KeyValue) error {
	if lc == nil {
		return ErrNilObject
	}

	lc.ExtraKVs = append(lc.ExtraKVs, kv)
	return nil
}

func (lc *LogContent) ResetExtraKVs() {
	lc.ExtraKVs = lc.ExtraKVs[:0]
}

// CommonHeaders contains 9 kv pairs.
// We need to encode both the keys and values.
// | key type | length* | key content| value type | length* | value content |
type CommonHeaders struct {
	_logStreamId string
	_flags       uint32
	_psm         string
	_idc         string
	_stage       string
	_cluster     string
	_podname     string
	_taskname    string
	_language    string
	_ipv4        Ipv4
	_ipv6        Ipv6
	extraKVs     []*KeyValue
}

func NewDefaultCommonHeaders() *CommonHeaders {
	ipv4Uint32, ipv6ByteArray := Ipv4(0), Ipv6{}
	ipv4, ipv6 := env.IPV4(), env.IPV6()

	if len(ipv4) == 4 {
		ipv4Uint32 = Ipv4(binary.LittleEndian.Uint32(ipv4))
	} else if len(ipv4) == 16 {
		ipv4Uint32 = Ipv4(binary.LittleEndian.Uint32(ipv4[12:16]))
	}
	if len(ipv6) == 16 {
		copy(ipv6ByteArray[:], ipv6)
	}
	return NewCommonHeaders(
		env.PSM(),
		env.IDC(),
		env.Stage(),
		env.Cluster(),
		env.PodName(),
		env.PSM(),
		"Go",
		ipv4Uint32,
		ipv6ByteArray,
	)
}

// NewCommonHeaders created the CommonHeaders
// This function is called in log sdk.
func NewCommonHeaders(psm, idc, stage, cluster, podname, taskname, language string, ipv4 Ipv4, ipv6 Ipv6) *CommonHeaders {
	c := &CommonHeaders{
		_logStreamId: "",
		_flags:       0,
		_psm:         psm,
		_idc:         idc,
		_stage:       stage,
		_cluster:     cluster,
		_podname:     podname,
		_taskname:    taskname,
		_language:    language,
		_ipv4:        ipv4,
		_ipv6:        ipv6,
		extraKVs:     nil,
	}
	c.SetUUID(uuid.New())
	return c
}

func NewEmptyCommonHeaders() *CommonHeaders {
	c := &CommonHeaders{}
	c.SetUUID(uuid.New())
	return c
}

func (h *CommonHeaders) GetLogStreamId() string {
	return h._logStreamId
}

// SetUUID updates the _logStreamId in the CommonHeaders.
// In most cases you should not call this function.
// It does not clear the CommonHeaders buf in ByteLog Message.
// You should understand what you are doing.
func (h *CommonHeaders) SetUUID(newUUID uuid.UUID) {
	uuidBuf := NewPacket(UUID_LENGTH)
	defer PutPacket(uuidBuf)
	hex.Encode(*uuidBuf, newUUID[:])
	h.SetLogStreamId(string(*uuidBuf))
}

// SetLogStreamId updates the _logStreamId in the CommonHeaders.
// In most cases you should not call this function.
// It does not clear the commonheader buf in ByteLog Message.
// You should understand what you are doing.
func (h *CommonHeaders) SetLogStreamId(logStreamId string) {
	if len(logStreamId) > SHORT_STRING_MAX_LEN-SEQ_ID_LENGTH {
		logStreamId = logStreamId[:SHORT_STRING_MAX_LEN-SEQ_ID_LENGTH]
	}
	h._logStreamId = logStreamId
}

func (h *CommonHeaders) GetFlags() uint32 {
	return h._flags
}

func (h *CommonHeaders) SetFlags(flags uint32) {
	h._flags = flags
}

func (h *CommonHeaders) GetPSM() string {
	return h._psm
}

func (h *CommonHeaders) GetIDC() string {
	return h._idc
}

func (h *CommonHeaders) GetStage() string {
	return h._stage
}

func (h *CommonHeaders) GetCluster() string {
	return h._cluster
}

func (h *CommonHeaders) GetPodname() string {
	return h._podname
}

func (h *CommonHeaders) GetTaskname() string {
	return h._taskname
}

func (h *CommonHeaders) GetLanguage() string {
	return h._language
}

func (h *CommonHeaders) GetIpV4() Ipv4 {
	return h._ipv4
}

func (h *CommonHeaders) GetIpV6() Ipv6 {
	return h._ipv6
}

func (h *CommonHeaders) SetPSM(psm string) {
	if h == nil {
		return
	}
	h._psm = psm
}

func (h *CommonHeaders) SetIDC(idc string) {
	h._idc = idc
}

func (h *CommonHeaders) SetStage(stage string) {
	h._stage = stage
}

func (h *CommonHeaders) SetCluster(cluster string) {
	h._cluster = cluster
}

func (h *CommonHeaders) SetPodname(podname string) {
	h._podname = podname
}

func (h *CommonHeaders) SetTaskname(taskname string) {
	h._taskname = taskname
}
func (h *CommonHeaders) SetLanguage(lang string) {
	h._language = lang
}

func (h *CommonHeaders) SetIpV4(ipv4 Ipv4) {
	h._ipv4 = ipv4
}

func (h *CommonHeaders) SetIpV6(ipv6 Ipv6) {
	h._ipv6 = ipv6
}

func (h *CommonHeaders) AddExtraKV(key, value interface{}) {
	if h == nil {
		return
	}
	kv, err := NewKeyValue(key, value)
	if err != nil {
		return
	}
	h.extraKVs = append(h.extraKVs, kv)
}

func (h *CommonHeaders) AddExtraKVs(kvlist ...interface{}) {
	if h == nil {
		return
	}
	if len(kvlist) == 0 || (len(kvlist)&1 == 1) { // ignore odd kvlist
		return
	}

	for i := 0; i+1 < len(kvlist); i += 2 {
		h.AddExtraKV(kvlist[i], kvlist[i+1])
	}
}

func (h *CommonHeaders) ResetExtraKVs() {
	h.extraKVs = h.extraKVs[:0]
}

func (h *CommonHeaders) Encode(buf []byte) ([]byte, int, int) {
	if h == nil {
		buf = EncodeUint32(buf, 0)
		return buf, 0, 0
	}

	var seqIdOffset, flagOffset = 0, 0

	pos := len(buf)
	buf = EncodeUint32(buf, 0) // CommonHeaders' length
	start := pos + LENGTH_BYTES

	{
		packet := NewPacket(0)
		defer PutPacket(packet)
		*packet = append(*packet, h._logStreamId...)
		*packet = append(*packet, make([]byte, SEQ_ID_LENGTH)...)
		buf = EncodeKeyValue(buf, KEY_BATCH_ID, *packet, StringType)
		seqIdOffset = len(buf) - SEQ_ID_LENGTH - 1
	}

	buf = EncodeKeyValueUint32(buf, KEY_FLAGS, h._flags)
	flagOffset = len(buf) - 4

	buf = EncodeKeyValueStr(buf, KEY_PSM, h._psm)
	buf = EncodeKeyValueStr(buf, KEY_IDC, h._idc)
	buf = EncodeKeyValueStr(buf, KEY_STAGE, h._stage)
	buf = EncodeKeyValueStr(buf, KEY_CLUSTER, h._cluster)
	buf = EncodeKeyValueStr(buf, KEY_PODNAME, h._podname)
	buf = EncodeKeyValueStr(buf, KEY_TASKNAME, h._taskname)
	buf = EncodeKeyValueStr(buf, KEY_LANGUAGE, h._language)

	ipv4bytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(ipv4bytes, uint32(h._ipv4))
	buf = EncodeKeyValue(buf, KEY_IPV4, ipv4bytes, Ipv4Type)
	buf = EncodeKeyValue(buf, KEY_IPV6, h._ipv6[:], Ipv6Type)

	if len(h.extraKVs) > 0 {
		for _, kv := range h.extraKVs {
			buf = kv.Encode(buf)
		}
	}

	length := len(buf) - start
	WriteUint32(buf, pos, uint32(length))
	return buf, seqIdOffset, flagOffset
}

func (h *CommonHeaders) Size() int {
	if h == nil {
		return 4
	}
	total := 4
	total += EncodedKVIBatchIDSize(KEY_BATCH_ID, h._logStreamId)
	total += EncodedKVFlagSize(KEY_FLAGS)
	total += EncodedKVSizeStr(KEY_PSM, h._psm)
	total += EncodedKVSizeStr(KEY_IDC, h._idc)
	total += EncodedKVSizeStr(KEY_STAGE, h._stage)
	total += EncodedKVSizeStr(KEY_CLUSTER, h._cluster)
	total += EncodedKVSizeStr(KEY_PODNAME, h._podname)
	total += EncodedKVSizeStr(KEY_TASKNAME, h._taskname)
	total += EncodedKVSizeStr(KEY_LANGUAGE, h._language)
	total += EncodedKVIPv4Size(KEY_IPV4)
	total += EncodedKVIPv6Size(KEY_IPV6)
	if len(h.extraKVs) > 0 {
		for _, kv := range h.extraKVs {
			total += kv.Size()
		}
	}
	return total
}

func (h *CommonHeaders) SeqIdOffset() int {
	// 4 + 1 + 1 + 8 (key_batchid) + 1 + 1 + 1 + len(_logStreamId)
	return 17 + len(h._logStreamId)
}

func (h *CommonHeaders) FlagOffset() int {
	return 4 + (len(h._logStreamId) - 32) + 62 + 1 + 1 + 6 + 1 + 1
}

func (h *CommonHeaders) Copy() *CommonHeaders {
	c := h.Clone()
	c.SetUUID(uuid.New())
	return c
}

func (h *CommonHeaders) Clone() *CommonHeaders {
	c := &CommonHeaders{
		_logStreamId: h._logStreamId,
		_flags:       h._flags,
		_psm:         h._psm,
		_idc:         h._idc,
		_stage:       h._stage,
		_cluster:     h._cluster,
		_podname:     h._podname,
		_taskname:    h._taskname,
		_language:    h._language,
		_ipv4:        h._ipv4,
		_ipv6:        h._ipv6,
	}
	for _, kv := range h.extraKVs {
		c.extraKVs = append(c.extraKVs, kv.Clone())
	}
	return c
}

// ContentCompressInfo
//+-------------------+-----------------+------------+
//| compressed Length | original Length |compressType+
//+-------4b----------+--------4b-------+------1b----+
func encodeContentCompressInfo(buf []byte, compress byte) []byte {
	buf = EncodeUint32(buf, 0)
	buf = EncodeUint32(buf, 0)
	buf = EncodeUint8(buf, compress)
	return buf
}

// DataPack represents a logs' unique properties.
// Never append one data pack into two batches. Use Clone to create a copy.
type DataPack struct {
	LogHeader  *LogHeader
	LogContent *LogContent
}

func NewEmptyDataPack() *DataPack {
	return dataPackPool.Get().(*DataPack)
}

func NewDataPack(msg string, ts uint64, source, context, logId, level, location string, spanId uint64) *DataPack {
	dp := dataPackPool.Get().(*DataPack)
	dp.LogHeader.Timestamp = ts
	dp.LogHeader.Source = source
	dp.LogHeader.Context = context
	dp.LogHeader.LogId = logId

	// customized fields
	dp.LogHeader.Level = level
	dp.LogHeader.Location = location
	dp.LogHeader.SpanId = spanId

	dp.LogContent.Msg = msg
	return dp
}

func (dp *DataPack) Clone() *DataPack {
	v := NewEmptyDataPack()

	if dp.LogHeader != nil {
		v.LogHeader = &LogHeader{}
		v.LogHeader.Timestamp = dp.LogHeader.Timestamp
		v.LogHeader.Source = dp.LogHeader.Source
		v.LogHeader.Context = dp.LogHeader.Context
		v.LogHeader.LogId = dp.LogHeader.LogId
		v.LogHeader.Level = dp.LogHeader.Level
		v.LogHeader.Location = dp.LogHeader.Location
		v.LogHeader.SpanId = dp.LogHeader.SpanId
		v.LogHeader.ExtraKVs = make([]*KeyValue, len(dp.LogHeader.ExtraKVs))
		for i, kv := range dp.LogHeader.ExtraKVs {
			v.LogHeader.ExtraKVs[i] = kv.Clone()
		}
	} else {
		v.LogHeader = nil
	}

	if dp.LogContent != nil {
		v.LogContent.Msg = dp.LogContent.Msg
		v.LogContent.ExtraKVs = make([]*KeyValue, len(dp.LogContent.ExtraKVs))
		for i, kv := range dp.LogContent.ExtraKVs {
			v.LogContent.ExtraKVs[i] = kv.Clone()
		}
	} else {
		v.LogContent = nil
	}
	return v
}

func (dp *DataPack) Time() uint64 {
	return dp.LogHeader.Timestamp
}

// Size returns the number of bytes before compression.
func (dp *DataPack) Size() int {
	return dp.LogHeader.Size() + dp.LogContent.Size()
}

// Validate checks whether a log(data pack) is valid.
// If only valid logs can be appended to LogBatch
func (dp *DataPack) Validate(sizeLimit int) error {
	if dp == nil {
		return ErrNilDataPack
	}

	if dp.LogHeader == nil {
		return ErrNilLogHeaders
	}

	if dp.LogHeader.Timestamp == 0 || len(dp.LogHeader.Location) == 0 {
		return ErrInvalidLogHeader
	}

	if dp.LogContent == nil {
		return ErrNilLogContent
	}

	if dp.Size() > sizeLimit {
		return ErrTooLargeDataPack
	}

	return nil
}

var truncatedReason = " [ truncated by ttlogagent_gosdk"

const truncateReasonLength = 2048

func (dp *DataPack) Truncate(sizeLimit int) error {
	if dp.Size() < sizeLimit {
		return nil
	}

	if sizeLimit <= truncateReasonLength {
		return fmt.Errorf("invalid size limit: %d", sizeLimit)
	}
	var err error
	if len(dp.LogContent.Msg) >= sizeLimit && len(dp.LogContent.Msg) >= truncateReasonLength && sizeLimit >= truncateReasonLength {
		dp.LogContent.Msg = dp.LogContent.Msg[:sizeLimit-truncateReasonLength] + truncatedReason
	}

	newSize := dp.Size()
	if newSize > sizeLimit {
		remainingLengthBudget := sizeLimit - dp.LogHeader.Size() - EncodedKVSizeText(KEY_MSG, dp.LogContent.Msg) - 4 // 4 is header length
		err = dp.truncateLongKVs(remainingLengthBudget)
		if err != nil {
			return err
		}
	}
	newSize = dp.Size()

	if newSize > sizeLimit {
		return ErrFailToTruncateDataPack
	}
	return nil
}

// truncateLongKVs truncates kvs in log content. It only truncates text key-values.
func (dp *DataPack) truncateLongKVs(sizeLimit int) error {
	var err error

	if sizeLimit <= 0 {
		return fmt.Errorf("size limit: %d, err: %w", sizeLimit, ErrNegativeSizeLimit)
	}

	longKVCount := len(dp.LogContent.ExtraKVs)
	if longKVCount == 0 {
		return nil
	}

	remainingLengthForTextKV := sizeLimit
	longTextKVCount := 0
	for _, kv := range dp.LogContent.ExtraKVs {
		if kv.ValueType != TextType {
			remainingLengthForTextKV -= kv.Size()
		} else {
			longTextKVCount++
		}
	}

	if longTextKVCount == 0 || remainingLengthForTextKV <= 0 {
		return nil
	}
	averageSize := remainingLengthForTextKV / longTextKVCount
	truncateReasonLength := len(truncatedReason)
	for i, _ := range dp.LogContent.ExtraKVs {
		if dp.LogContent.ExtraKVs[i].ValueType == TextType && dp.LogContent.ExtraKVs[i].Size() > averageSize {
			valueSizeLimit := averageSize - encodedShortStringSize(dp.LogContent.ExtraKVs[i].Key) - 6 - truncateReasonLength

			if valueSizeLimit >= 0 && valueSizeLimit <= len(dp.LogContent.ExtraKVs[i].Value) {
				dp.LogContent.ExtraKVs[i].Value = append(dp.LogContent.ExtraKVs[i].Value[:valueSizeLimit], truncatedReason...)
			}
		}
	}
	return err
}

// AddExtraKeyValue adds a kv to the data pack. This function is for use in log sdk.
// Please don't use this function unless you understand how ByteLog works.
func (dp *DataPack) AddExtraKeyValue(kv *KeyValue) {
	if dp == nil {
		return
	}

	if kv.IsLong() {
		if dp.LogContent == nil {
			dp.LogContent = &LogContent{}
		}
		dp.LogContent.AddExtraKeyValue(kv)
	} else {
		if dp.LogHeader == nil {
			dp.LogHeader = &LogHeader{}
		}
		dp.LogHeader.AddExtraKeyValue(kv)
	}
}

// AddExtraKeyValues adds several kvs to the data pack. This function is for use in log sdk.
// Please don't use this function unless you understand how ByteLog works.
func (dp *DataPack) AddExtraKeyValues(kvs ...*KeyValue) {
	if dp == nil {
		return
	}

	for _, kv := range kvs {
		dp.AddExtraKeyValue(kv)
	}
}

func (dp *DataPack) EncodeHeader(buf []byte) []byte {
	return dp.LogHeader.Encode(buf)
}

func (dp *DataPack) EncodeContent(buf []byte) []byte {
	return dp.LogContent.Encode(buf)
}

func (dp *DataPack) isError() bool {
	if dp.LogHeader.Level == Debug || dp.LogHeader.Level == Info || dp.LogHeader.Level == Notice || dp.LogHeader.Level == Trace {
		return false
	}

	return dp.LogHeader.Level == Error ||
		dp.LogHeader.Level == Warn ||
		dp.LogHeader.Level == Warning ||
		dp.LogHeader.Level == Fatal ||
		strings.Compare(strings.ToLower(dp.LogHeader.Level), strings.ToLower(Error)) == 0 ||
		strings.Compare(strings.ToLower(dp.LogHeader.Level), strings.ToLower(Warn)) == 0 ||
		strings.Compare(strings.ToLower(dp.LogHeader.Level), strings.ToLower(Warning)) == 0 ||
		strings.Compare(strings.ToLower(dp.LogHeader.Level), strings.ToLower(Fatal)) == 0
}

func (dp *DataPack) Recycle() {
	if dp.LogHeader != nil {
		dp.LogHeader.Timestamp = 0
		dp.LogHeader.Source = ""
		dp.LogHeader.Context = ""
		dp.LogHeader.LogId = ""
		dp.LogHeader.Level = ""
		dp.LogHeader.Location = ""
		dp.LogHeader.SpanId = 0
		for _, kv := range dp.LogHeader.ExtraKVs {
			kv.Recycle()
		}
		dp.LogHeader.ExtraKVs = dp.LogHeader.ExtraKVs[:0]
	} else {
		dp.LogHeader = &LogHeader{}
	}

	if dp.LogContent != nil {
		dp.LogContent.Msg = ""
		for _, kv := range dp.LogContent.ExtraKVs {
			kv.Recycle()
		}
		dp.LogContent.ExtraKVs = dp.LogContent.ExtraKVs[:0]
	} else {
		dp.LogContent = &LogContent{}
	}
	dataPackPool.Put(dp)
}

var dataPackPool = &sync.Pool{
	New: func() interface{} {
		return &DataPack{
			LogHeader:  &LogHeader{},
			LogContent: &LogContent{},
		}
	},
}

// DecodeSDKHeader is only for local tests
func DecodeSDKHeader(data []byte) (header *SDKHeader, readLength int, err error) {
	if (len(data)) < SDK_HEADER_FIXED_LEN+2 {
		return nil, 0, ErrNoEnoughBytes
	}
	h := &SDKHeader{}
	h.length, _ = DecodeUint32(data)
	h.magicNumber, _ = DecodeUint32(data[4:])
	rightMagiNumber, _ := StringToUint32(SDK_MAGIC_NUM_STR)
	if h.magicNumber != rightMagiNumber {
		return nil, 0, ErrMagicNumNotMatch
	}
	h.timestamp, _ = DecodeUint64(data[SDK_HEADER_TIMESTAMP_POS:])
	h.logCount, _ = DecodeUint16(data[SDK_HEADER_LOG_COUNT_POS:])
	h.version, _ = DecodeUint8(data[18:])
	h.reserved, _ = DecodeUint8(data[19:])
	h.tenant, err = DecodeVarString(data[20:])
	h.logStreamName, err = DecodeVarString(data[21+h.tenant.length:])

	if err != nil {
		return nil, 0, err
	}
	totalSize := int(SDK_HEADER_FIXED_LEN + 2 + h.tenant.length + h.logStreamName.length)
	return h, totalSize, nil
}

// DecodeByteLogHeader
func DecodeByteLogHeader(data []byte, offset int) (byteLogHeader *ByteLogHeader, readLength int, err error, totalLength, realHeaderLen, originalHeaderLen int) {
	if len(data) < offset+BYTELOG_HEADER_LEN {
		return nil, 0, ErrNoEnoughBytes, 0, 0, 0
	}
	pos := offset
	byteLogHeader = &ByteLogHeader{}
	byteLogHeader.Version, _ = DecodeUint8(data[pos:])
	pos += 1
	byteLogHeader.ProtoType, _ = DecodeUint8(data[pos:])
	pos += 1
	byteLogHeader.Compression, _ = DecodeUint8(data[pos:])
	pos += 1
	byteLogHeader.ReservedFlag, _ = DecodeUint8(data[pos:])
	pos += 1

	length, _ := DecodeUint32(data[pos:])
	totalLength = int(length)
	pos += 4
	length, _ = DecodeUint32(data[pos:])
	realHeaderLen = int(length)
	pos += 4
	length, _ = DecodeUint32(data[pos:])
	originalHeaderLen = int(length)
	pos += 4
	checksum, _ := DecodeUint32(data[pos:])
	byteLogHeader.Checksum = checksum
	pos += 4
	reserved, _ := DecodeUint32(data[pos:])
	byteLogHeader.Reserved = reserved
	pos += 4
	readLength = pos - offset
	if pos-offset != BYTELOG_HEADER_LEN {
		return nil, 0, fmt.Errorf("ByteLogHeader length not match"), 0, 0, 0
	}
	return
}

func decodeContentCompressionInfo(data []byte, offset int) (int, int, byte, int, error) {
	if len(data) < offset+9 {
		return 0, 0, 0, 0, ErrNoEnoughBytes
	}
	pos := offset
	contentCompressedLen, err := DecodeUint32(data[pos:])
	if err != nil {
		return 0, 0, 0, 0, err
	}
	pos += 4
	contentOriginalLen, err := DecodeUint32(data[pos:])
	if err != nil {
		return 0, 0, 0, 0, err
	}
	pos += 4
	contentCompressType, err := DecodeUint8(data[pos:])
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return int(contentCompressedLen), int(contentOriginalLen), contentCompressType, 9, nil
}

func decodePattern(data []byte, offset int) (*ByteLogPattern, int, error) {
	pos := offset
	patternLen, err := DecodeUint32(data[pos:])
	if err != nil {
		return nil, 0, err
	}
	pos += 4
	if patternLen == 0 {
		pattern := make([]byte, 0)
		return (*ByteLogPattern)(&pattern), 4, nil
	}
	pattern := make([]byte, patternLen)
	copy(pattern, data[pos:pos+int(patternLen)])
	pos += int(patternLen)
	return (*ByteLogPattern)(&pattern), pos - offset, nil
}

func decodeCommonHeaders(data []byte, offset int) (*CommonHeaders, int, error) {
	pos := offset
	commonHeadersLen, err := DecodeUint32(data[pos:])
	if err != nil {
		return nil, 0, err
	}
	pos += LENGTH_BYTES

	if commonHeadersLen == 0 {
		return nil, pos - offset, nil
	}
	batchIDKV, readLength := bytesToShortStrings(data[pos:], 2)
	pos += readLength

	if len(batchIDKV[1]) < SEQ_ID_LENGTH {
		return nil, 0, errors.New("invalid batch id")
	}

	KeyFlags, readLength, err := decodeShortStrWithType(data, pos)
	if KeyFlags != KEY_FLAGS {
		return nil, 0, errors.New("not _flags")
	}
	pos += readLength

	pos++ // flag_values

	flags, err := DecodeUint32(data[pos:])
	if err != nil {
		return nil, 0, err
	}
	pos += 4

	sevenFields, readLength := bytesToShortStrings(data[pos:], 14)

	pos += readLength
	psm, idc, stage, cluster, podname, taskname, language :=
		sevenFields[1], sevenFields[3], sevenFields[5], sevenFields[7],
		sevenFields[9], sevenFields[11], sevenFields[13]

	KeyIPv4, readLength, err := decodeShortStrWithType(data, pos)
	pos += readLength

	if KeyIPv4 != KEY_IPV4 || err != nil {
		return nil, 0, errors.New("invalid ipv4 key")
	}

	if data[pos] != Ipv4Type {
		return nil, 0, errors.New("not ipv4 type")
	}
	pos++ // ipv4_type
	ipv4 := Ipv4(binary.LittleEndian.Uint32(data[pos : pos+BYTELOG_IPV4_BYTES]))
	pos += BYTELOG_IPV4_BYTES

	KeyIPv6, readLength, err := decodeShortStrWithType(data, pos)
	pos += readLength
	if KeyIPv6 != KEY_IPV6 || err != nil {
		return nil, 0, errors.New("invalid ipv6 key")
	}

	if data[pos] != Ipv6Type {
		return nil, 0, errors.New("not ipv6 type")
	}
	pos++ //ipv6_type
	var ipv6 [16]byte
	for i := 0; i < BYTELOG_IPV6_BYTES; i++ {
		ipv6[i] = data[pos+i]
	}
	pos += BYTELOG_IPV6_BYTES

	kvs, readLength, err := bytesToShortKVs(data[pos:], offset+LENGTH_BYTES+int(commonHeadersLen)-pos)
	pos += readLength
	if err != nil {
		return nil, 0, err
	}

	if pos != offset+LENGTH_BYTES+int(commonHeadersLen) {
		return nil, 0, errors.New("CommonHeader lengths not match")
	}
	ch := NewCommonHeaders(psm, idc, stage, cluster, podname, taskname, language, ipv4, ipv6)
	ch._logStreamId = batchIDKV[1][:len(batchIDKV[1])-SEQ_ID_LENGTH]
	ch._flags = flags
	ch.extraKVs = kvs
	return ch, pos - offset, nil
}
