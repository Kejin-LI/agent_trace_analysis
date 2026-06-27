package codec

import "github.com/google/uuid"

// LogBatch represents a batch of logs sending from log sdk to log agent.
// There are two implementations: ByteLogBatch and FlatByteLogBatch.
type LogBatch interface {
	// Setters
	// SetSDKHeader updates the sdkHeader.
	// This function can update the tenant name or log stream name.
	SetSDKHeader(header *SDKHeader)

	GetTenant() string

	GetLogStream() string

	GetLogMessage() LogMessage

	LogMessage
}

// LogMessage represents a batch of logs.
// The difference between LogMessage and LogBatch is that LogMessage may not have a SDKHeader.
// There are four implementations: ByteLogMessage, FlatByteLogMessage, ByteLogBatch, FlatByteLogBatch.
type LogMessage interface {
	// Setters

	// AppendLog trys to append a data pack to ByteLogMessage. It may fail in cases like:
	// 1. The log is invalid, for example_for_agent, it has no timestamp or no file location.
	// 2. The log doesn't have a log header.
	// 3. The log doesn't have a log content.
	// 4. The number of logs exceeds 4096.
	// 5. The size of the log batch exceeds 128k.
	// 6. The log is not in the same time window (the same minute).
	AppendLog(log *DataPack) error

	// Encode encodes LogBatch and append the content to the buf.
	Encode(buf []byte) ([]byte, error)

	// SetCommonHeaders sets the CommonHeaders of the log batch.
	// It even copys the provided commonHeaders' uuid.
	SetCommonHeaders(commonHeaders *CommonHeaders)

	// SetNewCommonHeaders sets the CommonHeaders of the log batch.
	// It copys all the new commonheaders' fields but the uuid.
	// It is usually called for only once.
	SetNewCommonHeaders(commonHeaders *CommonHeaders)

	// Clear removes all logs in ByteLogMessage. This function will NOT Clear the common headers.
	Clear()

	// Reset removes all logs in ByteLogMessage. This function will Clear the common headers and buffers.
	Reset()

	// EnableDebug enables or disables Debug Mode. It will set a flag in the common headers.
	EnableDebug(isEnabled bool)

	// IncrId increases the id of the log batch. This is function is public since the outer sender may send one batch
	// for several times, and it is responsible for increasing the id.
	IncrId()

	// SetSeqId sets the seqId of the byteLogMessage.
	// It is dangerous to use this function. Please understand what you are doing.
	SetSeqId(newSeqId uint64)

	// SetUUID uses an uuid to compose the batchId in the CommonHeaders.
	SetUUID(uuid uuid.UUID)

	// SetLogStreamId uses a string to compose the batchId in the CommonHeaders.
	SetLogStreamId(logStreamId string) error

	// SetHeaderCompression sets the header compression option.
	SetHeaderCompression(compressType CompressType, useGlobalCompress ...bool)

	// SetContentCompression sets the content compression option.
	SetContentCompression(compressType CompressType, useGlobalCompress ...bool)

	// SetLargeMessage increase message size limit to 10MB
	SetLargeMessage()

	// SetChecksum sets the CRC-32 checksum of data to UserId in ByteLogHeader
	SetChecksum(isEnabled bool)

	// SetErrorLogSeparated is temp for separate error log
	SetErrorLogSeparated(isEnabled bool)

	// SetTruncateLargeLogs
	SetTruncateLargeLogs(isEnabled bool)

	// SetMessageSizeLimit
	SetMessageSizeLimit(limit int)

	// Getters

	// Size returns the size of the message of encoding without compression.
	Size() int

	// LogNumber returns the number of logs in the batch.
	LogNumber() int

	// ContainsErrorLog indicates whether there is a log of which the level is WARN, ERROR, or FATAL.
	ContainsErrorLog() bool

	// FirstTimeStamp returns the earliest log timestamp in the batch.
	FirstTimeStamp() uint64

	// LastTimeStamp returns the latest log timestamp in the batch.
	LastTimeStamp() uint64

	// GetSeqId returns the internal ByteLogMessage.
	GetSeqId() uint64

	// GetMessageSizeLimit return the size limit for this LogMessage.
	GetMessageSizeLimit() int
}
