package tccclient

import (
	"context"
	"errors"
	"runtime/debug"
	"strings"

	bccsdk "code.byted.org/bcc/bcc-go-client/confclient"
	"code.byted.org/gopkg/logs"
)

var (
	configNotListenError = errors.New("config not listen error")
)

// IsConfigNotFoundError checks if an error indicates that the configuration has
// never been set and released online.
func IsConfigNotFoundError(err error) bool {
	return bccsdk.IsConfNotExistError(err)
}

// IsConfigNotListenError checks if an error indicates that the configuration
// hasn't been watched by using [WithKeys].
func IsConfigNotListenError(err error) bool {
	return errors.Is(err, configNotListenError)
}

func catchPanic(ctx context.Context) {
	if err := recover(); err != nil {
		logs.CtxError(ctx, "catch panic: %v", err)
		logs.CtxError(ctx, "panic exception: %s", debug.Stack())
	}
}

// copy from bcc/common
// 背景: 兼容存量名称中包含 `/` 特殊字符的配置

func encodeConfName(confName string) string {
	return strings.ReplaceAll(confName, "/", "@2F")
}

func decodeConfName(confName string) string {
	return strings.ReplaceAll(confName, "@2F", "/")
}
