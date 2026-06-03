package uslice

//https://code.byted.org/gopkg/facility/blob/master/slice/slice.unique.generate.go

//UniqueXXX 切片去重，保持相对位置，返回新切片

func UniqueStrings(a []string) []string {
	unique := make(map[string]struct{}, len(a))
	r := make([]string, 0, len(a))
	for _, v := range a {
		if _, ok := unique[v]; !ok {
			unique[v] = struct{}{}
			r = append(r, v)
		}
	}
	return r
}

func UniqueInts(a []int) []int {
	unique := make(map[int]struct{}, len(a))
	r := make([]int, 0, len(a))
	for _, v := range a {
		if _, ok := unique[v]; !ok {
			unique[v] = struct{}{}
			r = append(r, v)
		}
	}
	return r
}

func UniqueInt64s(a []int64) []int64 {
	unique := make(map[int64]struct{}, len(a))
	r := make([]int64, 0, len(a))
	for _, v := range a {
		if _, ok := unique[v]; !ok {
			unique[v] = struct{}{}
			r = append(r, v)
		}
	}
	return r
}

func UniqueInt32s(a []int32) []int32 {
	unique := make(map[int32]struct{}, len(a))
	r := make([]int32, 0, len(a))
	for _, v := range a {
		if _, ok := unique[v]; !ok {
			unique[v] = struct{}{}
			r = append(r, v)
		}
	}
	return r
}

func UniqueInt16s(a []int16) []int16 {
	unique := make(map[int16]struct{}, len(a))
	r := make([]int16, 0, len(a))
	for _, v := range a {
		if _, ok := unique[v]; !ok {
			unique[v] = struct{}{}
			r = append(r, v)
		}
	}
	return r
}

func UniqueInt8s(a []int8) []int8 {
	unique := make(map[int8]struct{}, len(a))
	r := make([]int8, 0, len(a))
	for _, v := range a {
		if _, ok := unique[v]; !ok {
			unique[v] = struct{}{}
			r = append(r, v)
		}
	}
	return r
}

func UniqueUints(a []uint) []uint {
	unique := make(map[uint]struct{}, len(a))
	r := make([]uint, 0, len(a))
	for _, v := range a {
		if _, ok := unique[v]; !ok {
			unique[v] = struct{}{}
			r = append(r, v)
		}
	}
	return r
}

func UniqueUint64s(a []uint64) []uint64 {
	unique := make(map[uint64]struct{}, len(a))
	r := make([]uint64, 0, len(a))
	for _, v := range a {
		if _, ok := unique[v]; !ok {
			unique[v] = struct{}{}
			r = append(r, v)
		}
	}
	return r
}

func UniqueUint32s(a []uint32) []uint32 {
	unique := make(map[uint32]struct{}, len(a))
	r := make([]uint32, 0, len(a))
	for _, v := range a {
		if _, ok := unique[v]; !ok {
			unique[v] = struct{}{}
			r = append(r, v)
		}
	}
	return r
}

func UniqueUint16s(a []uint16) []uint16 {
	unique := make(map[uint16]struct{}, len(a))
	r := make([]uint16, 0, len(a))
	for _, v := range a {
		if _, ok := unique[v]; !ok {
			unique[v] = struct{}{}
			r = append(r, v)
		}
	}
	return r
}

func UniqueUnt8s(a []uint8) []uint8 {
	unique := make(map[uint8]struct{}, len(a))
	r := make([]uint8, 0, len(a))
	for _, v := range a {
		if _, ok := unique[v]; !ok {
			unique[v] = struct{}{}
			r = append(r, v)
		}
	}
	return r
}

func UniqueFloat64s(a []float64) []float64 {
	unique := make(map[float64]struct{}, len(a))
	r := make([]float64, 0, len(a))
	for _, v := range a {
		if _, ok := unique[v]; !ok {
			unique[v] = struct{}{}
			r = append(r, v)
		}
	}
	return r
}

func UniqueFloat32s(a []float32) []float32 {
	unique := make(map[float32]struct{}, len(a))
	r := make([]float32, 0, len(a))
	for _, v := range a {
		if _, ok := unique[v]; !ok {
			unique[v] = struct{}{}
			r = append(r, v)
		}
	}
	return r
}

func UniqueBools(a []bool) []bool {
	unique := make(map[bool]struct{}, len(a))
	r := make([]bool, 0, len(a))
	for _, v := range a {
		if _, ok := unique[v]; !ok {
			unique[v] = struct{}{}
			r = append(r, v)
		}
	}
	return r
}
