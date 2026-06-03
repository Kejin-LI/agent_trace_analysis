package uslice

import (
	"reflect"

	"code.byted.org/gopkg/logs"
)

//任意类型，性能低
func Contain(any interface{}, item interface{}) bool {
	switch a := any.(type) {
	case []string:
		if it, ok := item.(string); !ok {
			logs.Error("uslice Contain invalid item kind=%v", reflect.TypeOf(item).Kind())
			return false
		} else {
			return ContainString(a, it)
		}
	case []int:
		if it, ok := item.(int); !ok {
			logs.Error("uslice Contain invalid item kind=%v", reflect.TypeOf(item).Kind())
			return false
		} else {
			return ContainInt(a, it)
		}
	case []int64:
		if it, ok := item.(int64); !ok {
			logs.Error("uslice Contain invalid item kind=%v", reflect.TypeOf(item).Kind())
			return false
		} else {
			return ContainInt64(a, it)
		}
	case []int32:
		if it, ok := item.(int32); !ok {
			logs.Error("uslice Contain invalid item kind=%v", reflect.TypeOf(item).Kind())
			return false
		} else {
			return ContainInt32(a, it)
		}
	case []int16:
		if it, ok := item.(int16); !ok {
			logs.Error("uslice Contain invalid item kind=%v", reflect.TypeOf(item).Kind())
			return false
		} else {
			return ContainInt16(a, it)
		}
	case []int8:
		if it, ok := item.(int8); !ok {
			logs.Error("uslice Contain invalid item kind=%v", reflect.TypeOf(item).Kind())
			return false
		} else {
			return ContainInt8(a, it)
		}
	case []uint:
		if it, ok := item.(uint); !ok {
			logs.Error("uslice Contain invalid item kind=%v", reflect.TypeOf(item).Kind())
			return false
		} else {
			return ContainUint(a, it)
		}
	case []uint64:
		if it, ok := item.(uint64); !ok {
			logs.Error("uslice Contain invalid item kind=%v", reflect.TypeOf(item).Kind())
			return false
		} else {
			return ContainUint64(a, it)
		}
	case []uint32:
		if it, ok := item.(uint32); !ok {
			logs.Error("uslice Contain invalid item kind=%v", reflect.TypeOf(item).Kind())
			return false
		} else {
			return ContainUint32(a, it)
		}
	case []uint16:
		if it, ok := item.(uint16); !ok {
			logs.Error("uslice Contain invalid item kind=%v", reflect.TypeOf(item).Kind())
			return false
		} else {
			return ContainUint16(a, it)
		}
	case []uint8:
		if it, ok := item.(uint8); !ok {
			logs.Error("uslice Contain invalid item kind=%v", reflect.TypeOf(item).Kind())
			return false
		} else {
			return ContainUint8(a, it)
		}
	case []float64:
		if it, ok := item.(float64); !ok {
			logs.Error("uslice Contain invalid item kind=%v", reflect.TypeOf(item).Kind())
			return false
		} else {
			return ContainFloat64(a, it)
		}
	case []float32:
		if it, ok := item.(float32); !ok {
			logs.Error("uslice Contain invalid item kind=%v", reflect.TypeOf(item).Kind())
			return false
		} else {
			return ContainFloat32(a, it)
		}
	case []bool:
		if it, ok := item.(bool); !ok {
			logs.Error("uslice Contain invalid item kind=%v", reflect.TypeOf(item).Kind())
			return false
		} else {
			return ContainBool(a, it)
		}
	default:
		typeOf := reflect.TypeOf(any)
		if typeOf.Kind() != reflect.Slice {
			logs.Error("uslice Contain invalid a kind=%v", typeOf.Kind())
			return false
		}
		valueOf := reflect.ValueOf(any)
		for i := 0; i < valueOf.Len(); i++ {
			if valueOf.Index(i).Interface() == item {
				return true
			}
		}
		return false
	}
}

func ContainString(arr []string, item string) bool {
	for _, v := range arr {
		if v == item {
			return true
		}
	}
	return false
}

func ContainInt(arr []int, item int) bool {
	for _, v := range arr {
		if v == item {
			return true
		}
	}
	return false
}

func ContainInt64(arr []int64, item int64) bool {
	for _, v := range arr {
		if v == item {
			return true
		}
	}
	return false
}

func ContainInt32(arr []int32, item int32) bool {
	for _, v := range arr {
		if v == item {
			return true
		}
	}
	return false
}

func ContainInt16(arr []int16, item int16) bool {
	for _, v := range arr {
		if v == item {
			return true
		}
	}
	return false
}

func ContainInt8(arr []int8, item int8) bool {
	for _, v := range arr {
		if v == item {
			return true
		}
	}
	return false
}

func ContainUint(arr []uint, item uint) bool {
	for _, v := range arr {
		if v == item {
			return true
		}
	}
	return false
}

func ContainUint64(arr []uint64, item uint64) bool {
	for _, v := range arr {
		if v == item {
			return true
		}
	}
	return false
}

func ContainUint32(arr []uint32, item uint32) bool {
	for _, v := range arr {
		if v == item {
			return true
		}
	}
	return false
}

func ContainUint16(arr []uint16, item uint16) bool {
	for _, v := range arr {
		if v == item {
			return true
		}
	}
	return false
}

func ContainUint8(arr []uint8, item uint8) bool {
	for _, v := range arr {
		if v == item {
			return true
		}
	}
	return false
}

func ContainFloat64(arr []float64, item float64) bool {
	for _, v := range arr {
		if v == item {
			return true
		}
	}
	return false
}

func ContainFloat32(arr []float32, item float32) bool {
	for _, v := range arr {
		if v == item {
			return true
		}
	}
	return false
}

func ContainBool(arr []bool, item bool) bool {
	for _, v := range arr {
		if v == item {
			return true
		}
	}
	return false
}

//判断arr是否包含subArr的所有元素
func ContainStringSlice(arr []string, subArr []string) bool {
	mm := make(map[string]bool, len(arr))

	for _, item := range arr {
		mm[item] = true
	}

	for _, item := range subArr {
		if !mm[item] {
			return false
		}
	}

	return true
}
