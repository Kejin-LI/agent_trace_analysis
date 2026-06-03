package utils

import "unsafe"

func BytesToString(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}
func StringToBytes(s string) []byte {
	return *(*[]byte)(unsafe.Pointer(&s))
}

func StrInSlice(s string, sl []string) bool {
	for _, k := range sl {
		if k == s {
			return true
		}
	}
	return false
}
