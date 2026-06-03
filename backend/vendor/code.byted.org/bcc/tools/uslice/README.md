
## 用途

slice操作集合

## 功能点

Equal EqualXXX 判断2个切片是否相等 

Join 用sep切片连接成字符串，strings.Join的通用版

Random 随机

Contain ContainXXX : 是否包含指定元素

CopyXXX 深复制

UniqueXXX 判断是否有重复元素



## todo

todo
https://code.byted.org/gopkg/facility/blob/master/slice/slice.generate.go
暂时写在uconv模块，但没有封装得那么全面，是否需要？

todo
func StringSliceDelete(slice []string, target string) []string {
	index := 0
	for _, ele := range slice {
		if ele != target {
			slice[index] = ele
			index++
		}
	}
	return slice[:index]
}



// 计算两个Slice的交集并返回
func SliceIntersect(slice1 []int64, slice2 []int64) []int64 {
	ret := make([]int64, 0)
	tmp := make(map[int64]int)
	for _, v := range slice1 {
		tmp[v]++
	}
	for _, v := range slice2 {
		_, ok := tmp[v]
		if ok {
			ret = append(ret, v)
		}
	}
	return ret
}

