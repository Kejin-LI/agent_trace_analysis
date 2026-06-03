package udeep

import (
	"reflect"
	"strconv"
)

//是否共享数据
func DeepShare(x, y interface{}) [][2]string {
	mark0 := DeepPoint(x)
	mark1 := DeepPoint(y)

	uniq := make(map[[2]string]bool, len(mark0))
	r := [][2]string{}
	for k0, v0 := range mark0 {
		if v1, ok := mark1[k0]; ok {
			key := [2]string{v0, v1}
			if !uniq[key] {
				r = append(r, key)
				uniq[key] = true
			}
		}
	}
	for k1, v1 := range mark1 {
		if v0, ok := mark0[k1]; ok {
			key := [2]string{v0, v1}
			if !uniq[key] {
				r = append(r, key)
				uniq[key] = true
			}
		}
	}
	return r
}

//获取所有指针
func DeepPoint(val interface{}) map[uintptr]string {
	r := make(map[uintptr]string)
	deepPoint(val, r, "R")
	return r
}

func deepPoint(val interface{}, marks map[uintptr]string, prefix string) {
	if val == nil {
		return
	}
	vof := reflect.ValueOf(val)
	loopPoint(vof, marks, prefix)
}

//性能低，因为有字符串组装
func loopPoint(vof reflect.Value, marks map[uintptr]string, prefix string) {
	//logs.Info("loopPoint prefix=%v", prefix)
	tof := vof.Type()

	switch vof.Kind() {
	case reflect.Ptr:
		if vof.IsNil() {
			return
		}
		marks[vof.Pointer()] = prefix
		loopPoint(vof.Elem(), marks, prefix)
	case reflect.Interface:
		if vof.IsNil() {
			return
		}
		loopPoint(vof.Elem(), marks, prefix)
	case reflect.Struct:
		for i := 0; i < vof.NumField(); i++ {
			sf := vof.Type().Field(i)
			if sf.PkgPath != "" {
				continue
			}
			if needLoop(sf.Type.Kind()) {
				loopPoint(vof.Field(i), marks, prefix+"."+sf.Name)
			}
		}
	case reflect.Array:
		if needLoop(tof.Elem().Kind()) {
			for i := 0; i < vof.Len(); i++ {
				loopPoint(vof.Index(i), marks, prefix+"["+strconv.Itoa(i)+"]")
			}
		}
	case reflect.Slice:
		if vof.IsNil() {
			return
		}
		marks[vof.Pointer()] = prefix //如果两个slice指向同一个底层数组的不同未知，就无法判断
		if needLoop(tof.Elem().Kind()) {
			for i := 0; i < vof.Len(); i++ {
				loopPoint(vof.Index(i), marks, prefix+"["+strconv.Itoa(i)+"]")
			}
		}
	case reflect.Map:
		if vof.IsNil() {
			return
		}
		marks[vof.Pointer()] = prefix
		if needLoop(tof.Key().Kind()) {
			for _, key := range vof.MapKeys() {
				loopPoint(key, marks, prefix+"["+key.Type().Name()+"]")
			}
		}
		if needLoop(tof.Elem().Kind()) {
			for _, key := range vof.MapKeys() {
				value := vof.MapIndex(key)
				loopPoint(value, marks, prefix+"["+value.Type().Name()+"VAL]")
			}
		}
	case reflect.Uintptr:
		if real := vof.Interface().(uintptr); real != 0 {
			marks[real] = prefix
		}
	case reflect.UnsafePointer, reflect.Chan, reflect.Func:
		if vof.IsNil() {
			return
		}
		marks[vof.Pointer()] = prefix
	default:
	}
}

func needLoop(kind reflect.Kind) bool {
	switch kind {
	case reflect.Ptr, reflect.Interface, reflect.Struct, reflect.Array, reflect.Slice, reflect.Map,
		reflect.Uintptr, reflect.UnsafePointer, reflect.Chan, reflect.Func:
		return true
	default:
		return false
	}
}
