package uconv

import (
	"fmt"
	"strconv"
	"time"
)

//字节数转换为可读文字，例如 10240->10KB
func ToUnit(bytes int, decimals ...int) string {
	if len(decimals) == 0 || decimals[0] <= 0 {
		//整数
		if bytes < 1024 {
			return fmt.Sprintf("%vB", bytes)
		} else if bytes < 1024*1024 {
			return fmt.Sprintf("%vKB", int(float64(bytes)/1024.0))
		} else if bytes < 1024*1024*1024 {
			return fmt.Sprintf("%vMB", int(float64(bytes)/1024.0/1024.0))
		} else if bytes < 1024*1024*1024*1024 {
			return fmt.Sprintf("%vGB", int(float64(bytes)/1024.0/1024.0/1024.0))
		} else {
			return fmt.Sprintf("%vTB", int(float64(bytes)/1024.0/1024.0/1024.0/1024.0))
		}
	} else {
		//小数
		n := decimals[0]
		if bytes < 1024 {
			return fmt.Sprintf("%vB", bytes)
		} else if bytes < 1024*1024 {
			return fmt.Sprintf("%."+strconv.Itoa(n)+"fKB", float64(bytes)/1024.0)
		} else if bytes < 1024*1024*1024 {
			return fmt.Sprintf("%."+strconv.Itoa(n)+"fMB", float64(bytes)/1024.0/1024.0)
		} else if bytes < 1024*1024*1024*1024 {
			return fmt.Sprintf("%."+strconv.Itoa(n)+"fGB", float64(bytes)/1024.0/1024.0/1024.0)
		} else {
			return fmt.Sprintf("%."+strconv.Itoa(n)+"fTB", float64(bytes)/1024.0/1024.0/1024.0/1024.0)
		}
	}
}

//转换为qps，例如 (100000,10s)->10000
func ToQps(num int, cost time.Duration) int {
	if cost <= 0 {
		return 0
	}
	return num * 1000 * 1000 * 1000 / int(cost)
}

////转换为qps，例如 (100000,10s,2)->9.77KB/s
//func Speed2Unit(size int, cost time.Duration, decimals ...int) string {
//	return ToUnit(ToQps(size, cost), decimals...) + "/s"
//}
