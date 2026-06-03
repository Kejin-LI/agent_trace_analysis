package model

import (
	"fmt"
	"time"
)

// 特殊情况，例如path首次下载失败，但重试后成功
type CallbackRetryError struct {
	Dur   time.Duration
	Err   error
	Block bool
}

func (t *CallbackRetryError) Error() string {
	return fmt.Sprintf("AFTER %v BLOCK %v MSG %v", t.Dur, t.Block, t.Err)
}
