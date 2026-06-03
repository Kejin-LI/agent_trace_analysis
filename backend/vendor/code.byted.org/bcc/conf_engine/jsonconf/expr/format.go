package expr

import (
	"context"
	"fmt"
	"strings"

	json "code.byted.org/bcc/conf_engine/jsoniter"
	"code.byted.org/bcc/conf_engine/model"
	"code.byted.org/gopkg/logs"
)

var opStrMap = map[model.JSONOpType]string{
	model.JSONOpTypeEQ:  "==",
	model.JSONOpTypeNEQ: "!=",
	model.JSONOpTypeGE:  ">=",
	model.JSONOpTypeGT:  ">",
	model.JSONOpTypeLE:  "<=",
	model.JSONOpTypeLT:  "<",
	model.JSONOpTypeIN:  "in",
	model.JSONOpTypeNIN: "not in",
}

func FormatCondNode(ctx context.Context, t *model.FormatData, n *model.CondNode, vMap map[string]model.JSONVarInfo) error {
	if n == nil {
		return nil
	}
	orCompose := func(condList []*model.JSONCond) (string, error) {
		if len(condList) == 0 {
			return "true", nil
		}
		var condStrs []string
		for _, c := range condList {
			s, err := formatCond(ctx, c, vMap)
			if err != nil {
				return "", fmt.Errorf("FormatCondToEXPRText error: %v", err)
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
	t.ExprData.Txt = strings.Join(condStrs, "&&")

	//默认节点
	if t.ExprData.Txt == "" {
		t.ExprData.Txt = "true"
	}
	return nil

}
func formatCond(ctx context.Context, c *model.JSONCond, vMap map[string]model.JSONVarInfo) (string, error) {
	lvStr, err := formatCondLV(c, vMap)
	if err != nil {
		return "", fmt.Errorf("FormatCondLV error: %v", err)
	}
	lvType, err := getLVType(c, vMap)
	if err != nil {
		return "", fmt.Errorf("getLVType error: %v", err)
	}

	rvStr, err := formatRV(ctx, c.RV, lvType, c.OP)
	logs.Debug("rv:%+v, cond=%+v, vmap=%+v", rvStr, c, vMap)
	if err != nil {
		return "", fmt.Errorf("FormatRv error: %v", err)
	}
	finStr, err := formatOp(c, lvStr, lvType, rvStr)
	if err != nil {
		return "", fmt.Errorf("FormatOP error: %v", err)
	}
	return finStr, nil

}

func formatOp(cond *model.JSONCond, lvStr string, lvType model.VarType, rvStr string) (string, error) {
	opStr, found := opStrMap[cond.OP]
	if found {
		return fmt.Sprintf("(%v %v %v)", lvStr, opStr, rvStr), nil
	}

	switch cond.OP {
	case model.JSONOpTypeRange:
		return oPFormatRange(cond, lvStr, lvType)
	}
	return "", fmt.Errorf("OP unkonwn, cond=%+v", cond)
}

func oPFormatRange(cond *model.JSONCond, lvStr string, lvType model.VarType) (string, error) {
	valList, ok := cond.RV.([]interface{})
	if !ok {
		return "", fmt.Errorf("op range, rv not list, cond=%+v, rv.Type=%T", cond, cond.RV)
	}
	l := len(valList)
	if l%2 != 0 {
		return "", fmt.Errorf("op range, rv length not even, cond=%+v, rvList=%+v", cond, valList)
	}
	var subConds []string
	for i := 0; i < l; i += 2 {
		lv, rv := valList[i], valList[i+1]
		if lv != nil && rv != nil {
			subConds = append(subConds, fmt.Sprintf(`%s >= %v&&%s <= %v`, lvStr, lv, lvStr, rv))
		}
		if lv != nil && rv == nil {
			subConds = append(subConds, fmt.Sprintf(`%s >= %v`, lvStr, lv))
		}
		if lv == nil && rv != nil {
			subConds = append(subConds, fmt.Sprintf(`%s <= %v`, lvStr, rv))
		}

	}
	return fmt.Sprintf("(%v)", strings.Join(subConds, " || ")), nil
}

func formatRV(ctx context.Context, rv interface{}, lvType model.VarType, opType model.JSONOpType) (string, error) {
	if rv == nil {
		return "", nil
	}
	isJSONArrayType := opType == model.JSONOpTypeIN || opType == model.JSONOpTypeRange || opType == model.JSONOpTypeNIN
	isCommonType := lvType == model.VarTypeInt || lvType == model.VarTypeFloat || lvType == model.VarTypeBool
	if !isJSONArrayType && isCommonType {
		return fmt.Sprintf("%v", rv), nil
	}

	str, err := json.MarshalToString(&rv)

	if err != nil {
		return "", fmt.Errorf("marshalToString error: %v", err)
	}

	return str, nil
}
func getLVType(cond *model.JSONCond, vMap map[string]model.JSONVarInfo) (model.VarType, error) {
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
func formatCondLV(cond *model.JSONCond, vMap map[string]model.JSONVarInfo) (string, error) {
	lv := cond.LV
	if lv != "" {
		_, found := vMap[cond.LV]
		if !found {
			return "", fmt.Errorf("var not found, var_name=%+v", cond.LV)
		}

	}

	if cond.Func.FuncName == "" { //未调用函数的情况
		return cond.LV, nil
	}

	fInfo := getJSONFuncInfo(cond.Func.FuncName)
	if fInfo == nil {
		return "", fmt.Errorf("getJSONFuncInfo empty: %s", cond.Func.FuncName)
	}

	if fInfo.VarCheck != nil {
		err := fInfo.VarCheck(context.TODO(), cond.Func, vMap)
		if err != nil {
			return "", fmt.Errorf("VarCheck error: %v", err)
		}
	}
	fStr, err := fInfo.FormatEXPR(cond.Func, lv)
	if err != nil {
		return "", fmt.Errorf("FormatEXPR function error: %v", err)
	}
	return fStr, nil
}
