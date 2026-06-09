//go:build !noasm && amd64
// +build !noasm,amd64

package metrics_codec

import (
	"reflect"
	"unsafe"

	"golang.org/x/sys/cpu"
)

var (
	smTable = [16]int8{1, 2, 4, 8, 16, 32, 64, -128, 1, 2, 4, 8, 16, 32, 64, -128}
	hmTable = [16]uint8{0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f}
)

//go:noescape
func _validateStringSIMD(table unsafe.Pointer, str unsafe.Pointer, len uint64, sm, hm unsafe.Pointer, rt unsafe.Pointer)

func validateTagNameSIMD(s string) bool {
	sptr := unsafe.Pointer((*reflect.StringHeader)(unsafe.Pointer(&s)).Data)
	var rt byte
	_validateStringSIMD(unsafe.Pointer(&nameLUBT), sptr, uint64(len(s)), unsafe.Pointer(&smTable), unsafe.Pointer(&hmTable), unsafe.Pointer(&rt))
	return rt != 0
}

func validateTagValueSIMD(s string) bool {
	sptr := unsafe.Pointer((*reflect.StringHeader)(unsafe.Pointer(&s)).Data)
	var rt byte
	_validateStringSIMD(unsafe.Pointer(&valueLUBT), sptr, uint64(len(s)), unsafe.Pointer(&smTable), unsafe.Pointer(&hmTable), unsafe.Pointer(&rt))
	return rt != 0
}

func init() {
	supportSIMD = cpu.X86.HasSSE3
}
