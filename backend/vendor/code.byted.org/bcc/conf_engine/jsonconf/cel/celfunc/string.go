package celfunc

import (
	"strconv"
	"strings"

	"code.byted.org/bcc/conf_engine/model"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	"code.byted.org/ttarch/byteconf-cel-go/checker/decls"
	"code.byted.org/ttarch/byteconf-cel-go/common/types"
	"code.byted.org/ttarch/byteconf-cel-go/common/types/ref"
	"code.byted.org/ttarch/byteconf-cel-go/interpreter/functions"
)

var StringToLowerFunc = &functions.Overload{
	Operator: string(model.StrToLower),
	Unary: func(value ref.Val) ref.Val {
		str, ok := value.(types.String)
		if !ok {
			return types.NewErr(
				"invalid operand of type '%v' to obj.str_to_lower()",
				value.Type())
		}
		lowerStr := strings.ToLower(string(str))
		return types.String(lowerStr)
	},
}

var StringToUpperFunc = &functions.Overload{
	Operator: string(model.StrToUpper),
	Unary: func(value ref.Val) ref.Val {
		str, ok := value.(types.String)
		if !ok {
			return types.NewErr(
				"invalid operand of type '%v' to obj.str_to_upper()",
				value.Type())
		}
		upperStr := strings.ToUpper(string(str))
		return types.String(upperStr)
	},
}

var ConvertNumberFunc = &functions.Overload{
	Operator: string(model.ConvertNumber),
	Unary: func(value ref.Val) ref.Val {
		str, ok := value.(types.String)
		if !ok {
			return types.NewErr(
				"invalid operand of type '%v' to obj.convert_number()",
				value.Type())
		}
		i, err := strconv.ParseInt(string(str), 10, 64)
		if err != nil {
			return types.IntZero
		}
		return types.Int(i)
	},
}

func StringToLowerFuncDecl() *exprpb.Decl {
	return decls.NewFunction(
		string(model.StrToLower),
		decls.NewInstanceOverload(
			string(model.StrToLower),
			[]*exprpb.Type{decls.String},
			decls.String))
}

func StringToUpperFuncDecl() *exprpb.Decl {
	return decls.NewFunction(
		string(model.StrToUpper),
		decls.NewInstanceOverload(
			string(model.StrToUpper),
			[]*exprpb.Type{decls.String},
			decls.String))
}

func ConvertNumberFuncDecl() *exprpb.Decl {
	return decls.NewFunction(
		string(model.ConvertNumber),
		decls.NewInstanceOverload(
			string(model.ConvertNumber),
			[]*exprpb.Type{decls.String},
			decls.Int))
}

func GetSubstringFunc(hasEndArg bool) *functions.Overload {
	operatorName := getSubstringOperatorName(hasEndArg)
	substrFunc := func(args ...ref.Val) ref.Val {
		if hasEndArg && len(args) != 3 || !hasEndArg && len(args) != 2 {
			return types.NewErr("invalid arguments to '%s'", operatorName)
		}

		str, ok := args[0].(types.String)
		if !ok {
			return types.NewErr(
				"invalid operand of type '%v' to str.%s(begin, end) str",
				operatorName,
				args[0].Type())
		}
		s := string(str)

		begin, ok := args[1].(types.Int)
		if !ok {
			return types.NewErr(
				"invalid operand of type '%v' to str.%s(begin, end) begin",
				operatorName,
				args[1].Type())
		}
		beginOffset := int(begin)

		var endPtr *int
		if hasEndArg {
			end, ok := args[2].(types.Int)
			if !ok {
				return types.NewErr(
					"invalid operand of type '%v' to str.%s(begin, end) end",
					operatorName,
					args[2].Type())
			}
			endOffset := int(end)
			endPtr = &endOffset
		}
		return types.String(substring(s, beginOffset, endPtr))
	}
	functionFunc := func(args ...ref.Val) ref.Val {
		return substrFunc(args...)
	}
	binaryFunc := func(lhs ref.Val, rhs ref.Val) ref.Val {
		return substrFunc(lhs, rhs)
	}
	overload := &functions.Overload{
		Operator: operatorName,
	}
	if hasEndArg {
		overload.Function = functionFunc
	} else {
		overload.Binary = binaryFunc
	}
	return overload
}

func getSubstringOperatorName(hasEndArg bool) string {
	operatorName := "substring_end"
	if !hasEndArg {
		operatorName = "substring_no_end"
	}
	return operatorName
}

func GetSubstringNoEndFuncDecl(hasEndArg bool) *exprpb.Decl {
	operatorName := getSubstringOperatorName(hasEndArg)
	typeDef := []*exprpb.Type{decls.String, decls.Int}
	if hasEndArg {
		typeDef = append(typeDef, decls.Int)
	}
	return decls.NewFunction(
		operatorName,
		decls.NewInstanceOverload(
			operatorName,
			typeDef,
			decls.String))
}

func substring(s string, begin int, endPtr *int) string {
	strlen := len(s)
	var end int
	if endPtr == nil {
		end = strlen
	} else {
		end = *endPtr
		if end < 0 {
			end += strlen
		}
	}
	if begin < 0 {
		begin += strlen
		if begin < 0 {
			return ""
		}
	}
	if begin >= strlen {
		return ""
	}
	if end > strlen {
		end = strlen
	}
	if begin > end {
		return ""
	}
	return s[begin:end]
}
