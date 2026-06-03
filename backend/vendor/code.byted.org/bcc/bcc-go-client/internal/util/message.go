package util

import (
	"context"
	"net/http"

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

func AddLogIDToHttpReq(ctx context.Context, req *http.Request) {
	if req == nil {
		return
	}

	var logID string
	logIdKeyList := []string{"X-Tt-Logid", "x-tt-logid", "K_LOGID", "K_logid", "X-TT-LOGID"}
	for _, key := range logIdKeyList {
		if v := ctx.Value(key); v != nil {
			if str, ok := v.(string); ok {
				logID = str
				break
			}
		}
	}
	if len(logID) == 0 {
		logID = logid.GenLogID()
	}

	req.Header.Set("X-Tt-Logid", logID)
}
