package udeep

import (
	"reflect"
	"sync"
	"time"
	"unsafe"
)

// 输入的src尽量用指针
// 	struct 字段越多性能越高，通过memcpy优化，特殊字段会重新赋值
// 	slice和array 性能比较高，数值类型会使用memcpy优化
// 	map 性能低，尽量使用以下类型 key(int|string)  value(int|string|float64|bool)
// 		数值类型也有优化，key不能是结构体
// interface 支持
// 不支持类型: Uintptr UnsafePointer
// 私有变量不会复制；特例：time.Time
func DeepCopy(src interface{}) interface{} {
	if src == nil {
		return nil
	}
	srcValue := reflect.ValueOf(src)
	dstValue := reflect.New(srcValue.Type()).Elem()
	copyRecursive(srcValue, dstValue)
	return dstValue.Interface()
}

type CopyInterface interface {
	DeepCopy() interface{}
}

//------------------------------------------------------------------------------------
func copyRecursive(src, dst reflect.Value) {
	if src.CanInterface() {
		if diyer, ok := src.Interface().(CopyInterface); ok {
			dst.Set(reflect.ValueOf(diyer.DeepCopy()))
			return
		}
	}

	switch src.Kind() {
	case reflect.Ptr:
		srcValue := src.Elem()
		if !srcValue.IsValid() {
			return
		}
		dst.Set(reflect.New(srcValue.Type()))
		copyRecursive(srcValue, dst.Elem())

	case reflect.Interface:
		if src.IsNil() {
			return
		}
		srcValue := src.Elem()
		dstValue := reflect.New(srcValue.Type()).Elem()
		copyRecursive(srcValue, dstValue)
		dst.Set(dstValue)

	case reflect.Struct:
		t, ok := src.Interface().(time.Time)
		if ok {
			dst.Set(reflect.ValueOf(t))
			return
		}
		fn := copyStruct(src.Type(), false)
		fn(src, dst)

	case reflect.Slice:
		if src.IsNil() {
			return
		}
		fn := copySlice(src.Type(), false)
		fn(src, dst)

	case reflect.Map:
		if src.IsNil() {
			return
		}
		fn := copyMap(src.Type(), false)
		fn(src, dst)

	case reflect.Array:
		fn := copyArray(src.Type(), false)
		fn(src, dst)

	default:
		dst.Set(src)
	}
}

type structInfo struct {
	size     int          //
	items    []structItem //
	sections [][2]int     //分段复制
}

type structItem struct {
	index int
	fn    func(src reflect.Value, dst reflect.Value)
}

var g_structs = make(map[reflect.Type]*structInfo, 16)
var g_structLock sync.RWMutex

func getStruct(ty reflect.Type) *structInfo {
	g_structLock.RLock()
	stu := g_structs[ty]
	g_structLock.RUnlock()

	//类似c语言的占位，例如char占了4字节
	if stu == nil {
		stu = &structInfo{}
		stu.size = int(ty.Size())

		if stu.size > 0 { //防止空结构
			first := 0
			last := stu.size

			for i := 0; i < ty.NumField(); i++ {
				itemField := ty.Field(i)
				itemType := itemField.Type
				offset := int(itemField.Offset)
				//logs.Infof("  name=%v Offset=%v size=%v end=%v", itemField.Name, offset, int(itemType.Size()), offset+size)

				//私有变量 不确定判断条件
				if itemField.PkgPath != "" {
					if itemField.PkgPath == "time" { //time.Time
					} else {
						//logs.Warnf(fmt.Sprintf("private variate=%v.%v pkg=%v", ty.Name(), itemField.Name, itemField.PkgPath))
						if first != offset { //非连续私有成员
							stu.sections = append(stu.sections, [2]int{first, int(itemField.Offset)})
						}
						if i == ty.NumField()-1 {
							first = last
						} else {
							first = int(ty.Field(i + 1).Offset)
						}
					}
					continue
				}

				var fn func(src reflect.Value, dst reflect.Value)

				if canMemcopy(itemType) {
					//memcpy
				} else {
					//todo CopyInterface
					switch itemType.Kind() {
					case reflect.Array:
						fn = copyArray(itemType, true)
					case reflect.Slice:
						fn = copySlice(itemType, true)
					case reflect.Map:
						fn = copyMap(itemType, true)
					case reflect.Struct:
						fn = copyStruct(itemType, true)
					case reflect.Ptr, reflect.Interface:
						fn = copyRecursive
					default: //Uintptr UnsafePointer
						panic("invalid ty=" + itemType.Kind().String())
					}
				}
				if fn != nil {
					stu.items = append(stu.items, structItem{i, fn})
				}
			}
			if first < last {
				stu.sections = append(stu.sections, [2]int{first, last})
			}
		}

		g_structLock.Lock()
		//logs.Infof("getStruct name=%v sections=%v", ty.Name(), stu.sections)
		g_structs[ty] = stu
		g_structLock.Unlock()
	}
	return stu
}

func canMemcopy(ty reflect.Type) bool {
	switch ty.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32,
		reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Uint64, reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.String:
		return true
	case reflect.Func, reflect.Chan: //值传递应该没问题
		return true
	case reflect.Array:
		return canMemcopy(ty.Elem())
	case reflect.Struct:
		for i := 0; i < ty.NumField(); i++ {
			if !canMemcopy(ty.Field(i).Type) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func copyArray(ty reflect.Type, inStruct bool) func(reflect.Value, reflect.Value) {
	elem := ty.Elem()
	if canMemcopy(elem) {
		return func(src reflect.Value, dst reflect.Value) {
			if inStruct {
				return
			}
			copy(array2Bytes(dst), array2Bytes(src))
		}
	} else {
		return func(src reflect.Value, dst reflect.Value) {
			for i := 0; i < src.Len(); i++ {
				copyRecursive(src.Index(i), dst.Index(i))
			}
		}
	}
}

func copySlice(ty reflect.Type, inStruct bool) func(reflect.Value, reflect.Value) {
	elem := ty.Elem()
	if canMemcopy(elem) {
		return func(src reflect.Value, dst reflect.Value) {
			if src.IsNil() {
				return
			}
			dstSlice := reflect.MakeSlice(src.Type(), src.Len(), src.Cap())
			copy(slice2Bytes(dstSlice), slice2Bytes(src))
			dst.Set(dstSlice)
		}
	} else {
		return func(src reflect.Value, dst reflect.Value) {
			if src.IsNil() {
				return
			}
			dst.Set(reflect.MakeSlice(src.Type(), src.Len(), src.Cap()))
			for i := 0; i < src.Len(); i++ {
				copyRecursive(src.Index(i), dst.Index(i))
			}
		}
	}
}

func copyMap(ty reflect.Type, inStruct bool) func(reflect.Value, reflect.Value) {
	keyType := ty.Key()
	valueType := ty.Elem()

	keyCanCopy := canMemcopy(keyType)
	valueCanCopy := canMemcopy(valueType)

	if keyCanCopy && valueCanCopy {
		if keyType.Kind() == reflect.Int {
			if valueType.Kind() == reflect.Int { //map[int]int
				return func(src reflect.Value, dst reflect.Value) {
					if src.IsNil() {
						return
					}
					ds := make(map[int]int, src.Len())
					real := src.Interface().(map[int]int)
					for k, v := range real {
						ds[k] = v
					}
					dst.Set(reflect.ValueOf(ds))
				}
			} else if valueType.Kind() == reflect.String { //map[int]string
				return func(src reflect.Value, dst reflect.Value) {
					ds := make(map[int]string, src.Len())
					real := src.Interface().(map[int]string)
					for k, v := range real {
						ds[k] = v
					}
					dst.Set(reflect.ValueOf(ds))
				}
			} else if valueType.Kind() == reflect.Float64 { //map[int]float64
				return func(src reflect.Value, dst reflect.Value) {
					ds := make(map[int]float64, src.Len())
					real := src.Interface().(map[int]float64)
					for k, v := range real {
						ds[k] = v
					}
					dst.Set(reflect.ValueOf(ds))
				}
			} else if valueType.Kind() == reflect.Bool { //map[int]bool
				return func(src reflect.Value, dst reflect.Value) {
					ds := make(map[int]bool, src.Len())
					real := src.Interface().(map[int]bool)
					for k, v := range real {
						ds[k] = v
					}
					dst.Set(reflect.ValueOf(ds))
				}
			}
		} else if keyType.Kind() == reflect.String {
			if valueType.Kind() == reflect.String { //map[string]string
				return func(src reflect.Value, dst reflect.Value) {
					ds := make(map[string]string, src.Len())
					real := src.Interface().(map[string]string)
					for k, v := range real {
						ds[k] = v
					}
					dst.Set(reflect.ValueOf(ds))
				}
			} else if valueType.Kind() == reflect.Int { //map[string]int
				return func(src reflect.Value, dst reflect.Value) {
					ds := make(map[string]int, src.Len())
					real := src.Interface().(map[string]int)
					for k, v := range real {
						ds[k] = v
					}
					dst.Set(reflect.ValueOf(ds))
				}
			} else if valueType.Kind() == reflect.Float64 { //map[string]float64
				return func(src reflect.Value, dst reflect.Value) {
					ds := make(map[string]float64, src.Len())
					real := src.Interface().(map[string]float64)
					for k, v := range real {
						ds[k] = v
					}
					dst.Set(reflect.ValueOf(ds))
				}
			} else if valueType.Kind() == reflect.Bool { //map[string]bool
				return func(src reflect.Value, dst reflect.Value) {
					ds := make(map[string]bool, src.Len())
					real := src.Interface().(map[string]bool)
					for k, v := range real {
						ds[k] = v
					}
					dst.Set(reflect.ValueOf(ds))
				}
			}
		}
	}

	return func(src reflect.Value, dst reflect.Value) {
		if src.IsNil() {
			return
		}
		dst.Set(reflect.MakeMapWithSize(src.Type(), src.Len()))
		for _, srcKey := range src.MapKeys() {
			k1 := srcKey
			if !keyCanCopy {
				k1 = reflect.ValueOf(DeepCopy(srcKey.Interface()))
			}
			v1 := src.MapIndex(srcKey)
			if !valueCanCopy {
				dstValue := reflect.New(v1.Type()).Elem()
				copyRecursive(v1, dstValue)
				v1 = dstValue
			}
			dst.SetMapIndex(k1, v1)
		}
	}
}

func copyStruct(ty reflect.Type, inStruct bool) func(reflect.Value, reflect.Value) {
	if ty.Kind() != reflect.Struct {
		panic("invalid type")
	}
	stu := getStruct(ty)
	return func(src reflect.Value, dst reflect.Value) {
		srcBs := struct2bytes(src)
		dstBs := struct2bytes(dst)
		//copy(dstBs, srcBs) //不能跳过私有变量
		for _, v := range stu.sections {
			copy(dstBs[v[0]:v[1]], srcBs[v[0]:v[1]])
		}
		for _, item := range stu.items {
			item.fn(src.Field(item.index), dst.Field(item.index))
		}
	}
	return nil
}

func array2Bytes(value reflect.Value) []byte {
	if value.Kind() != reflect.Array {
		panic("invalid type")
	}
	ptr := value.UnsafeAddr()
	size := uintptr(value.Len()) * value.Type().Elem().Size() //sizeof() * len

	h := [3]uintptr{ptr, size, size}
	return *(*[]byte)(unsafe.Pointer(&h))
}

func slice2Bytes(value reflect.Value) []byte {
	if value.Kind() != reflect.Slice {
		panic("invalid type")
	}
	ptr := value.Pointer()
	size := uintptr(value.Len()) * value.Type().Elem().Size() //sizeof() * len

	h := [3]uintptr{ptr, size, size}
	return *(*[]byte)(unsafe.Pointer(&h))
}

func struct2bytes(value reflect.Value) []byte {
	if value.Kind() != reflect.Struct {
		panic("invalid type")
	}
	ptr := uintptr(unsafe.Pointer(value.UnsafeAddr()))
	size := int(value.Type().Size())

	type SliceHeader struct {
		addr uintptr
		len  int
		cap  int
	}
	header := &SliceHeader{
		addr: ptr,
		cap:  size,
		len:  size,
	}
	data := *(*[]byte)(unsafe.Pointer(header))
	return data
}
