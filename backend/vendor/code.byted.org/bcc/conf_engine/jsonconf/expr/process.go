package expr

import (
	"fmt"

	"github.com/antonmedv/expr"
)

func Compile(list []*Conf) error {
	options := []expr.Option{
		expr.Env(funcEnv),
		expr.AllowUndefinedVariables(),
	}
	for i := len(list) - 1; i >= 0; i-- {
		if err := list[i].compile(options); err != nil {
			return fmt.Errorf("ch[%v] Compile error: %v", i, err)
		}
	}
	return nil
}
