package ctxvalues

import (
	"context"
)

const CTXKeyLogID = "K_LOGID" // logid key

// SetLogID set logid to context.Context / 在 context.Context 中设置 logid
func SetLogID(ctx context.Context, logid string) context.Context {
	return context.WithValue(ctx, CTXKeyLogID, logid)
}

// LogID get logid from context.Context / 从 context.Context 中获取 logid
//
// return (val, ok), if ok == false, then val must be empty string
func LogID(ctx context.Context) (string, bool) {
	return getStringFromContext(ctx, CTXKeyLogID)
}

// LogIDDefault get logid from context.Context / 从 context.Context 中获取 logid and not return bool
//
// if logid not found in ctx, then return default logid: '-'
func LogIDDefault(ctx context.Context) string {
	val, _ := LogID(ctx)
	if val == "" {
		return "-"
	}
	return val
}
