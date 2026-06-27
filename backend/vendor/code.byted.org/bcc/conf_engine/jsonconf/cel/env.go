package cel

import (
	"fmt"

	"code.byted.org/bcc/conf_engine/jsonconf/cel/celfunc"

	"code.byted.org/ttarch/byteconf-cel-go/cel"
	"code.byted.org/ttarch/byteconf-cel-go/checker/decls"
)

const GlobalVarPrefix = "global_var."

const LocalVarPrefix = "local_var."

func EnvInit() (*cel.Env, error) {
	env, err := cel.NewEnv(
		cel.Declarations(
			decls.NewIdent("global_var", celfunc.MapStrDyn, nil),
			decls.NewIdent("local_var", celfunc.MapStrDyn, nil),
		),
		cel.Lib(celfunc.CustomFuncLib{}),
	)

	if err != nil {
		return nil, fmt.Errorf("new env error")
	}
	return env, nil
}
