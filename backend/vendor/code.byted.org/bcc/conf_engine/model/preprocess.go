package model

import (
	"strconv"
)

// FillVar 预处理流量参数，填充默认条件变量
func FillVar(scope map[string]interface{}, varMap map[string]JSONVarInfo) map[string]interface{} {
	if scope == nil {
		scope = make(map[string]interface{})
	}
	for k, v := range varMap {
		if _, ok := scope[k]; !ok {
			switch v.Type {
			case VarTypeString:
				scope[k] = ""
			case VarTypeFloat:
				scope[k] = 0.0
			case VarTypeBool:
				scope[k] = false
			case VarTypeInt:
				scope[k] = 0
			case VarTypeList:
				scope[k] = make([]interface{}, 0)
			case VarTypeDict:
				scope[k] = make(map[string]interface{})
			default:
				scope[k] = ""
			}
		}
	}
	return scope
}

func FormatConfStr(t *FormatConf, s *ConfNode) {
	txt := s.OriText
	t.Txt = strconv.Quote(txt)
}
