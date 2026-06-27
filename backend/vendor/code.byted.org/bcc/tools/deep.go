package tools

import (
	"reflect"

	"code.byted.org/bcc/tools/udeep"
)

//深度复制（x尽量用指针）//比json快4～10倍
func DeepCopy(x interface{}) interface{} {
	return udeep.DeepCopy(x)
}

//深度比较
func DeepEqual(x, y interface{}) bool {
	return reflect.DeepEqual(x, y)
}

//获取所有指针（性能底）
func DeepPoint(x interface{}) map[uintptr]string {
	return udeep.DeepPoint(x)
}

//是否共享数据（性能底）
func DeepShare(x, y interface{}) [][2]string {
	return udeep.DeepShare(x, y)
}
