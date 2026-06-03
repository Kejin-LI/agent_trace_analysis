package tools

import "code.byted.org/bcc/tools/uhash"

func Crc32(b []byte) uint32 {
	return uhash.Crc32(b)
}

func Crc64(b []byte) uint64 {
	return uhash.Crc64(b)
}

//返回 [0~2^63]
func Crc(b []byte) int {
	r := int(uhash.Crc64(b))
	if r < 0 {
		r = -r
	}
	return r
}
