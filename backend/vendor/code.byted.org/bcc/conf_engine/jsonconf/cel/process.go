package cel

import (
	"fmt"
)

func CListCompileWithDefaultEnv(cList []Conf) error {
	env, err := EnvInit()
	if err != nil {
		return fmt.Errorf("env init error: %v", err)
	}
	if err := InitProgram(cList, env); err != nil {
		return fmt.Errorf("init program error: %v", err)
	}
	return nil
}

// CListMarshalWithDefaultEnv 编译并序列化
func CListMarshalWithDefaultEnv(cList []*Conf) error {
	env, err := EnvInit()
	if err != nil {
		return fmt.Errorf("env init error: %v", err)
	}
	if err := InitMarshal(cList, env); err != nil {
		return fmt.Errorf("initMarshal error: %v", err)
	}
	return nil
}
