package ctxvalues

import (
	"context"
)

const ctxKeyStressTag = "K_STRESS" // stress key （压测）

// StressTag get stress tag from context.Context / 从 context.Context 中获取 stress tag
func StressTag(ctx context.Context) (string, bool) {
	return getStringFromContext(ctx, ctxKeyStressTag)
}

// IsEnableStress get stress if enable from context.Context / 从 context.Context 获取是否开启流量压测
func IsEnableStress(ctx context.Context) bool {
	val, _ := StressTag(ctx)
	return val != "" && val != "-"
}
