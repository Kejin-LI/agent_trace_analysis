package uslice

import (
	"bytes"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"code.byted.org/bcc/tools/uconv"
	"code.byted.org/gopkg/logs"
	//"code.byted.org/toutiao/easygo/util/uconv"
)

func Join(any interface{}, sep string) string {
	switch a := any.(type) {
	case []string:
		return strings.Join(a, sep)
	case []int:
		size := (20 + len(sep)) * len(a)
		buf := bytes.NewBuffer(make([]byte, 0, size))
		for k, v := range a {
			if k > 0 {
				buf.WriteString(sep)
			}
			buf.WriteString(uconv.Itoa(v))
		}
		return uconv.Bytes2String(buf.Bytes())
	case []int64:
		size := (20 + len(sep)) * len(a)
		buf := bytes.NewBuffer(make([]byte, 0, size))
		for k, v := range a {
			if k > 0 {
				buf.WriteString(sep)
			}
			buf.WriteString(uconv.Ltoa(v))
		}
		return uconv.Bytes2String(buf.Bytes())
	case []int32:
		size := (10 + len(sep)) * len(a)
		buf := bytes.NewBuffer(make([]byte, 0, size))
		for k, v := range a {
			if k > 0 {
				buf.WriteString(sep)
			}
			buf.WriteString(uconv.Itoa(int(v)))
		}
		return uconv.Bytes2String(buf.Bytes())
	case []int16:
		size := (5 + len(sep)) * len(a)
		buf := bytes.NewBuffer(make([]byte, 0, size))
		for k, v := range a {
			if k > 0 {
				buf.WriteString(sep)
			}
			buf.WriteString(uconv.Itoa(int(v)))
		}
		return uconv.Bytes2String(buf.Bytes())
	case []int8:
		size := (3 + len(sep)) * len(a)
		buf := bytes.NewBuffer(make([]byte, 0, size))
		for k, v := range a {
			if k > 0 {
				buf.WriteString(sep)
			}
			buf.WriteString(uconv.Itoa(int(v)))
		}
		return uconv.Bytes2String(buf.Bytes())
	case []uint:
		size := (20 + len(sep)) * len(a)
		buf := bytes.NewBuffer(make([]byte, 0, size))
		for k, v := range a {
			if k > 0 {
				buf.WriteString(sep)
			}
			buf.WriteString(strconv.FormatUint(uint64(v), 10))
		}
		return uconv.Bytes2String(buf.Bytes())
	case []uint64:
		size := (20 + len(sep)) * len(a)
		buf := bytes.NewBuffer(make([]byte, 0, size))
		for k, v := range a {
			if k > 0 {
				buf.WriteString(sep)
			}
			buf.WriteString(strconv.FormatUint(v, 10))
		}
		return uconv.Bytes2String(buf.Bytes())
	case []uint32:
		size := (10 + len(sep)) * len(a)
		buf := bytes.NewBuffer(make([]byte, 0, size))
		for k, v := range a {
			if k > 0 {
				buf.WriteString(sep)
			}
			buf.WriteString(uconv.Itoa(int(v)))
		}
		return uconv.Bytes2String(buf.Bytes())
	case []uint16:
		size := (5 + len(sep)) * len(a)
		buf := bytes.NewBuffer(make([]byte, 0, size))
		for k, v := range a {
			if k > 0 {
				buf.WriteString(sep)
			}
			buf.WriteString(uconv.Itoa(int(v)))
		}
		return uconv.Bytes2String(buf.Bytes())
	case []uint8:
		size := (3 + len(sep)) * len(a)
		buf := bytes.NewBuffer(make([]byte, 0, size))
		for k, v := range a {
			if k > 0 {
				buf.WriteString(sep)
			}
			buf.WriteString(uconv.Itoa(int(v)))
		}
		return uconv.Bytes2String(buf.Bytes())
	case []float64:
		size := (20 + len(sep)) * len(a)
		buf := bytes.NewBuffer(make([]byte, 0, size))
		for k, v := range a {
			if k > 0 {
				buf.WriteString(sep)
			}
			if v >= (2<<32) || v <= -(2<<32) { //过大或过小，用E //todo 再考虑
				buf.WriteString(strconv.FormatFloat(v, 'E', -1, 64))
			} else {
				buf.WriteString(strconv.FormatFloat(v, 'f', -1, 64))
			}
		}
		return uconv.Bytes2String(buf.Bytes())
	case []float32:
		size := (13 + len(sep)) * len(a)
		buf := bytes.NewBuffer(make([]byte, 0, size))
		for k, v := range a {
			if k > 0 {
				buf.WriteString(sep)
			}
			buf.WriteString(strconv.FormatFloat(float64(v), 'f', -1, 32))
		}
		return uconv.Bytes2String(buf.Bytes())
	case []bool:
		size := (5 + len(sep)) * len(a)
		buf := bytes.NewBuffer(make([]byte, 0, size))
		for k, v := range a {
			if k > 0 {
				buf.WriteString(sep)
			}
			buf.WriteString(uconv.Btoa(v))
		}
		return uconv.Bytes2String(buf.Bytes())
	default: //性能差
		valueof := reflect.ValueOf(any)
		if valueof.Kind() == reflect.Slice {
			buf := &bytes.Buffer{}
			size := valueof.Len()
			for i := 0; i < size; i++ {
				v := valueof.Index(i).Interface()
				if i > 0 {
					buf.WriteString(sep)
				}
				buf.WriteString(fmt.Sprintf("%s", v))
			}
			return uconv.Bytes2String(buf.Bytes())
		}
		logs.Error("slice join invalid type=%v any=%v", reflect.TypeOf(any).Name(), any)
		return ""
	}
}
