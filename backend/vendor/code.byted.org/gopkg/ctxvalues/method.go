package ctxvalues

import (
	"context"
)

const ctxKeyMethod = "K_METHOD" // 本服务当前所处的接口名字（也就是Method名字）

// Method get method from context.Context / 从 context.Context 中获取 method
func Method(ctx context.Context) (string, bool) {
	return getStringFromContext(ctx, ctxKeyMethod)
}
