package expr

import "fmt"

func Run(list []*Conf, scope map[string]interface{}, pathIndex *[]int) (hit bool, res string, err error) {
	globalVar := GetGlobalVar(scope)
	for i := len(list) - 1; i >= 0; i-- {
		*pathIndex = append(*pathIndex, i)
		hit, res, err = list[i].run(globalVar, pathIndex)
		if err != nil {
			err = fmt.Errorf("ch[%v]. txt:%v Run error: %v", i, list[i].Cond.Txt, err)
			return
		}
		if hit {
			return
		}
		*pathIndex = (*pathIndex)[:len(*pathIndex)-1]

	}
	return false, "", nil
}
func GetEnvFunc() map[string]interface{} {
	return funcEnv
}
func GetGlobalVar(scope map[string]interface{}) map[string]interface{} {
	for k, v := range funcEnv {
		scope[k] = v
	}
	return scope
}
