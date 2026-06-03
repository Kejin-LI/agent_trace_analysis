package uconv

import (
	"strconv"
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
