package celfunc

import (
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	"code.byted.org/ttarch/byteconf-cel-go/cel"
	"code.byted.org/ttarch/byteconf-cel-go/checker/decls"
)

var MapStrDyn *exprpb.Type

type CustomFuncLib struct{}

func init() {
	MapStrDyn = decls.NewMapType(decls.String, decls.Dyn)
}

func (CustomFuncLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Declarations(
			MultiGetFuncDecl(1),
			MultiGetFuncDecl(2),
			MultiGetFuncDecl(3),
			MultiGetFuncDecl(4),
			StringToLowerFuncDecl(),
			CurUnixTimeFuncDecl(),
			StringToUpperFuncDecl(),
			ConvertNumberFuncDecl(),
			UnixTimeSinceFuncDecl(),
			GetSubstringNoEndFuncDecl(true),
			GetSubstringNoEndFuncDecl(false),
			SemverCmpFuncDecl(),
			SemverRangeFuncDecl(),
		),
	}
}

func (CustomFuncLib) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{
		cel.Functions(
			MultiGetFunc(1),
			MultiGetFunc(2),
			MultiGetFunc(3),
			MultiGetFunc(4),
			StringToLowerFunc,
			CurUnixTimeFunc,
			StringToUpperFunc,
			ConvertNumberFunc,
			UnixTimeSinceFunc,
			GetSubstringFunc(true),
			GetSubstringFunc(false),
			SemverCmpFunc,
			SemverRangeFunc,
		),
	}
}
