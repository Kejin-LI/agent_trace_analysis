package cel

import (
	"context"
	"fmt"
	"strings"

	"code.byted.org/bcc/conf_engine/model"
)

var opStrMap = map[model.JSONOpType]string{
	model.JSONOpTypeEQ:  "==",
	model.JSONOpTypeNEQ: "!=",
	model.JSONOpTypeGE:  ">=",
	model.JSONOpTypeGT:  ">",
	model.JSONOpTypeLE:  "<=",
	model.JSONOpTypeLT:  "<",
}

func FormatOP(ctx context.Context, lvStr string, rvStr string, cond *model.JSONCond, vType model.VarType) (string, error) {
	opStr, found := opStrMap[cond.OP]
	if found {
		return fmt.Sprintf("(%v %v %v)", lvStr, opStr, rvStr), nil
	}

	switch cond.OP {
	case model.JSONOpTypeIN, model.JSONOpTypeNIN:
		return OpFormatIN(ctx, lvStr, rvStr, cond, vType)
	case model.JSONOpTypeRange:
		return OPFormatRange(ctx, lvStr, rvStr, cond, vType)
	}
	return "", fmt.Errorf("OP unkonwn, cond=%+v", cond)
}

func OpFormatIN(ctx context.Context, lvStr string, rvStr string, cond *model.JSONCond, vType model.VarType) (string, error) {
	var finStr string
	valList, ok := cond.RV.([]interface{})
	if !ok {
		return "", fmt.Errorf("op in/nin, rv not list, cond=%+v, rv.Type=%T", cond, cond.RV)
	}
	if len(valList) == 1 {
		rvSingleVal, err := FormatConst(ctx, valList[0], vType, model.JSONOpTypeIN)
		if err != nil {
			return "", fmt.Errorf("FormatConst error: %v, rvStr=%s, lvStr=%s", err, rvStr, lvStr)
		}
		if cond.OP == model.JSONOpTypeIN {
			finStr = fmt.Sprintf("(%v == %v)", lvStr, rvSingleVal)
		} else {
			finStr = fmt.Sprintf("(%v != %v)", lvStr, rvSingleVal)
		}
	} else {
		if cond.OP == model.JSONOpTypeIN {
			finStr = fmt.Sprintf("(%v in %v)", lvStr, rvStr)
		} else {
			finStr = fmt.Sprintf("!(%v in %v)", lvStr, rvStr)
		}
	}
	return finStr, nil
}

// Range运算符: 闭区间，[v[0], v[1]] ∪ [v[2], v[3]] ...
func OPFormatRange(ctx context.Context, lvStr string, rvStr string, cond *model.JSONCond, vType model.VarType) (string, error) {
	valList, ok := cond.RV.([]interface{})
	if !ok {
		return "", fmt.Errorf("op range, rv not list, cond=%+v, rv.Type=%T", cond, cond.RV)
	}
	l := len(valList)
	if l%2 != 0 {
		return "", fmt.Errorf("op range, rv length not even, cond=%+v, rvList=%+v", cond, valList)
	}

	var err error
	retList := []string{}
	for i := 0; i < l; i += 2 {
		tStr := []string{}
		lv, rv := valList[i], valList[i+1]
		lb, rb := "", ""

		if lv != nil {
			lb, err = FormatConst(ctx, lv, vType, model.JSONOpTypeRange)
			if err != nil {
				return "", fmt.Errorf("FormatConst error: %v", err)
			}
		}
		if rv != nil {
			rb, err = FormatConst(ctx, rv, vType, model.JSONOpTypeRange)
			if err != nil {
				return "", fmt.Errorf("FormatConst error")
			}
		}
		if lb == "" && rb == "" {
			return "", fmt.Errorf("range op, lb and rb empty, cond=%+v, valList=%+v", cond, valList)
		}
		if lb != "" {
			tStr = append(tStr, fmt.Sprintf("%v >= %v", lvStr, lb))
		}
		if rb != "" {
			tStr = append(tStr, fmt.Sprintf("%v <= %v", lvStr, rb))
		}
		retList = append(retList, fmt.Sprintf("%v", strings.Join(tStr, "&&")))
	}
	return fmt.Sprintf("(%v)", strings.Join(retList, " || ")), nil
}
