package cel

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"code.byted.org/bcc/conf_engine/jsonconf"
	json "code.byted.org/bcc/conf_engine/jsoniter"
	"code.byted.org/bcc/conf_engine/model"
	"code.byted.org/gopkg/logs"
)

// conf内容不考虑做占位符替换
func FormatConfStr(t *model.FormatData, s *model.ConfNode) {
	txt := s.OriText
	t.CelData.Txt = strconv.Quote(txt)
}

func FormatCondNodeCel(ctx context.Context, t *model.FormatData, n *model.CondNode, vMap map[string]model.JSONVarInfo) error {
	if n == nil {
		return nil
	}

	orCompose := func(condList []*model.JSONCond) (string, error) {
		if len(condList) == 0 {
			return "true", nil
		}
		var condStrs []string
		for _, c := range condList {
			s, err := FormatCondToCELText(ctx, c, vMap)
			if err != nil {
				return "", fmt.Errorf("FormatCondToCELText error: %v", err)
			}
			condStrs = append(condStrs, s)
		}
		return "(" + strings.Join(condStrs, "||") + ")", nil
	}

	var condStrs []string

	for _, condList := range n.CondMatrix {
		s, err := orCompose(condList)
		if err != nil {
			return err
		}
		condStrs = append(condStrs, s)
	}
	t.CelData.Txt = strings.Join(condStrs, "&&")

	//默认节点
	if t.CelData.Txt == "" {
		t.CelData.Txt = "true"
	}

	return nil
}

func GetAllPaths(cList []Conf) [][]int {
	allPaths := make([][]int, 0)
	for idx, node := range cList {
		paths := make([][]int, 0)
		getJSONNodePaths(idx, &node, []int{}, &paths)
		allPaths = append(allPaths, paths...)
	}
	return allPaths
}
func FormatCondToCELText(ctx context.Context, cond *model.JSONCond, vMap map[string]model.JSONVarInfo) (string, error) {
	lvStr, err := FormatCondLV(ctx, cond, vMap)
	if err != nil {
		return "", fmt.Errorf("FormatCondLV error: %v", err)
	}

	lvType, err := GetLVType(ctx, cond, vMap)
	logs.Debug("lvtype=%+v, cond=%+v, vmap=%+v", lvType, cond, vMap)
	if err != nil {
		return "", fmt.Errorf("GetLVType error: %v", err)
	}

	rvStr, err := FormatConst(ctx, cond.RV, lvType, cond.OP)
	logs.Debug("rv:%+v, cond=%+v, vmap=%+v", rvStr, cond, vMap)
	if err != nil {
		return "", fmt.Errorf("FormatRv error: %v", err)
	}
	finStr, err := FormatOP(ctx, lvStr, rvStr, cond, lvType)
	if err != nil {
		return "", fmt.Errorf("FormatOP error: %v", err)
	}

	return finStr, nil
}

func FormatCondLV(ctx context.Context, cond *model.JSONCond, vMap map[string]model.JSONVarInfo) (string, error) {
	lvText := ""
	if cond.LV != "" {
		v, found := vMap[cond.LV]
		if !found {
			return "", fmt.Errorf("var not found, var_name=%+v", cond.LV)
		}
		switch v.Scope {
		case model.JSONGlobal:
			lvText = GlobalVarPrefix + cond.LV
		case model.JSONLocal:
			lvText = LocalVarPrefix + cond.LV
		default:
			return "", fmt.Errorf("unknown scope type, var_info=%+v", v)
		}
	}

	if cond.Func.FuncName == "" { //未调用函数的情况
		return lvText, nil
	}

	fInfo := getJSONFuncInfo(cond.Func.FuncName)
	if fInfo == nil {
		return "", fmt.Errorf("getJSONFuncInfo empty: %s", cond.Func.FuncName)
	}

	if fInfo.VarCheck != nil {
		err := fInfo.VarCheck(ctx, cond.Func, vMap)
		if err != nil {
			return "", fmt.Errorf("VarCheck error: %v", err)
		}
	}

	fStr, err := FormatFunc(ctx, cond, fInfo, lvText)
	if err != nil {
		return "", fmt.Errorf("FormatFunc error: %v", err)
	}
	return fStr, nil
}

//
// FormatFunc
//  @Description:
//  @param ctx
//  @param cond
//  @param fInfo
//  @param lvText：左值，条件变量
//  @return string
//  @return error
//
func FormatFunc(ctx context.Context, cond *model.JSONCond, fInfo *jsonconf.JSONFuncInfo, lvText string) (string, error) {
	f := cond.Func
	fType := fInfo.FuncType
	fStr, err := fInfo.FormatCEL(ctx, f, lvText)
	if err != nil {
		return "", fmt.Errorf("format error: %v", err)
	}
	if fInfo.CustomCel {
		return fStr, nil
	}
	switch fType {
	case model.JSONFuncInstance:
		if cond.LV == "" {
			return "", fmt.Errorf("JSONFuncInstance lv emtpy, cond=%+v", cond)
		}
		return fmt.Sprintf(`%s.%s`, lvText, fStr), nil
	case model.JSONFuncNormal:
		if cond.LV != "" {
			return "", fmt.Errorf("JSONFuncNormal lv emtpy, cond=%+v", cond)
		}
		return fmt.Sprintf(`%s`, fStr), nil
	}

	return "", fmt.Errorf("unknown funcType, cond=%+v", cond)
}

// format常量，对表达式右值进行format
// todo: 进行宏替换
func FormatConst(ctx context.Context, v interface{}, varType model.VarType, opType model.JSONOpType) (string, error) {
	if v == nil {
		return "", nil
	}
	isCommonType := varType == model.VarTypeInt || varType == model.VarTypeFloat || varType == model.VarTypeBool
	isJSONArrayType := opType == model.JSONOpTypeIN || opType == model.JSONOpTypeRange || opType == model.JSONOpTypeNIN

	if isCommonType && !isJSONArrayType {
		if varType == model.VarTypeFloat {
			return fmt.Sprintf("double(%v)", v), nil
		}
		return fmt.Sprintf("%v", v), nil
	}

	// format float array 切 op=IN or NIN， 特别注意：op=range
	if varType == model.VarTypeFloat && isJSONArrayType {
		// RANGE的情况，是可能取range中的LV和RV，到不是直接取[]interface{}
		rv, ok := v.([]interface{})
		if ok {
			itemList := make([]string, len(rv))
			for index, item := range rv {
				itemList[index] = fmt.Sprintf("double(%v)", item)
			}
			return fmt.Sprintf("[%v]", strings.Join(itemList, ",")), nil
		} else {
			return fmt.Sprintf("double(%v)", v), nil
		}
	}

	str, err := json.MarshalToString(&v)
	if err != nil {
		return "", fmt.Errorf("marshalToString error: %v", err)
	}

	return str, nil
}

func getJSONNodePaths(idx int, node *Conf, curPath []int, allPaths *[][]int) {
	curPath = append(curPath, idx)
	if node.Children == nil {
		*allPaths = append(*allPaths, curPath)
	}
	for cidx, child := range node.Children {
		getJSONNodePaths(cidx, &child, curPath, allPaths)
	}
}

func GetLVType(ctx context.Context, cond *model.JSONCond, vMap map[string]model.JSONVarInfo) (model.VarType, error) {
	if cond.Func.FuncName == "" {
		v, found := vMap[cond.LV]
		if !found {
			return model.VarTypeUnknown, fmt.Errorf("var not found, var_name=%+v", cond.LV)
		}
		return v.Type, nil
	}
	for _, f := range getJSONFuncList() {
		if f.Name == cond.Func.FuncName {
			// get等函数returnType依赖于用户填写的func参数
			if f.ReturnType == model.VarTypeUnknown {
				return cond.Func.RetType, nil
			}
			return f.ReturnType, nil
		}
	}
	return model.VarTypeUnknown, fmt.Errorf("func not found: %+v", cond.Func.FuncName)
}
