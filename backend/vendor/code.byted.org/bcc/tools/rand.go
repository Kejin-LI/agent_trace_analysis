package tools

import (
	"math/rand"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

//return [0,max)
func RandInt() int {
	return rand.Int()
}

//return [min,max)
func RandIntRange(min int, max int) int {
	return rand.Int()%(max-min) + min
}

//return [0.0,1.0)
func RandFloat64() float64 {
	return rand.Float64()
}

//生成随机码:数字
func RandCode10(size int) string {
	b := make([]byte, size)
	for i := 0; i < size; i++ {
		num := byte(rand.Int31n(10))
		b[i] = '0' + num
	}
	return string(b)
}

//生成随机码:大写字母
func RandCode26(size int) string {
	b := make([]byte, size)
	for i := 0; i < size; i++ {
		num := byte(rand.Int31n(26))
		b[i] = 'A' + num
	}
	return string(b)
}

//生成随机码:数字+大写字母
func RandCode36(size int) string {
	b := make([]byte, size)
	for i := 0; i < size; i++ {
		num := byte(rand.Int31n(36))
		if num < 10 {
			b[i] = '0' + num
		} else {
			b[i] = 'A' + num - 10
		}
	}
	return string(b)
}
