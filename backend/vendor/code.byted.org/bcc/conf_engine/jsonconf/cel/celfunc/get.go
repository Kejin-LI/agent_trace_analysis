package celfunc

import (
	"fmt"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	"code.byted.org/ttarch/byteconf-cel-go/checker/decls"
	"code.byted.org/ttarch/byteconf-cel-go/common/types"
	"code.byted.org/ttarch/byteconf-cel-go/common/types/ref"
	"code.byted.org/ttarch/byteconf-cel-go/common/types/traits"
	"code.byted.org/ttarch/byteconf-cel-go/interpreter/functions"
)

func MultiGetFunc(argNumber int) *functions.Overload {
	return &functions.Overload{
		Operator: fmt.Sprintf("get_%d", argNumber),
		Function: func(args ...ref.Val) ref.Val {
			if len(args) < 3 {
				return types.NewErr("invalid arguments to 'get'")
			}
			defVal := args[len(args)-1]

			var attrs traits.Mapper
			entry := args[0]
			var ok bool

			for i := 1; i < len(args)-1; i++ {
				attrs, ok = entry.(traits.Mapper)
				if !ok {
					return types.NewErr(
						"invalid operand of type '%v' to obj.get(key, def)",
						args[i-1].Type())
				}
				key, ok := args[i].(types.String)
				if !ok {
					return types.NewErr(
						"invalid key of type '%v' to obj.multi_get(key..., def), keyIdx=%d",
						args[i].Type(), i)
				}
				if attrs.Contains(key) != types.True {
					return defVal
				}
				entry = attrs.Get(key)
			}
			if entry.Type() != defVal.Type() {
				return defVal
			}
			return entry
		},
	}
}

func MultiGetFuncDecl(argNumber int) *exprpb.Decl {
	args := make([]*exprpb.Type, 0)
	args = append(args, MapStrDyn)
	for i := 0; i < argNumber; i++ {
		args = append(args, decls.String)
	}
	args = append(args, decls.Dyn)
	return decls.NewFunction(
		fmt.Sprintf("get_%v", argNumber),
		decls.NewInstanceOverload(
			fmt.Sprintf("get_map_%v", argNumber),
			args,
			decls.Dyn))
}
