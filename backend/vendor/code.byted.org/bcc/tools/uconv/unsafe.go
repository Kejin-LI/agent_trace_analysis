package uconv

import (
	"unsafe"
)

//string转换为[]byte (只读的，临时使用，外部保证生命周期）
func String2Bytes(s string) []byte {
	x := (*[2]uintptr)(unsafe.Pointer(&s))
	h := [3]uintptr{x[0], x[1], x[1]}
	//runtime.KeepAlive(&s) //todo 是否有必要？
	return *(*[]byte)(unsafe.Pointer(&h))
}

//[]byte转换为string (只读的，临时使用，外部保证生命周期）
func Bytes2String(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}
