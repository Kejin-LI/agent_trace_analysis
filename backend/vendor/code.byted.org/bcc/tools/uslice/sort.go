package uslice

import (
	"reflect"
	"sort"

	"code.byted.org/gopkg/logs"
)

//切片升序排序（基本类型但不包含bool）
func Sort(any interface{}) {
	switch a := any.(type) {
	case []string:
		sort.Strings(a)
	case []int:
		sort.Ints(a)
	case []int64:
		sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
	case []int32:
		sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
	case []int16:
		sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
	case []int8:
		sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
	case []uint:
		sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
	case []uint64:
		sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
	case []uint32:
		sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
	case []uint16:
		sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
	case []uint8:
		sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
	case []float64:
		sort.Float64s(a)
	case []float32:
		sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
	case sort.Interface:
		sort.Sort(a)
	default:
		logs.Error("slice sort invalid type=%v any=%v", reflect.TypeOf(any).Name(), any)
	}
}

//切片降序排序（基本类型但不包含bool）
func SortDesc(any interface{}) {
	switch a := any.(type) {
	case []string:
		sort.Sort(sort.Reverse(sort.StringSlice(a)))
	case []int:
		sort.Sort(sort.Reverse(sort.IntSlice(a)))
	case []int64:
		sort.Slice(a, func(i, j int) bool { return a[i] > a[j] })
	case []int32:
		sort.Slice(a, func(i, j int) bool { return a[i] > a[j] })
	case []int16:
		sort.Slice(a, func(i, j int) bool { return a[i] > a[j] })
	case []int8:
		sort.Slice(a, func(i, j int) bool { return a[i] > a[j] })
	case []uint:
		sort.Slice(a, func(i, j int) bool { return a[i] > a[j] })
	case []uint64:
		sort.Slice(a, func(i, j int) bool { return a[i] > a[j] })
	case []uint32:
		sort.Slice(a, func(i, j int) bool { return a[i] > a[j] })
	case []uint16:
		sort.Slice(a, func(i, j int) bool { return a[i] > a[j] })
	case []uint8:
		sort.Slice(a, func(i, j int) bool { return a[i] > a[j] })
	case []float64:
		sort.Sort(sort.Reverse(sort.Float64Slice(a)))
	case []float32:
		sort.Slice(a, func(i, j int) bool { return a[i] > a[j] })
	case sort.Interface:
		sort.Sort(sort.Reverse(a))
	default:
		logs.Error("slice sort invalid type=%v any=%v", reflect.TypeOf(any).Name(), any)
	}
}
