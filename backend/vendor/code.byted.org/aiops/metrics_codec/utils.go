package metrics_codec

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"sync"
	"time"
	"unicode"
	"unsafe"
)

const (
	MaxFieldCount    = 1024
	MessageSizeLimit = (128 + 32) << 10

	maxFieldCount = 1024
	maxTagLen     = 255
	minValidChar  = '\u0020' // space
	maxValidChar  = '\u007E' // ~
	maxASCII      = '\u007F' // unicode.MaxASCII

	validFlag              = 1
	simdBranchLenThreshold = 16
)

const (
	offset32 uint32 = 2166136261
	prime32  uint32 = 16777619
)

var supportSIMD bool

var (
	// these chars are valid in tag names
	whiteListNameChars = []byte{'_', '-', '.', '%', ':', '/'}

	// tag values cannot start or end with these runes.
	specialChars = []byte{' '}

	// these chars are valid in tag values
	whiteListValueChars = []byte{' ', '!', '#', '$', '%', '&', '\'', '+', '-', '.', '/', ':', ';', '=', '?', '@', '[',
		'\\', ']', '^', '_', '`', '~'}

	// lookup table for tag value
	valueLUT  [128]uint8
	valueLUBT [16]uint8

	// lookup table for tag name
	nameLUT  [128]uint8
	nameLUBT [16]uint8
)

func init() {
	// build the lookup tables
	for i := 0; i < 128; i++ {
		if unicode.IsNumber(rune(i)) || unicode.IsLetter(rune(i)) || isCharInList(byte(i), whiteListNameChars) {
			nameLUT[i] = validFlag
			lowerNibble := i & 0x0f
			upperNibble := i >> 4
			nameLUBT[lowerNibble] |= 1 << upperNibble
		}

		if unicode.IsNumber(rune(i)) || unicode.IsLetter(rune(i)) || isCharInList(byte(i), whiteListValueChars) {
			valueLUT[i] = validFlag
			lowerNibble := i & 0x0f
			upperNibble := i >> 4
			valueLUBT[lowerNibble] |= 1 << upperNibble
		}
	}
}

func EncodeInt8(buf []byte, i int8) []byte {
	return append(buf, byte(i))
}

func EncodeInt32(buf []byte, i int32) []byte {
	return append(buf, byte(i), byte(i>>8), byte(i>>16), byte(i>>24))
}

func EncodeInt64(buf []byte, i int64) []byte {
	return append(buf, byte(i), byte(i>>8), byte(i>>16), byte(i>>24), byte(i>>32), byte(i>>40), byte(i>>48), byte(i>>56))
}

func EncodeUInt64(buf []byte, i uint64) []byte {
	return append(buf, byte(i), byte(i>>8), byte(i>>16), byte(i>>24), byte(i>>32), byte(i>>40), byte(i>>48), byte(i>>56))
}

func EncodeInt32Array(buf []byte, is []int32) []byte {
	if len(is) > math.MaxInt32 {
		is = is[:math.MaxInt32]
	}
	buf = EncodeInt32(buf, int32(len(is)))
	for _, i := range is {
		buf = EncodeInt32(buf, i)
	}
	return buf
}

func EncodeStr(buf []byte, s string) []byte {
	if len(s) >= math.MaxInt32 {
		s = s[:math.MaxInt32]
	}
	buf = EncodeInt32(buf, int32(len(s)))
	buf = append(buf, s...)
	return buf
}

func EncodeStrArray(buf []byte, ss []string) []byte {
	if len(ss) > math.MaxInt32 {
		ss = ss[:math.MaxInt32]
	}
	buf = EncodeInt32(buf, int32(len(ss)))
	for _, s := range ss {
		buf = EncodeStr(buf, s)
	}
	return buf
}

func EncodeFloatArray(buf []byte, fs []float64) []byte {
	if len(fs) > math.MaxInt32 {
		fs = fs[:math.MaxInt32]
	}
	buf = EncodeInt32(buf, int32(len(fs)))
	for _, f := range fs {
		buf = EncodeUInt64(buf, math.Float64bits(f))
	}
	return buf
}

func EncodeFloatArrayV3(buf []byte, fs []float64) []byte {
	if len(fs) > math.MaxInt32 {
		fs = fs[:math.MaxInt32]
	}
	buf = EncodeInt32(buf, int32(len(fs)))
	for _, f := range fs {
		buf = EncodeDoubleV3(buf, f)
	}
	return buf
}

func EncodeDoubleV3(buf []byte, f float64) []byte {
	buf = EncodeInt32(buf, int32(8))
	buf = EncodeUInt64(buf, math.Float64bits(f))
	return buf
}

func Fnv32a(ss ...string) uint32 {
	if len(ss) == 0 {
		return 0
	}
	h := offset32
	for _, s := range ss {
		for _, char := range []byte(s) {
			h ^= uint32(char)
			h *= prime32
		}
	}
	return h
}

func Fnv32aTags(ts ...Tag) uint32 {
	if len(ts) == 0 {
		return 0
	}

	h := offset32
	h = ComputeHashcode(h, ts...)
	return h
}

func ComputeHashcode(hashcode uint32, ts ...Tag) uint32 {
	h := hashcode
	for _, t := range ts {
		for _, char := range []byte(t.Key) {
			h ^= uint32(char)
			h *= prime32
		}

		for _, char := range []byte(t.Value) {
			h ^= uint32(char)
			h *= prime32
		}
	}
	return h
}

func DecodeUint8(data []byte) uint8 {
	return data[0]
}

func DecodeUint32(data []byte) uint32 {
	_ = data[3]
	return uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
}

func DecodeUint64(b []byte) uint64 {
	return binary.LittleEndian.Uint64(b)
}

func ConvertInt64ToFloat64(b []byte) float64 {
	return float64(int64(DecodeUint64(b)))
}

func DecodeInt32Array(data []byte, offset int) ([]int32, int, error) {
	if len(data)-offset < 4 { // We need 4 bytes to indicate the length of a float array.
		return nil, 0, fmt.Errorf("no enough bytes for an int32 array")
	}
	pos := offset
	numberOfValues := int(DecodeUint32(data[pos:]))
	pos += 4
	res := make([]int32, 0, numberOfValues)
	for i := 0; i < numberOfValues; i++ {
		value := DecodeUint32(data[pos:])
		pos += 4
		res = append(res, int32(value))
	}
	return res, pos - offset, nil
}

func DecodeFloatArrayV3(data []byte, offset int, fields []Field) ([]float64, int, error) {
	if len(data)-offset < 4 { // We need 4 bytes to indicate the length of a float array.
		return nil, 0, fmt.Errorf("no enough bytes for a float array")
	}
	pos := offset
	numberOfValues := int(DecodeUint32(data[pos:]))
	pos += 4

	isMultiField := len(fields) > 0

	if isMultiField && numberOfValues != len(fields) {
		return nil, 0, fmt.Errorf("conflicts in lengths of v3 multi-field metric: %d and field names: %d", numberOfValues, len(fields))
	}

	res := make([]float64, 0, numberOfValues)
	for i := 0; i < numberOfValues; i++ {
		length := int(DecodeUint32(data[pos:]))
		pos += 4
		var value float64
		var valueType int8 = 0 // double by default
		if isMultiField {
			valueType = fields[i].VType
		}
		switch valueType {
		case 0:
			value = math.Float64frombits(DecodeUint64(data[pos : pos+length]))
		case 1:
			value = ConvertInt64ToFloat64(data[pos : pos+length])
		}
		pos += length
		res = append(res, value)
	}
	return res, pos - offset, nil
}

func DecodeFloatArrayV4(data []byte, offset int, fields []Field, indices []int32) ([]float64, int, error) {
	if len(data)-offset < 4 { // We need 4 bytes to indicate the length of a float array.
		return nil, 0, fmt.Errorf("no enough bytes for a float array")
	}
	pos := offset
	numberOfValues := int(DecodeUint32(data[pos:]))
	pos += 4
	res := make([]float64, 0, numberOfValues)

	// if fields is empty, which means it is a single field value, the value is always float64.
	isMultiField := len(fields) > 0

	// Even for multi field metric, indices can be empty.
	// It means use all fields have values, and the indices are actually [0, 1, 2, ..., len(fields)-1]
	useIndex := isMultiField && len(indices) > 0

	if isMultiField {
		if (numberOfValues != len(indices)) && (numberOfValues != len(fields)) {
			return nil, 0, fmt.Errorf("conflicts in lengths of v4 multi-field metric: %d, field names: %d, indices: %d", numberOfValues, len(fields), len(indices))
		}
	}

	for i := 0; i < numberOfValues; i++ {
		length := int(DecodeUint32(data[pos:]))
		pos += 4
		var value float64
		var valueType int8 = 0 // double by default
		if isMultiField {
			if useIndex {
				valueType = fields[indices[i]].VType
			} else {
				valueType = fields[i].VType
			}
		}

		switch valueType {
		case 0:
			value = math.Float64frombits(DecodeUint64(data[pos : pos+length]))
		case 1:
			value = ConvertInt64ToFloat64(data[pos : pos+length])
		}
		pos += length
		res = append(res, value)
	}
	return res, pos - offset, nil
}

func DecodeStrArray(data []byte, offset int) ([]string, int, error) {
	if len(data)-offset < 4 { // We need 4 bytes to indicate the length of a string array.
		return nil, 0, ErrTooShortForStringArray
	}
	pos := offset
	numberOfStr := int(DecodeUint32(data[pos:]))
	pos += 4
	res := make([]string, 0, numberOfStr)
	for i := 0; i < numberOfStr; i++ {
		str, readLength, err := DecodeStr(data, pos)
		if err != nil {
			return nil, 0, err
		}
		pos += readLength
		res = append(res, str)
	}
	return res, pos - offset, nil
}

func DecodeStr(data []byte, offset int) (string, int, error) {
	if len(data)-offset < 4 {
		return "", 0, ErrNoEnoughBytesForString
	}
	pos := offset
	length := DecodeUint32(data[pos:])
	if length > maxTagLen {
		sampleRange := int(math.Min(float64(length)+4, 255))
		return "", 0, fmt.Errorf("string is too long: %d, pos: %d, show %d bytes around, "+
			"before: %s, after: %s", length, pos, sampleRange,
			bytesToStringUnsafe(safeGetBytes(data, pos-sampleRange, pos)),
			bytesToStringUnsafe(safeGetBytes(data, pos, pos+sampleRange)),
		)
	}
	pos += 4
	res := string(data[pos : pos+int(length)])
	pos += int(length)
	return res, pos - offset, nil
}

func FillBackInt8(buf []byte, offset int, value int8) {
	buf[offset] = byte(value)
}

func FillBackInt32(buf []byte, offset int, value int32) {
	_ = buf[offset+3]
	buf[offset] = byte(value)
	buf[offset+1] = byte(value >> 8)
	buf[offset+2] = byte(value >> 16)
	buf[offset+3] = byte(value >> 24)
}

func FillBackInt64(buf []byte, offset int, value int64) {
	_ = buf[offset+7]
	buf[offset+0] = byte(value)
	buf[offset+1] = byte(value >> 8)
	buf[offset+2] = byte(value >> 16)
	buf[offset+3] = byte(value >> 24)
	buf[offset+4] = byte(value >> 32)
	buf[offset+5] = byte(value >> 40)
	buf[offset+6] = byte(value >> 48)
	buf[offset+7] = byte(value >> 56)
}

func IsAgentSidecar() bool {
	if sidecar := os.Getenv("METRICSERVER2_SIDECAR"); sidecar == "1" {
		return true
	}

	return false
}

// IsRuneSpecial checks whether the rune is a special rune, which cannot be the first or the last char of the string.
func IsRuneSpecial(b rune) bool {
	for _, v := range specialChars {
		if int32(v) == b {
			return true
		}
	}
	return false
}

// IsRuneValidForName checks whether the rune is valid for a metric name or a tag name.
func IsRuneValidForName(b rune) bool {
	return validateRuneTable(&nameLUT, b)
}

// IsRuneValidForValue checks whether the rune is valid for a tag value.
func IsRuneValidForValue(b rune) bool {
	return validateRuneTable(&valueLUT, b)
}

// IsValidString is the same as IsValidName.
// It checks whether the string is valid for a metric name or tag name.
func IsValidString(s string) bool {
	return IsValidName(s)
}

// IsValidName checks whether the string is valid for a metric name or tag name.
func IsValidName(s string) bool {
	l := len(s)
	if l == 0 || l > maxTagLen {
		return false
	}
	if l < simdBranchLenThreshold || !supportSIMD {
		return validateStringTable(&nameLUT, s)
	}
	return validateTagNameSIMD(s)
}

// IsValidTagValue checks whether the string is valid for a tag value.
func IsValidTagValue(s string) bool {
	l := len(s)
	if l == 0 || l > maxTagLen {
		return false
	}
	for _, char := range specialChars {
		if s[0] == char || s[l-1] == char {
			return false
		}
	}
	if l < simdBranchLenThreshold || !supportSIMD {
		return validateStringTable(&valueLUT, s)
	}
	return validateTagValueSIMD(s)
}

func SizeOfVStr(s string) int {
	return 4 + len(s)
}

func SizeOfVStrs(strs []string) int {
	res := 4
	for _, v := range strs {
		res += SizeOfVStr(v)
	}
	return res
}

func BuildTsFromStrArray(strArr []string) ([]Tag, error) {
	if len(strArr)&1 == 1 {
		return nil, fmt.Errorf("the count of strings is odd: %d", len(strArr))
	}
	Ts := make([]Tag, 0, len(strArr)/2)
	for i := 0; i+1 < len(strArr); i += 2 {
		tag := Tag{
			Key:   strArr[i],
			Value: strArr[i+1],
		}
		Ts = append(Ts, tag)
	}
	return Ts, nil
}

func TimeUnixMilli(ts time.Time) int64 {
	return ts.UnixNano() / int64(time.Millisecond/time.Nanosecond)
}

func TimeFromUnixMilli(msec int64) time.Time {
	return time.Unix(msec/1e3, msec%1e3*1e6)
}

func isCharInList(c byte, list []byte) bool {
	for _, b := range list {
		if b == c {
			return true
		}

	}
	return false
}

func validateStringTable(table *[128]uint8, s string) bool {
	for _, r := range []byte(s) {
		if r > maxASCII {
			return false
		}
		if table[r] != validFlag {
			return false
		}
	}
	return true
}

func validateRuneTable(table *[128]uint8, r rune) bool {
	if r > maxASCII {
		return false
	}
	if table[byte(r)] != validFlag {
		return false
	}
	return true
}

func cloneStr(str string) string {
	dst := make([]byte, len(str))
	copy(dst, str)
	return string(dst)
}

func bytesToStringUnsafe(b []byte) string {
	/* #nosec G103 */
	return *(*string)(unsafe.Pointer(&b))
}

type bufferPool struct {
	sync.Pool
}

func newBufferPool() *bufferPool {
	return &bufferPool{
		Pool: sync.Pool{New: func() interface{} {
			buffer := make([]byte, 0, 128)
			return &buffer
		},
		},
	}
}

func (b *bufferPool) get() *[]byte {
	return b.Get().(*[]byte)
}

func (b *bufferPool) put(p *[]byte) {
	if p == nil {
		return
	}
	*p = (*p)[:0]
	b.Pool.Put(p)
}

func generateKey(buffer []byte, metricName string, metricType MType, fields []Field) string {
	buffer = append(buffer, metricName...)
	buffer = EncodeInt8(buffer, int8(metricType))

	for _, v := range fields {
		buffer = EncodeStr(buffer, v.Name)
		buffer = EncodeInt8(buffer, v.VType)
	}
	return bytesToStringUnsafe(buffer)
}

func safeGetBytes(data []byte, start, end int) []byte {
	if start < 0 {
		start = 0
	}

	if end > len(data) {
		end = len(data)
	}
	return data[start:end]
}
