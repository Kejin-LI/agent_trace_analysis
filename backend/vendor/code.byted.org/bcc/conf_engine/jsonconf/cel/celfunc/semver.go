package celfunc

import (
	"code.byted.org/bcc/conf_engine/model"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	"code.byted.org/ttarch/byteconf-cel-go/checker/decls"
	"code.byted.org/ttarch/byteconf-cel-go/common/types"
	"code.byted.org/ttarch/byteconf-cel-go/common/types/ref"
	"code.byted.org/ttarch/byteconf-cel-go/interpreter/functions"

	"github.com/blang/semver/v4"
)

var SemverCmpFunc = &functions.Overload{
	Operator: string(model.SemverCmp),
	Binary: func(base ref.Val, cmp ref.Val) ref.Val {
		left, leftSuc := base.(types.String)
		right, rightSuc := cmp.(types.String)
		if !leftSuc || !rightSuc {
			return types.NewErr(
				"invalid operand of type semver_cmp(%v, %v)", base.Type(), cmp.Type())
		}

		lv, leftErr := semver.Make(string(left))
		rv, rightErr := semver.Make(string(right))
		if leftErr != nil || rightErr != nil {
			return types.NewErr(
				"invalid value semver_cmp(%v, %v)", left, right)
		}

		return types.Int(lv.Compare(rv))
	},
}

func SemverCmpFuncDecl() *exprpb.Decl {
	return decls.NewFunction(
		string(model.SemverCmp),
		decls.NewInstanceOverload(
			"semver_cmp",
			[]*exprpb.Type{decls.String, decls.String},
			decls.Int))
}

var SemverRangeFunc = &functions.Overload{
	Operator: string(model.SemverRange),
	Binary: func(base, verRange ref.Val) ref.Val {
		left, leftSuc := base.(types.String)
		right, rightSuc := verRange.(types.String)
		if !leftSuc || !rightSuc {
			return types.NewErr(
				"invalid operand of type semver_range(%v, %v)", base.Type(), verRange.Type())
		}

		lv, err := semver.Parse(string(left))
		if err != nil {
			return types.NewErr("invalid value semver_range(%v, %v)", left, right)
		}

		expectedRange, err := semver.ParseRange(string(right))
		if err != nil {
			return types.NewErr("invalid value semver_range(%v, %v)", left, right)
		}

		return types.Bool(expectedRange(lv))
	},
}

func SemverRangeFuncDecl() *exprpb.Decl {
	return decls.NewFunction(
		string(model.SemverRange),
		decls.NewInstanceOverload(
			string(model.SemverRange),
			[]*exprpb.Type{decls.String, decls.String},
			decls.Bool))
}
