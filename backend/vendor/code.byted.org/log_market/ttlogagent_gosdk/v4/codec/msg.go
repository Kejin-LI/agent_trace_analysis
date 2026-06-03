package codec

// LogBatch represents a batch of logs sending from log sdk to log agent.
// There are two implementations: ByteLogBatch and FlatByteLogBatch.
type LogBatch interface {
	// Setters

	// AppendLog trys to append a data pack to ByteLogBatch. It may fail in cases like:
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
	// It is usually called for only once.
	SetCommonHeaders(commonHeaders *CommonHeaders)

	// SetSDKHeader updates the sdkHeader.
	// This function can update the tenant name or log stream name.
	SetSDKHeader(header *SDKHeader)

	// SetUserId updates the user id in ByteLogMessage header.
	SetUserId(userId uint64)

	// Clear removes all logs in ByteLogMessage. This function will NOT Clear the common headers.
	Clear()

	// Reset removes all logs in ByteLogMessage. This function will Clear the common headers and buffers.
	Reset()

	// EnableDebug enables or disables Debug Mode. It will set a flag in the common headers.
	EnableDebug(isEnabled bool)

	// IncrId increases the id of the log batch. This is function is public since the outer sender may send one batch
	// for several times, and it is responsible for increasing the id.
	IncrId()

	// Getters

	// Size returns the size of the message of encoding.
	Size() int

	// LogNumber returns the number of logs in the batch.
	LogNumber() int

	// ContainsErrorLog indicates whether there is a log of which the level is WARN, ERROR, or FATAL.
	ContainsErrorLog() bool

	// FirstTimeStamp returns the earliest log timestamp in the batch.
	FirstTimeStamp() uint64

	// LastTimeStamp returns the last log timestamp in the batch.
	LastTimeStamp() uint64
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
	// It is usually called for only once.
	SetCommonHeaders(commonHeaders *CommonHeaders)

	// SetUserId updates the user id in ByteLogMessage header.
	SetUserId(userId uint64)

	// Clear removes all logs in ByteLogMessage. This function will NOT Clear the common headers.
	Clear()

	// Reset removes all logs in ByteLogMessage. This function will Clear the common headers and buffers.
	Reset()

	// EnableDebug enables or disables Debug Mode. It will set a flag in the common headers.
	EnableDebug(isEnabled bool)

	// IncrId increases the id of the log batch. This is function is public since the outer sender may send one batch
	// for several times, and it is responsible for increasing the id.
	IncrId()

	// Getters

	// Size returns the size of the message of encoding.
	Size() int

	// LogNumber returns the number of logs in the batch.
	LogNumber() int

	// ContainsErrorLog indicates whether there is a log of which the level is WARN, ERROR, or FATAL.
	ContainsErrorLog() bool

	// FirstTimeStamp returns the earliest log timestamp in the batch.
	FirstTimeStamp() uint64

	// LastTimeStamp returns the last log timestamp in the batch.
	LastTimeStamp() uint64
}
