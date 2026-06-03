package uslice

import (
	"reflect"
)

//切片是否相等（任意类型）
func Equal(a0, a1 interface{}) bool {
	if reflect.TypeOf(a0) != reflect.TypeOf(a1) {
		//logs.Error("Equal diff type a0=%v a1=%v", reflect.TypeOf(a0).Kind(), reflect.TypeOf(a1).Kind())
		return false
	}
	switch a0.(type) {
	case []string:
		return EqualStrings(a0.([]string), a1.([]string))
	case []int:
		return EqualInts(a0.([]int), a1.([]int))
	case []int64:
		return EqualInt64s(a0.([]int64), a1.([]int64))
	case []int32:
		return EqualInt32s(a0.([]int32), a1.([]int32))
	case []int16:
		return EqualInt16s(a0.([]int16), a1.([]int16))
	case []int8:
		return EqualInt8s(a0.([]int8), a1.([]int8))
	case []uint:
		return EqualUints(a0.([]uint), a1.([]uint))
	case []uint64:
		return EqualUint64s(a0.([]uint64), a1.([]uint64))
	case []uint32:
		return EqualUint32s(a0.([]uint32), a1.([]uint32))
	case []uint16:
		return EqualUint16s(a0.([]uint16), a1.([]uint16))
	case []uint8:
		return EqualUint8s(a0.([]uint8), a1.([]uint8))
	case []float64:
		return EqualFloat64s(a0.([]float64), a1.([]float64))
	case []float32:
		return EqualFloat32s(a0.([]float32), a1.([]float32))
	case []bool:
		return EqualBools(a0.([]bool), a1.([]bool))
	case []interface{}:
		return EqualInterfaces(a0.([]interface{}), a1.([]interface{}))
	default:
		return reflect.DeepEqual(a0, a1)
	}
}

func EqualStrings(arr0, arr1 []string) bool {
	if len(arr0) != len(arr1) {
		return false
	}
	for i := 0; i < len(arr0); i++ {
		if arr0[i] != arr1[i] {
			return false
		}
	}
	return true
}

func EqualInts(arr0, arr1 []int) bool {
	if len(arr0) != len(arr1) {
		return false
	}
	for i := 0; i < len(arr0); i++ {
		if arr0[i] != arr1[i] {
			return false
		}
	}
	return true
}

func EqualInt64s(arr0, arr1 []int64) bool {
	if len(arr0) != len(arr1) {
		return false
	}
	for i := 0; i < len(arr0); i++ {
		if arr0[i] != arr1[i] {
			return false
		}
	}
	return true
}

func EqualInt32s(arr0, arr1 []int32) bool {
	if len(arr0) != len(arr1) {
		return false
	}
	for i := 0; i < len(arr0); i++ {
		if arr0[i] != arr1[i] {
			return false
		}
	}
	return true
}

func EqualInt16s(arr0, arr1 []int16) bool {
	if len(arr0) != len(arr1) {
		return false
	}
	for i := 0; i < len(arr0); i++ {
		if arr0[i] != arr1[i] {
			return false
		}
	}
	return true
}

func EqualInt8s(arr0, arr1 []int8) bool {
	if len(arr0) != len(arr1) {
		return false
	}
	for i := 0; i < len(arr0); i++ {
		if arr0[i] != arr1[i] {
			return false
		}
	}
	return true
}

func EqualUints(arr0, arr1 []uint) bool {
	if len(arr0) != len(arr1) {
		return false
	}
	for i := 0; i < len(arr0); i++ {
		if arr0[i] != arr1[i] {
			return false
		}
	}
	return true
}

func EqualUint64s(arr0, arr1 []uint64) bool {
	if len(arr0) != len(arr1) {
		return false
	}
	for i := 0; i < len(arr0); i++ {
		if arr0[i] != arr1[i] {
			return false
		}
	}
	return true
}

func EqualUint32s(arr0, arr1 []uint32) bool {
	if len(arr0) != len(arr1) {
		return false
	}
	for i := 0; i < len(arr0); i++ {
		if arr0[i] != arr1[i] {
			return false
		}
	}
	return true
}

func EqualUint16s(arr0, arr1 []uint16) bool {
	if len(arr0) != len(arr1) {
		return false
	}
	for i := 0; i < len(arr0); i++ {
		if arr0[i] != arr1[i] {
			return false
		}
	}
	return true
}

func EqualUint8s(arr0, arr1 []uint8) bool {
	if len(arr0) != len(arr1) {
		return false
	}
	for i := 0; i < len(arr0); i++ {
		if arr0[i] != arr1[i] {
			return false
		}
	}
	return true
}

func EqualFloat64s(arr0, arr1 []float64) bool {
	if len(arr0) != len(arr1) {
		return false
	}
	for i := 0; i < len(arr0); i++ {
		if arr0[i] != arr1[i] {
			return false
		}
	}
	return true
}

func EqualFloat32s(arr0, arr1 []float32) bool {
	if len(arr0) != len(arr1) {
		return false
	}
	for i := 0; i < len(arr0); i++ {
		if arr0[i] != arr1[i] {
			return false
		}
	}
	return true
}

func EqualBools(arr0, arr1 []bool) bool {
	if len(arr0) != len(arr1) {
		return false
	}
	for i := 0; i < len(arr0); i++ {
		if arr0[i] != arr1[i] {
			return false
		}
	}
	return true
}

func EqualInterfaces(arr0, arr1 []interface{}) bool {
	if len(arr0) != len(arr1) {
		return false
	}
	for i := 0; i < len(arr0); i++ {
		if arr0[i] != arr1[i] {
			return false
		}
	}
	return true
}
