package tools

import (
	"code.byted.org/gopkg/logs"
	ztihelper "code.byted.org/security/zti-jwt-helper-golang/helper"
)

func GetZtiToken(disableAuth bool) string {
	if disableAuth {
		return ""
	}
	token, err := ztihelper.GetJwtSVID()
	if err != nil {
		logs.Info("ZTI token get failed, err:%v", err)
	}
	return token
}
