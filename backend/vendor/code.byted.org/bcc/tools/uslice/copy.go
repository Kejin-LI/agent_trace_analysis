package uslice

//深拷贝
func CopyStrings(arr []string) (r []string) {
	r = make([]string, len(arr))
	copy(r, arr[:])
	return
}

func CopyInts(arr []int) (r []int) {
	r = make([]int, len(arr))
	copy(r, arr[:])
	return
}

func CopyInt64s(arr []int64) (r []int64) {
	r = make([]int64, len(arr))
	copy(r, arr[:])
	return
}

func CopyInt32s(arr []int32) (r []int32) {
	r = make([]int32, len(arr))
	copy(r, arr[:])
	return
}

func CopyInt16s(arr []int16) (r []int16) {
	r = make([]int16, len(arr))
	copy(r, arr[:])
	return
}

func CopyInt8s(arr []int8) (r []int8) {
	r = make([]int8, len(arr))
	copy(r, arr[:])
	return
}

func CopyUints(arr []uint) (r []uint) {
	r = make([]uint, len(arr))
	copy(r, arr[:])
	return
}

func CopyUint64s(arr []uint64) (r []uint64) {
	r = make([]uint64, len(arr))
	copy(r, arr[:])
	return
}

func CopyUint32s(arr []uint32) (r []uint32) {
	r = make([]uint32, len(arr))
	copy(r, arr[:])
	return
}

func CopyUint16s(arr []uint16) (r []uint16) {
	r = make([]uint16, len(arr))
	copy(r, arr[:])
	return
}

func CopyUint8s(arr []uint8) (r []uint8) {
	r = make([]uint8, len(arr))
	copy(r, arr[:])
	return
}

func CopyFloat32s(arr []float32) (r []float32) {
	r = make([]float32, len(arr))
	copy(r, arr[:])
	return
}

func CopyFloat64s(arr []float64) (r []float64) {
	r = make([]float64, len(arr))
	copy(r, arr[:])
	return
}

func CopyBools(arr []bool) (r []bool) {
	r = make([]bool, len(arr))
	copy(r, arr[:])
	return
}
