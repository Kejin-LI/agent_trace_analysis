package codec

import "github.com/google/uuid"

type ByteLogOption func(message LogMessage)

// SetUUID uses the provided uuid to generate the batch id in the common headers.
// It will look like "_batch_id={uuid}{seq_id}"
func SetUUID(uuid uuid.UUID) ByteLogOption {
	return func(logMessage LogMessage) {
		logMessage.SetUUID(uuid)
	}
}

// SetLogStreamId uses the provided string to generate the batch id in the common headers.
// It will look like "_batch_id={logStream_id}{seq_id}"
func SetLogStreamId(logStreamId string) ByteLogOption {
	return func(logMessage LogMessage) {
		logMessage.SetLogStreamId(logStreamId)
	}
}

// SetHeaderCompression sets the compression type of the header area.
// For the extraParameters,
// the first one is whether to use a global compressor, which can save memory.
// the second one is whether to skip the common header region, which can reduce burden on proxy service.
func SetHeaderCompression(compressionType CompressType, extraParameters ...bool) ByteLogOption {
	return func(logMessage LogMessage) {
		logMessage.SetHeaderCompression(compressionType, extraParameters...)
	}
}

// SetContentCompression sets the compression type of the content area.
// For the extraParameters, the first one is whether to use a global compressor, which can save memory.
func SetContentCompression(compressionType CompressType, extraParameters ...bool) ByteLogOption {
	return func(logMessage LogMessage) {
		logMessage.SetContentCompression(compressionType, extraParameters...)
	}
}

func SetDebug(isEnabled bool) ByteLogOption {
	return func(logMessage LogMessage) {
		logMessage.EnableDebug(isEnabled)
	}
}

func SetLargeMessage() ByteLogOption {
	return func(message LogMessage) {
		message.SetLargeMessage()
	}
}

func SetChecksum(isEnabled bool) ByteLogOption {
	return func(message LogMessage) {
		message.SetChecksum(isEnabled)
	}
}

// SetErrorLogSeparated is temp for separate error log, this option will make LogBatch AppendLog func return an
// ErrDifferentLevelForErrorLog error when next log's level is different from other logs in origin batch,
// we separate error logs according this special error,
// and notice that error log means those log level is warn, warning, error and fatal.
func SetErrorLogSeparated(isEnabled bool) ByteLogOption {
	return func(message LogMessage) {
		message.SetErrorLogSeparated(isEnabled)
	}
}

func SetTruncateLargeLogs(isEnabled bool) ByteLogOption {
	return func(message LogMessage) {
		message.SetTruncateLargeLogs(isEnabled)
	}
}

func SetMessageSizeLimit(limit int) ByteLogOption {
	return func(message LogMessage) {
		if limit <= 1000 {
			return
		}
		message.SetMessageSizeLimit(limit)
	}
}
