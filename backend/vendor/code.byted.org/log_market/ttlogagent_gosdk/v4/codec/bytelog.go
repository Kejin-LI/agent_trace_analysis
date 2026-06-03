package codec

import (
	"encoding/binary"
	"encoding/hex"
	"sync"

	"code.byted.org/gopkg/env"

	"github.com/google/uuid"

	sg "code.byted.org/bytedtrace/serializer-go"
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

// NewDefaultByteLogBatch creates a ByteLogBatch with <tenant, logstream> = <"argos", "argos">
func NewDefaultByteLogBatch() *ByteLogBatch {
	return NewByteLogBatch(DefaultTenant, DefaultLogStreamName)
}

// NewByteLogBatch creates a ByteLogBatch with specified tenant and logstream.
func NewByteLogBatch(tenant, logStream string) *ByteLogBatch {
	return newByteLogBatch(NewSDKHeader(tenant, logStream), NewByteLogMessage(), uuid.New())
}

func newByteLogBatch(header *SDKHeader, logMessage *ByteLogMessage, uuid uuid.UUID) *ByteLogBatch {
	logBatch := &ByteLogBatch{
		header:       header,
		logMessage:   logMessage,
		sdkHeaderBuf: make([]byte, 0, 64),
	}

	logMessage.uuid = uuid
	hex.Encode(logMessage.uuidBuf, logMessage.uuid[:])
	return logBatch
}

// Encode converts a ByteLogBatch into bytes and append them to the buf.
// It first encodes the sdk header and then encodes the ByteLogMessage.
func (m *ByteLogBatch) Encode(buf []byte) ([]byte, error) {
	if m.LogNumber() == 0 {
		return buf, ErrEmptyBatch
	}
	var err error
	if len(m.sdkHeaderBuf) == 0 {
		m.sdkHeaderBuf = m.header.Encode(m.sdkHeaderBuf)
	}

	startOfMessageBatch := len(buf)
	buf = append(buf, m.sdkHeaderBuf...)
	buf, err = m.logMessage.Encode(buf)
	byteLogBodyEnd := len(buf)
	length := byteLogBodyEnd - LENGTH_BYTES - startOfMessageBatch

	// Update length, timestamp and log count in sdk header.
	WriteUint32(buf, startOfMessageBatch+SDK_HEADER_LENGTH_POS, uint32(length))
	WriteUint64(buf, startOfMessageBatch+SDK_HEADER_TIMESTAMP_POS, m.FirstTimeStamp())
	WriteUint16(buf, startOfMessageBatch+SDK_HEADER_LOG_COUNT_POS, uint16(m.LogNumber()))
	return buf, err
}

// AppendLog trys to append a data pack to ByteLogMessage. It may fail in cases like:
// 1. The log is invalid, for example_for_agent, it has no timestamp or no file location.
// 2. The log doesn't have a log header.
// 3. The log doesn't have a log content.
// 4. The number of logs exceeds 4096.
// 5. The size of the log batch exceeds 128k.
// 6. The log is not in the same time window (the same minute).
func (m *ByteLogBatch) AppendLog(log *DataPack) error {
	err := log.Validate()
	if err != nil {
		return err
	}

	if m.Size()+log.Size() > oneMessageLimitByte {
		return ErrExceedOneMessageSizeLimit
	}

	return m.logMessage.AppendLog(log)
}

// SetCommonHeaders updates the common headers filed in the internal ByteLogMessage.
func (m *ByteLogBatch) SetCommonHeaders(headers *CommonHeaders) {
	if m.logMessage == nil {
		m.logMessage = NewByteLogMessage()
	}
	m.logMessage.SetCommonHeaders(headers)
}

// GetCommonHeaders returns the internal ByteLogMessage's common headers.
// It is dangerous to use this function. Please avoid using it.
func (m *ByteLogBatch) GetCommonHeaders() *CommonHeaders {
	if m.logMessage == nil {
		return nil
	}
	return m.logMessage.GetCommonHeaders()
}

// SetSDKHeader updates the SDKHeader of ByteLogBatch.
func (m *ByteLogBatch) SetSDKHeader(sdkHeader *SDKHeader) {
	m.header = sdkHeader
	m.sdkHeaderBuf = m.sdkHeaderBuf[:0]
}

// SetUserId updates the user id in the ByteLogMessage.
func (m *ByteLogBatch) SetUserId(userId uint64) {
	m.logMessage.SetUserId(userId)
}

// Size returns the length of the byte slice after encoding.
func (m *ByteLogBatch) Size() int {
	if m.LogNumber() == 0 {
		return 0
	}
	if len(m.sdkHeaderBuf) == 0 {
		m.sdkHeaderBuf = m.header.Encode(m.sdkHeaderBuf)
	}

	return len(m.sdkHeaderBuf) + m.logMessage.Size()
}

// LogNumber returns the number of data packs in the ByteLogMessage.
func (m *ByteLogBatch) LogNumber() int {
	return m.logMessage.LogNumber()
}

// Clear clears the sdk header and the ByteLogMessage.
func (m *ByteLogBatch) Clear() {
	m.header.clear()
	m.logMessage.Clear()
}

// Reset clear the sdk header and the ByteLogMessage.
// It also reset the ByteLog header and common headers in the ByteLogMessage.
func (m *ByteLogBatch) Reset() {
	m.Clear()
	m.sdkHeaderBuf = m.sdkHeaderBuf[:0]
	m.logMessage.Reset()
}

// EnableDebug will set a bit in ByteLogMessage's reserved field.
func (m *ByteLogBatch) EnableDebug(isEnabled bool) {
	m.logMessage.EnableDebug(isEnabled)
}

// ContainsErrorLog returns whether the ByteLogMessage contains warn, error, or fatal logs.
func (m *ByteLogBatch) ContainsErrorLog() bool {
	return m.logMessage.ContainsErrorLog()
}

// IncrId increases the sequenceId.
// This function needs to be called after sending. Note that don't use this func after encoding.
func (m *ByteLogBatch) IncrId() {
	m.logMessage.IncrId()
}

// FirstTimeStamp returns the earliest log timestamp in the batch.
func (m *ByteLogBatch) FirstTimeStamp() uint64 {
	return m.logMessage.FirstTimeStamp()
}

// LastTimeStamp returns the last log timestamp in the batch.
func (m *ByteLogBatch) LastTimeStamp() uint64 {
	return m.logMessage.LastTimeStamp()
}

//SDKHeader
//+--------+--------------+-----------+------------+---------+----------+-------------+-----------------+
//|   4B   |      4B      |    8B     |    2B      |   1B    |    1B    |  VarString  |    VarString    |
//+--------+--------------+-----------+------------+---------+----------+-------------+-----------------+
//| length | magic number | timestamp | logs count | version | reserved | tenant name | log stream name |
//+--------+--------------+-----------+------------+---------+----------+-------------+-----------------+
//- length：为后续报文的长度，包括SDK header的剩余部分以及Bytelog message；
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

func (h *SDKHeader) clear() {
	h.length = 0
	h.logCount = 0
	h.timestamp = 0
}

// ByteLogMessage is an implementation of StreamLogBatch.
// One ByteLogMessage contains multi data packs (logs).
type ByteLogMessage struct {
	byteLogHeader *ByteLogHeader
	pattern       *ByteLogPattern
	commonHeaders *CommonHeaders
	dataPacks     []*DataPack

	firstTimeStamp uint64
	lastTimeStamp  uint64
	currSize       int

	flags             uint32
	uuid              uuid.UUID
	seqId             uint64
	uuidBuf           []byte
	logHeadersAreaEnd int
	byteLogHeadersBuf []byte
	commonHeadersBuf  []byte
}

//ByteLogHeader
//+-------+------------+-------------+----------+--------------+-------------------------+---------------------+-------+
//|  1B   |     1B     |     1B      |   1B     |      4B      |          4B             |          4B         |  4B   |
//+-------+------------+-------------+----------+--------------+-------------------------+---------------------+-------+
//|version| prototype  | compression | reserved | total Length |header compressed Length |header origin Length |user id|
//+-------+------------+-------------+----------+--------------+-------------------------+---------------------+-------+
type ByteLogHeader struct {
	Version     byte
	ProtoType   byte
	Compression byte
	Reserved    byte
	UserId      uint64

	ContentCompression byte
}

type ByteLogPattern []byte

// NewByteLogMessage creates a default ByteLogMessage. Most bytes are 0 by default.
func NewByteLogMessage() *ByteLogMessage {
	logMessage := &ByteLogMessage{
		byteLogHeader:     NewDefaultByteLogHeader(),
		pattern:           nil,
		commonHeaders:     nil,
		dataPacks:         make([]*DataPack, 0, 128),
		uuid:              uuid.New(),
		uuidBuf:           make([]byte, UUID_LENGTH),
		byteLogHeadersBuf: make([]byte, 0, 32),
		commonHeadersBuf:  make([]byte, 0, 128),
	}

	hex.Encode(logMessage.uuidBuf, logMessage.uuid[:])
	return logMessage
}

// Encode converts a ByteLogMessage into bytes and append them to the buf.
// It first encodes the ByteLog header, then the common headers, then log headers and the log contents in the end.
func (bm *ByteLogMessage) Encode(buf []byte) ([]byte, error) {
	if bm.LogNumber() == 0 {
		return buf, ErrEmptyBatch
	}
	var err error
	startOfByteLogMessage := len(buf)
	if len(bm.byteLogHeadersBuf) == 0 {
		bm.byteLogHeadersBuf = bm.byteLogHeader.Encode(bm.byteLogHeadersBuf) // byteLogHeader
		bm.byteLogHeadersBuf = bm.pattern.Encode(bm.byteLogHeadersBuf)       // pattern
	}

	if len(bm.commonHeadersBuf) == 0 {
		bm.commonHeadersBuf = bm.commonHeaders.Encode(bm.commonHeadersBuf) // common headers
		if len(bm.commonHeadersBuf) > LENGTH_BYTES {
			hex.Encode(bm.commonHeadersBuf[LENGTH_BYTES+EncodedStringSize(KEY_BATCH_ID)+1+1:], bm.uuid[:])
		}

	}
	buf = append(buf, bm.byteLogHeadersBuf...)
	commonHeaderStart := len(buf)
	buf = append(buf, bm.commonHeadersBuf...)
	if len(bm.commonHeadersBuf) > LENGTH_BYTES {
		WriteUint64Hex(buf, commonHeaderStart+COMMONHEADER_SEQID_POS, bm.seqId)
		WriteUint32(buf, commonHeaderStart+COMMONHEADER_FLAG_POS, bm.flags)
	}

	buf = bm.serializeDataPacks(buf, bm)
	byteLogBodyEnd := len(buf)

	// Set Length in ByteLog message header
	WriteUint32(buf, startOfByteLogMessage+BYTELOG_HEADER_LEN-DISTANCE_BTW_BODY_LEN_POS_AND_BODY,
		uint32(byteLogBodyEnd-BYTELOG_HEADER_LEN-startOfByteLogMessage))
	WriteUint32(buf, startOfByteLogMessage+BYTELOG_HEADER_LEN-DISTANCE_BTW_HEADER_COMP_LEN_POS_AND_HEADER,
		uint32(bm.logHeadersAreaEnd-BYTELOG_HEADER_LEN-startOfByteLogMessage))
	WriteUint32(buf, startOfByteLogMessage+BYTELOG_HEADER_LEN-DISTANCE_BTW_HEADER_ORIN_LEN_POS_AND_HEADER,
		uint32(bm.logHeadersAreaEnd-BYTELOG_HEADER_LEN-startOfByteLogMessage))
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
	err := log.Validate()
	if err != nil {
		return err
	}

	if bm.LogNumber() >= oneMessageLimitLogNumber {
		return ErrTooManyLogs
	}

	if bm.currSize == 0 {
		bm.currSize = bm.Size()
	}
	if bm.currSize+log.Size() > oneMessageLimitByte {
		return ErrExceedOneMessageSizeLimit
	}

	if bm.LogNumber() == 0 {
		bm.firstTimeStamp = log.Time()
		bm.lastTimeStamp = log.Time()
	} else {
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

// FirstTimeStamp returns the earliest log timestamp in the batch.
func (bm *ByteLogMessage) FirstTimeStamp() uint64 {
	return bm.firstTimeStamp
}

// LastTimeStamp returns the last log timestamp in the batch.
func (bm *ByteLogMessage) LastTimeStamp() uint64 {
	return bm.lastTimeStamp
}

// Size returns the size of the byte slice of encoding.
func (bm *ByteLogMessage) Size() int {
	if bm.LogNumber() == 0 {
		return 0
	}
	if bm.currSize == 0 {
		bm.currSize = bm.actualSize()
	}

	return bm.currSize
}

// IncrId increases the sequenceId.
// This function needs to be called after sending. Note that don't use this func after encoding.
func (bm *ByteLogMessage) IncrId() {
	bm.seqId++
}

// ContainsErrorLog returns whether the ByteLogMessage contains warn, error, or fatal logs.
func (bm *ByteLogMessage) ContainsErrorLog() bool {
	return (bm.flags & 0x01) != 0
}

// EnableDebug will set a bit in ByteLogMessage's reserved field.
func (bm *ByteLogMessage) EnableDebug(isEnabled bool) {
	reserved := bm.byteLogHeader.Reserved
	if isEnabled {
		reserved |= 0x01
	} else {
		reserved &= 0xFE
	}
	bm.byteLogHeader = newByteLogHeader(
		bm.byteLogHeader.Version,
		bm.byteLogHeader.ProtoType,
		bm.byteLogHeader.Compression,
		bm.byteLogHeader.ContentCompression,
		reserved,
		bm.byteLogHeader.UserId,
	)
	bm.byteLogHeadersBuf = bm.byteLogHeadersBuf[:0]
}

func (bm *ByteLogMessage) Clear() {
	bm.flags = 0
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

func (bm *ByteLogMessage) SetUserId(userId uint64) {
	bm.byteLogHeader.UserId = userId
	bm.byteLogHeadersBuf = bm.byteLogHeadersBuf[:0]
}

func (bm *ByteLogMessage) actualSize() int {
	headerAreaSize := BYTELOG_HEADER_LEN + bm.pattern.Size() + bm.commonHeaders.GetEncodedSize()
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
	bm.flags |= 0x01
}

func (bm *ByteLogMessage) serializeDataPacks(buf []byte, logMessage *ByteLogMessage) []byte {
	logHeadersLengthPos := len(buf)
	buf = EncodeUint32(buf, 0) // length of logMessage headers
	startOfLogHeaders := len(buf)
	for i := range logMessage.dataPacks {
		buf = logMessage.dataPacks[i].EncodeHeader(buf)
	}
	bm.logHeadersAreaEnd = len(buf)
	logHeadersLength := uint32(len(buf) - startOfLogHeaders)
	WriteUint32(buf, logHeadersLengthPos, logHeadersLength)

	contentLengthPos := len(buf)
	buf = encodeContentCompressInfo(buf, logMessage.byteLogHeader.ContentCompression)

	logContentAreaStart := len(buf)
	for i := range logMessage.dataPacks {
		buf = logMessage.dataPacks[i].EncodeContent(buf)
	}
	logContentAreaEnd := len(buf)
	contentLength := uint32(logContentAreaEnd - logContentAreaStart)

	WriteUint32(buf, contentLengthPos, contentLength)
	WriteUint32(buf, contentLengthPos+LENGTH_BYTES, contentLength)
	return buf
}

// SetCommonHeaders sets the CommonHeaders of the log batch.
// It is usually called for only once.
func (bm *ByteLogMessage) SetCommonHeaders(headers *CommonHeaders) {
	bm.commonHeaders = headers
	bm.commonHeadersBuf = bm.commonHeadersBuf[:0]
	bm.commonHeadersBuf = headers.Encode(bm.commonHeadersBuf)
	if len(bm.commonHeadersBuf) > LENGTH_BYTES {
		hex.Encode(bm.commonHeadersBuf[LENGTH_BYTES+EncodedStringSize(KEY_BATCH_ID)+1+1:], bm.uuid[:])
	}
	bm.currSize = bm.actualSize()
}

func (bm *ByteLogMessage) SetUUID(newUUID uuid.UUID) {
	bm.uuid = newUUID
	hex.Encode(bm.uuidBuf, bm.uuid[:])
	if len(bm.commonHeadersBuf) > LENGTH_BYTES {
		hex.Encode(bm.commonHeadersBuf[LENGTH_BYTES+EncodedStringSize(KEY_BATCH_ID)+1+1:], bm.uuid[:])
	}
	bm.currSize = bm.actualSize()
}

// GetCommonHeaders returns the internal ByteLogMessage's common headers.
// It is dangerous to use this function. Please avoid using it.
func (bm *ByteLogMessage) GetCommonHeaders() *CommonHeaders {
	return bm.commonHeaders
}

// GetDataPacks return the internal data packs in the ByteLogMessage.
// It is dangerous to use this function. Please avoid using it.
func (bm *ByteLogMessage) GetDataPacks() []*DataPack {
	return bm.dataPacks
}

func NewDefaultByteLogHeader() *ByteLogHeader {
	return NewByteLogHeader(DEFAULT_USER_ID)
}

func NewByteLogHeader(userID uint64) *ByteLogHeader {
	return newByteLogHeader(BYTELOG_VERSION, BYTELOG_PROTOTYPE, DEFAULT_HEADER_COMPRESSION, DEFAULT_CONTENT_COMPRESSION, BYTELOG_RESERVED, userID)
}

func newByteLogHeader(version, protoType, compression, contentCompression, reserved byte, userId uint64) *ByteLogHeader {
	return &ByteLogHeader{
		Version:            version,
		ProtoType:          protoType,
		Compression:        compression,
		Reserved:           reserved,
		UserId:             userId,
		ContentCompression: contentCompression,
	}
}

func (h *ByteLogHeader) Encode(buf []byte) []byte {
	buf = EncodeUint8(buf, h.Version)
	buf = EncodeUint8(buf, h.ProtoType)
	buf = EncodeUint8(buf, h.Compression)
	buf = EncodeUint8(buf, h.Reserved)
	buf = EncodeUint32(buf, 0) // ByteLog Body Total Length
	buf = EncodeUint32(buf, 0) // ByteLog Header Area Compressed Length
	buf = EncodeUint32(buf, 0) // ByteLog Header Area Origin Length
	buf = EncodeUint64(buf, h.UserId)
	return buf
}

func (p *ByteLogPattern) Encode(buf []byte) []byte {
	if p == nil {
		buf = EncodeUint32(buf, 0)
		return buf
	}
	// TODO: support pattern in the future
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

func (h *LogHeader) Size() int {
	totalLength := 4 // Length of LogHeader
	totalLength += 9 // Timestamp (type + uint64)
	totalLength += EncodedStringSize(h.Source)
	totalLength += EncodedStringSize(h.Context)
	totalLength += EncodedStringSize(h.LogId)
	totalLength += EncodedKVSizeStr(KEY_LEVEL, h.Level)
	totalLength += EncodedKVSizeStr(KEY_LOCATION, h.Location)
	totalLength += EncodedStringSize(KEY_SPANID)
	totalLength += 9 // span_id (type + uint64)

	// TODO: check whether long string is allowed here
	for _, kv := range h.ExtraKVs {
		totalLength += kv.Size()
	}
	return totalLength
}

func (h *LogHeader) Encode(buf []byte) []byte {
	if h == nil {
		return buf
	}
	logHeaderLengthPos := len(buf)
	buf = EncodeUint32(buf, 0) // length
	logHeaderStart := len(buf)
	buf = append(buf, sg.Uint64Type)
	buf = EncodeUint64(buf, h.Timestamp)
	buf = encodeShortStr(buf, h.Source)
	buf = encodeShortStr(buf, h.Context)
	buf = encodeShortStr(buf, h.LogId)
	buf = EncodeKeyValue(buf, KEY_LEVEL, StringToSliceByte(h.Level), StringType)
	buf = EncodeKeyValue(buf, KEY_LOCATION, StringToSliceByte(h.Location), StringType)
	buf = EncodeKeyValueUint64(buf, KEY_SPANID, h.SpanId)

	for _, kv := range h.ExtraKVs {
		buf = kv.Encode(buf)
	}

	WriteUint32(buf, logHeaderLengthPos, uint32(len(buf)-logHeaderStart))
	return buf
}

// AddExtraKV creates a short Key-Value pair and store this kv.
// The key and value may be trimmed.
func (h *LogHeader) AddExtraKV(key, value interface{}) {
	if h == nil {
		return
	}
	kv, err := NewKeyValue(key, value) // This is a short KV.
	if err != nil {
		return
	}

	h.ExtraKVs = append(h.ExtraKVs, kv)
}

// AddExtraKVs creates several Key-Value pairs. The keys and values may be trimmed.
func (h *LogHeader) AddExtraKVs(kvlist ...interface{}) {
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

// AddExtraKeyValue directly add the KeyValue to the slice. It does NOT check whether the KV is long.
// TODO: check this behaviour. Currently bytelog allows long kvs in the log headers.
func (h *LogHeader) AddExtraKeyValue(kv *KeyValue) error {
	if h == nil {
		return ErrNilObject
	}

	h.ExtraKVs = append(h.ExtraKVs, kv)
	return nil
}

func (h *LogHeader) ResetExtraKVs() {
	h.ExtraKVs = h.ExtraKVs[:0]
}

type LogContent struct {
	Msg      string
	ExtraKVs []*KeyValue
}

func (c *LogContent) Encode(buf []byte) []byte {
	if c == nil {
		return buf
	}
	buf = EncodeUint32(buf, 0)
	start := len(buf)
	buf = EncodeKeyValueStr(buf, KEY_MSG, c.Msg)
	for _, kv := range c.ExtraKVs {
		buf = kv.Encode(buf)
	}
	end := len(buf)
	WriteUint32(buf, start-4, uint32(end-start))
	return buf
}

func (c *LogContent) Size() int {
	if c == nil {
		return 4
	}
	totalLength := 4
	totalLength += EncodedKVSizeStr(KEY_MSG, c.Msg)
	return totalLength
}

// AddExtraKV creates a long Key-Value pair and store this kv.
func (h *LogContent) AddExtraKV(key, value interface{}) {
	if h == nil {
		return
	}
	kv, err := NewKeyValue(key, value, true)
	if err != nil {
		return
	}

	h.ExtraKVs = append(h.ExtraKVs, kv)
}

// AddExtraKVs creates several Key-Value pairs.
func (h *LogContent) AddExtraKVs(kvlist ...interface{}) {
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

// AddExtraKeyValue directly add the KeyValue to the slice. It does NOT check whether the KV is long.
func (h *LogContent) AddExtraKeyValue(kv *KeyValue) error {
	if h == nil {
		return ErrNilObject
	}

	h.ExtraKVs = append(h.ExtraKVs, kv)
	return nil
}

func (h *LogContent) ResetExtraKVs() {
	h.ExtraKVs = h.ExtraKVs[:0]
}

// CommonHeaders contains 9 kv pairs.
// We need to encode both the keys and values.
// | key type | length* | key content| value type | length* | value content |
type CommonHeaders struct {
	_psm      string
	_idc      string
	_stage    string
	_cluster  string
	_podname  string
	_taskname string
	_language string
	_ipv4     Ipv4
	_ipv6     Ipv6
	extraKVs  []*KeyValue
}

func NewDefaultCommonHeaders() *CommonHeaders {
	ipv4Uint32, ipv6ByteArray := Ipv4(0), Ipv6{}
	ipv4, ipv6 := env.IPV4(), env.IPV6()
	if len(ipv4) == 4 {
		ipv4Uint32 = Ipv4(binary.LittleEndian.Uint32(ipv4))
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

// NewCommonHeaders created the common headers
// This function is called in log sdk.
func NewCommonHeaders(psm, idc, stage, cluster, podname, taskname, language string, ipv4 Ipv4, ipv6 Ipv6) *CommonHeaders {
	return &CommonHeaders{
		_psm:      psm,
		_idc:      idc,
		_stage:    stage,
		_cluster:  cluster,
		_podname:  podname,
		_taskname: taskname,
		_language: language,
		_ipv4:     ipv4,
		_ipv6:     ipv6,
		extraKVs:  nil,
	}
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

func (h *CommonHeaders) Encode(buf []byte) []byte {
	if h == nil {
		pos := len(buf)
		buf = EncodeUint32(buf, 0)
		start := pos + LENGTH_BYTES
		buf = EncodeKeyValue(buf, KEY_BATCH_ID, make([]byte, BATCH_ID_LENGTH), StringType)
		buf = EncodeKeyValue(buf, KEY_FLAGS, make([]byte, 4), IntType)
		length := len(buf) - start
		WriteUint32(buf, pos, uint32(length))
		return buf
	}
	pos := len(buf)
	buf = EncodeUint32(buf, 0)
	start := pos + LENGTH_BYTES
	buf = EncodeKeyValue(buf, KEY_BATCH_ID, make([]byte, BATCH_ID_LENGTH), StringType)
	buf = EncodeKeyValue(buf, KEY_FLAGS, make([]byte, 4), IntType)
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
	return buf
}

func (h *CommonHeaders) GetEncodedSize() int {
	if h == nil {
		return 4
	}
	total := 4
	total += EncodedKVIBatchIDSize(KEY_BATCH_ID)
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

func (dp *DataPack) Size() int {
	return dp.LogHeader.Size() + dp.LogContent.Size()
}

// Validate checks whether a log(data pack) is valid.
// If only valid logs can be appended to LogBatch
func (dp *DataPack) Validate() error {
	if dp == nil {
		return ErrNilDataPack
	}

	if dp.LogHeader == nil {
		return ErrNilLogHeader
	}

	if dp.LogHeader.Timestamp == 0 || len(dp.LogHeader.Location) == 0 {
		return ErrInvalidLogHeader
	}

	if dp.LogContent == nil {
		return ErrNilLogContent
	}

	if dp.Size() > oneMessageLimitByte {
		return ErrTooLargeDataPack
	}

	return nil
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
	return dp.LogHeader.Level == Error ||
		dp.LogHeader.Level == Warn ||
		dp.LogHeader.Level == Fatal
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
