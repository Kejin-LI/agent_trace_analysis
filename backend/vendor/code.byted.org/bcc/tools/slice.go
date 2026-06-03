package tools

import "code.byted.org/bcc/tools/uslice"

//切片操作：是否包含元素（任意类型）
func SliceContain(any interface{}, item interface{}) bool {
	return uslice.Contain(any, item)
}

//切片操作：是否相等（任意类型）
func SliceEqual(any0, any1 interface{}) bool {
	return uslice.Equal(any0, any1)
}

//切片操作，连接为字符串（任意类型）
func SliceJoin(any interface{}, sep string) string {
	return uslice.Join(any, sep)
}

//切片操作：随机（任意类型）
func SliceRandom(any interface{}) {
	uslice.Random(any)
}

//切片操作：升序排序（基本类型）
func SliceSort(any interface{}) {
	uslice.Sort(any)
}

//切片操作：降序排序（基本类型）
func SliceSortDesc(any interface{}) {
	uslice.SortDesc(any)
}

//切片操作：把size分割多份，每份batch个 //每份为[B,E)
func SliceSplitNum(size int, batch int) []uslice.OneSplit {
	return uslice.SplitNum(size, batch)
}
