package cel

import (
	"fmt"

	"code.byted.org/ttarch/byteconf-cel-go/cel"
	"code.byted.org/ttarch/byteconf-cel-go/common/types"
)

func EvalBool(p cel.Program, scope map[string]interface{}) (bool, error) {
	retRaw, _, err := p.Eval(scope)
	if err != nil {
		return false, fmt.Errorf("eval error: %v", err)
	}
	retBool, ok := retRaw.(types.Bool)
	if !ok {
		return false, fmt.Errorf("result not Bool: ret=%+v, type=%T", retRaw, retRaw)
	}
	return bool(retBool), nil
}

func EvalString(p cel.Program, scope map[string]interface{}) (string, error) {
	retRaw, _, err := p.Eval(scope)
	if err != nil {
		return "", fmt.Errorf("eval error: %v", err)
	}
	retString, ok := retRaw.(types.String)
	if !ok {
		return "", fmt.Errorf("result not String: ret=%+v, type=%T", retRaw, retRaw)
	}
	return string(retString), nil
}
