package tools

import (
	"strconv"
	"time"
	"unsafe"

	"code.byted.org/bcc/tools/uconv"
)

//string -> int64
func Atol(s string) int64 {
	ret, _ := strconv.ParseInt(s, 10, 64)
	return ret
}

//string -> int
func Atoi(s string) int {
	ret, _ := strconv.Atoi(s)
	return ret
}

//string -> float64
func Atof(s string) float64 {
	ret, _ := strconv.ParseFloat(s, 64)
	return ret
}

//string -> bool
func Atob(s string) bool {
	ret, _ := strconv.ParseBool(s)
	return ret
}

//int64 -> string
func Ltoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

//int -> string
func Itoa(v int) string {
	return strconv.Itoa(v)
}

//float64 -> string
func Ftoa(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

//bool -> string
func Btoa(v bool) string {
	return strconv.FormatBool(v)
}

//任意类型->string
func ToString(v interface{}) string {
	return uconv.ToString(v)
}

//任意类型->[]byte
func ToBytes(v interface{}) []byte {
	return uconv.ToBytes(v)
}

//任意类型->int
func ToInt(v interface{}) int {
	return uconv.ToInt(v)
}

func ToIntE(v interface{}) (int, error) {
	return uconv.ToIntEx(v)
}

//任意类型->bool
func ToBool(v interface{}) bool {
	return uconv.ToBool(v)
}

func ToBoolE(v interface{}) (bool, error) {
	return uconv.ToBoolEx(v)
}

//任意类型->float
func ToFloat(v interface{}) float64 {
	return uconv.ToFloat(v)
}

func ToFloatE(v interface{}) (float64, error) {
	return uconv.ToFloatEx(v)
}

//字节数->可读文字（10240->10KB）
func ToUnit(bytes int, decimals ...int) string {
	return uconv.ToUnit(bytes, decimals...)
}

//数量耗时->qps（100000,10s->10000）
func ToQps(num int, cost time.Duration) int {
	return uconv.ToQps(num, cost)
}

//string转换为[]byte (只读的，临时使用，外部保证生命周期）
func String2Bytes(s string) []byte {
	x := (*[2]uintptr)(unsafe.Pointer(&s))
	h := [3]uintptr{x[0], x[1], x[1]}
	//runtime.KeepAlive(&s) //作用有限
	return *(*[]byte)(unsafe.Pointer(&h))
}

//[]byte转换为string (只读的，临时使用，外部保证生命周期）
func Bytes2String(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}

//ptr
func PtrInt(arg int) *int {
	return &arg
}

func PtrInt8(arg int8) *int8 {
	return &arg
}

func PtrInt16(arg int16) *int16 {
	return &arg
}

func PtrInt32(arg int32) *int32 {
	return &arg
}

func PtrInt64(arg int64) *int64 {
	return &arg
}

func PtrUint(arg uint) *uint {
	return &arg
}

func PtrUint8(arg uint8) *uint8 {
	return &arg
}

func PtrUint16(arg uint16) *uint16 {
	return &arg
}

func PtrUint32(arg uint32) *uint32 {
	return &arg
}

func PtrUint64(arg uint64) *uint64 {
	return &arg
}

func PtrFloat64(arg float64) *float64 {
	return &arg
}

func PtrFloat32(arg float32) *float32 {
	return &arg
}

func PtrString(arg string) *string {
	return &arg
}

func PtrBool(arg bool) *bool {
	return &arg
}

func PtrDuration(arg time.Duration) *time.Duration {
	return &arg
}

func PtrBytes(s []byte) *[]byte {
	return &s
}
