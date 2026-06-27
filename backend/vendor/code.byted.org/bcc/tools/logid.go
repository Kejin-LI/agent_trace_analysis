package tools

import (
	"context"

	"code.byted.org/gopkg/logid"
)

func CreateCtx() context.Context {
	logID := logid.GenLogID()
	ctx := context.WithValue(context.Background(), "K_LOGID", logID)
	return ctx
}

func GetLogID(ctx context.Context) string {
	return ctx.Value("K_LOGID").(string)
}
