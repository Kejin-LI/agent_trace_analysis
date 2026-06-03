package celfunc

import (
	"time"

	"code.byted.org/bcc/conf_engine/model"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	"code.byted.org/ttarch/byteconf-cel-go/checker/decls"
	"code.byted.org/ttarch/byteconf-cel-go/common/types"
	"code.byted.org/ttarch/byteconf-cel-go/common/types/ref"
	"code.byted.org/ttarch/byteconf-cel-go/interpreter/functions"
)

var CurUnixTimeFunc = &functions.Overload{
	Operator: string(model.CurrentUnixTime),
	Function: func(args ...ref.Val) ref.Val {
		if len(args) != 0 {
			return types.NewErr("invalid arguments to 'cur_unix_time'")
		}
		return types.Int(time.Now().Unix())
	},
}

func CurUnixTimeFuncDecl() *exprpb.Decl {
	return decls.NewFunction(
		string(model.CurrentUnixTime),
		decls.NewOverload(
			string(model.CurrentUnixTime),
			[]*exprpb.Type{},
			decls.Int))
}

var UnixTimeSinceFunc = &functions.Overload{
	Operator: string(model.UnixTimeSince),
	Unary: func(value ref.Val) ref.Val {
		t, ok := value.(types.Int)
		if !ok {
			return types.NewErr(
				"invalid operand of type '%v' to unix_time_since()",
				value.Type())
		}
		return types.Int(time.Now().Unix() - int64(t))
	},
}

func UnixTimeSinceFuncDecl() *exprpb.Decl {
	return decls.NewFunction(
		string(model.UnixTimeSince),
		decls.NewOverload(
			string(model.UnixTimeSince),
			[]*exprpb.Type{decls.Int},
			decls.Int))
}
