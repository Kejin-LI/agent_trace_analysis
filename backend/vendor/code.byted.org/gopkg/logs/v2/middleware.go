package logs

import (
	"code.byted.org/gopkg/logs/v2/writer"
	"code.byted.org/log_market/ttlogagent_gosdk/v4/codec"
)

// ContentSetter allows to rewrite the log content.
type ContentSetter interface {
	SetBody([]byte)
	SetKVList([]*codec.KeyValue)
}

// RewritableLog provides some methods to get and set logs.
type RewritableLog interface {
	writer.StructuredLog
	ContentSetter
}

// Middleware function is a hook to get and update logs before writing to each writers.
type Middleware func(log RewritableLog) RewritableLog
