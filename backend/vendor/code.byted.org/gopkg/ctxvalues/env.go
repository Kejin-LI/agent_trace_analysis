package ctxvalues

import (
	"context"
)

const ctxKeyENV = "K_ENV" // 上游服务带过来的环境参数

// Env get env from context.Context / 从 context.Context 中获取 env
func Env(ctx context.Context) (string, bool) {
	return getStringFromContext(ctx, ctxKeyENV)
}

// EnvDefault get env from context.Context / 从 context.Context 中获取 env
//
// if env not found in ctx, then return default env: ''
func EnvDefault(ctx context.Context) string {
	val, _ := Env(ctx)
	return val
}
