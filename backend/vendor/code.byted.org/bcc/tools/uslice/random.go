package uslice

import (
	"math/rand"
	"reflect"

	"code.byted.org/gopkg/logs"
)

//切片随机（基本类型+任意类型的切片）
func Random(any interface{}) {
	switch a := any.(type) {
	case []string:
		for i := 0; i < len(a); i++ {
			pos := rand.Intn(len(a))
			a[i], a[pos] = a[pos], a[i]
		}
	case []int:
		for i := 0; i < len(a); i++ {
			pos := rand.Intn(len(a))
			a[i], a[pos] = a[pos], a[i]
		}
	case []int64:
		for i := 0; i < len(a); i++ {
			pos := rand.Intn(len(a))
			a[i], a[pos] = a[pos], a[i]
		}
	case []int32:
		for i := 0; i < len(a); i++ {
			pos := rand.Intn(len(a))
			a[i], a[pos] = a[pos], a[i]
		}
	case []int16:
		for i := 0; i < len(a); i++ {
			pos := rand.Intn(len(a))
			a[i], a[pos] = a[pos], a[i]
		}
	case []int8:
		for i := 0; i < len(a); i++ {
			pos := rand.Intn(len(a))
			a[i], a[pos] = a[pos], a[i]
		}
	case []uint:
		for i := 0; i < len(a); i++ {
			pos := rand.Intn(len(a))
			a[i], a[pos] = a[pos], a[i]
		}
	case []uint64:
		for i := 0; i < len(a); i++ {
			pos := rand.Intn(len(a))
			a[i], a[pos] = a[pos], a[i]
		}
	case []uint32:
		for i := 0; i < len(a); i++ {
			pos := rand.Intn(len(a))
			a[i], a[pos] = a[pos], a[i]
		}
	case []uint16:
		for i := 0; i < len(a); i++ {
			pos := rand.Intn(len(a))
			a[i], a[pos] = a[pos], a[i]
		}
	case []uint8:
		for i := 0; i < len(a); i++ {
			pos := rand.Intn(len(a))
			a[i], a[pos] = a[pos], a[i]
		}
	case []float64:
		for i := 0; i < len(a); i++ {
			pos := rand.Intn(len(a))
			a[i], a[pos] = a[pos], a[i]
		}
	case []float32:
		for i := 0; i < len(a); i++ {
			pos := rand.Intn(len(a))
			a[i], a[pos] = a[pos], a[i]
		}
	case []bool:
		for i := 0; i < len(a); i++ {
			pos := rand.Intn(len(a))
			a[i], a[pos] = a[pos], a[i]
		}
	case []interface{}:
		for i := 0; i < len(a); i++ {
			pos := rand.Intn(len(a))
			a[i], a[pos] = a[pos], a[i]
		}
	default:
		valueof := reflect.ValueOf(any)
		if valueof.Kind() == reflect.Slice {
			//虽然通用，但性能差
			fn := reflect.Swapper(any)
			size := valueof.Len()
			for i := 0; i < size; i++ {
				pos := rand.Intn(size)
				fn(i, pos)
			}
		} else {
			logs.Error("SliceRandom invalid type=%v any=%v", reflect.TypeOf(any).Name(), any)
		}
	}
}
