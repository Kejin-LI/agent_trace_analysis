package mask_pack

import (
	"fmt"
	"reflect"
	"time"
)

var setValue = true

func SetValue(flag bool) {
	setValue = flag
}

// return a new interface slice
func MaskInterfaceSlice(v ...interface{}) []interface{} {
	var p []int
	for i := range v {
		if needMask(v[i]) {
			p = append(p, i)
		}
	}
	if len(p) != 0 {
		masked := make([]interface{}, len(v))
	b:
		for i := range v {
			for ii := range p {
				if i == p[ii] {
					masked[i] = maskInterfaceWithoutCheck(v[i])

					break b
				}
			}
			masked[i] = v[i]
		}
		return masked
	}
	return v
}

// may return a new interface
func MaskInterface(src interface{}) interface{} {
	if !needMask(src) {
		return src
	}
	return maskInterfaceWithoutCheck(src)
}

func maskInterfaceWithoutCheck(src interface{}) interface{} {
	var doSet bool
	original := reflect.ValueOf(src)
	// is nil
	cpy := reflect.New(original.Type()).Elem()
	// avoid lost unexported data
	if cpy.CanSet() && setValue {
		// handle unexported may case change origin value
		// pointer need to be changed
		cpy.Set(original)
		doSet = true
	}
	// add data
	maskRecursive(original, cpy, doSet == false)
	return cpy.Interface()
}

func MaskSprintf(src interface{}) string {
	var dst interface{}
	if needMask(src) {
		dst = MaskInterface(src)
	} else {
		dst = src
	}
	return fmt.Sprintf("%v", dst)
}

func needMask(src interface{}) bool {
	if src == nil {
		return false
	}
	switch src.(type) {
	case string, float64, float32, bool, []byte, time.Time, *time.Time,
		int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return false
	}
	original := reflect.ValueOf(src)
	return needMaskRecursive(original)
}

func needMaskRecursive(src reflect.Value) bool {
	switch src.Kind() {
	case reflect.Ptr:
		originalValue := src.Elem()
		if !originalValue.IsValid() {
			return false
		}
		return needMaskRecursive(originalValue)

	case reflect.Interface:
		if src.IsNil() {
			return false
		}
		originalValue := src.Elem()
		return needMaskRecursive(originalValue)

	case reflect.Struct:
		if _, ok := src.Interface().(time.Time); ok {
			return false
		}
		for i := 0; i < src.NumField(); i++ {
			if src.Type().Field(i).PkgPath != "" {
				continue
			}
			if tag := src.Type().Field(i).Tag.Get("mask"); tag != "" {
				return true
			}
			if needMaskRecursive(src.Field(i)) {
				return true
			}
		}

	case reflect.Slice:
		if src.IsNil() {
			return false
		}

		for i := 0; i < src.Len(); i++ {
			if needMaskRecursive(src.Index(i)) {
				return true
			}
		}

	case reflect.Map:
		if src.IsNil() {
			return false
		}

		for _, key := range src.MapKeys() {
			originalValue := src.MapIndex(key)
			if needMaskRecursive(originalValue) {
				return true
			}
		}
	}

	return false
}

// todo: support unexported field
func maskRecursive(src, dst reflect.Value, forceSet bool) {
	switch src.Kind() {
	case reflect.Ptr:
		originalValue := src.Elem()
		if !originalValue.IsValid() {
			return
		}

		dst.Set(reflect.New(originalValue.Type()))
		// pointer must change
		maskRecursive(originalValue, dst.Elem(), forceSet)

	case reflect.Interface:
		if src.IsNil() {
			return
		}
		if needMaskRecursive(src) || forceSet {
			originalValue := src.Elem()
			copyValue := reflect.New(originalValue.Type()).Elem()
			maskRecursive(originalValue, copyValue, forceSet)
			dst.Set(copyValue)
		}

	case reflect.Struct:
		if t, ok := src.Interface().(time.Time); ok {
			if forceSet {
				dst.Set(reflect.ValueOf(t))
			}
			return
		}

		RecursiveTagMutex.RLock()
		if _, ok := RecursiveTag[src.Type()]; !ok {
			RecursiveTagMutex.RUnlock()
			RecursiveTagMutex.Lock()
			RecursiveTag[src.Type()] = make(map[string]string)
			// once
			if len(RecursiveTag[src.Type()]) == 0 {
				for i := 0; i < src.NumField(); i++ {
					// unexported field
					if src.Type().Field(i).PkgPath != "" {
						continue
					}
					if needMaskRecursive(src.Field(i)) {
						RecursiveTag[src.Type()][src.Type().Field(i).Name] = "nested_struct"
						continue
					}
					if tag := src.Type().Field(i).Tag.Get("mask"); tag != "" {
						RecursiveTag[src.Type()][src.Type().Field(i).Name] = tag
					}
				}
			}
			RecursiveTagMutex.Unlock()
		}

		RecursiveTagMutex.RLock()
		if len(RecursiveTag[src.Type()]) == 0 && forceSet == false {
			RecursiveTagMutex.RUnlock()
			return
		}
		RecursiveTagMutex.RUnlock()

		for i := 0; i < src.NumField(); i++ {
			RecursiveTagMutex.RLock()
			tag, ok := RecursiveTag[src.Type()][src.Type().Field(i).Name]
			RecursiveTagMutex.RUnlock()
			if !ok {
				if forceSet {
					maskRecursive(src.Field(i), dst.Field(i), forceSet)
				}
				continue
			}

			// 直接 tag
			if (src.Field(i).Kind() == reflect.String || src.Field(i).Kind() == reflect.Slice ||
				src.Field(i).Kind() == reflect.Ptr) && tag != "nested_struct" {
				switch src.Field(i).Kind() {
				case reflect.String:
					res := maskStringByTag(tag, src.Field(i).String())
					dst.Field(i).SetString(res)
					continue
				case reflect.Ptr:
					// 只处理 string
					if reflect.Indirect(src.Field(i)).Kind() == reflect.String {
						res := maskStringByTag(tag, src.Field(i).Elem().String())
						dst.Field(i).Set(reflect.ValueOf(&res))
						continue
					}
				case reflect.Slice:
					// handle slice of strings or string pointers
					if src.Field(i).Len() == 0 {
						continue
					}
					sliceItemKind := src.Field(i).Index(0).Kind()
					if sliceItemKind == reflect.String {
						dstItems := make([]string, src.Field(i).Len())
						for j := 0; j < src.Field(i).Len(); j++ {
							dstItems[j] = maskStringByTag(tag, src.Field(i).Index(j).String())
						}
						dst.Field(i).Set(reflect.ValueOf(dstItems))
						continue
					}

					if sliceItemKind == reflect.Ptr && reflect.Indirect(src.Field(i).Index(0)).Kind() == reflect.String {
						dstItems := make([]*string, src.Field(i).Len())
						for j := 0; j < src.Field(i).Len(); j++ {
							res := maskStringByTag(tag, src.Field(i).Index(j).Elem().String())
							dstItems[j] = &res
						}
						dst.Field(i).Set(reflect.ValueOf(dstItems))
					}

					continue
				}
			}

			if tag == "nested_struct" {
				maskRecursive(src.Field(i), dst.Field(i), forceSet)
				continue
			}

			// 针对嵌套的 tag
			maskRecursive(src.Field(i), dst.Field(i), forceSet)
		}

	case reflect.Slice:
		if src.IsNil() {
			return
		}
		dst.Set(reflect.MakeSlice(src.Type(), src.Len(), src.Cap()))

		for i := 0; i < src.Len(); i++ {
			maskRecursive(src.Index(i), dst.Index(i), forceSet)
		}

	case reflect.Map:
		if src.IsNil() {
			return
		}
		dst.Set(reflect.MakeMap(src.Type()))

		for _, key := range src.MapKeys() {
			originalValue := src.MapIndex(key)
			var copyValue reflect.Value
			// ignore key is struct, or update key
			copyKey := MaskInterface(key.Interface())
			if needMaskRecursive(originalValue) || forceSet {
				copyValue = reflect.New(originalValue.Type()).Elem()
				maskRecursive(originalValue, copyValue, forceSet)
				dst.SetMapIndex(reflect.ValueOf(copyKey), copyValue)
			}
		}
		return

	default:
		if forceSet {
			dst.Set(src)
		}
		return
	}
}

//func getUnexportedField(field reflect.Value) interface{} {
//	return reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Interface()
//}
//
//func setUnexportedField(field reflect.Value, value interface{}) {
//	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).
//		Elem().
//		Set(reflect.ValueOf(value))
//}
