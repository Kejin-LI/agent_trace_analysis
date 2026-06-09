package utils

import (
	"encoding/json"
	"fmt"
	"math"
	"runtime"
	"sync/atomic"
	"time"
	"unsafe"

	"code.byted.org/aiops/metrics_codec"
	"code.byted.org/gopkg/apm_vendor_interface"
)

func IsValidString(s string) bool {
	return metrics_codec.IsValidName(s)
}

func IsValidTagValue(s string) bool {
	return metrics_codec.IsValidTagValue(s)
}

const (
	offset32 = 2166136261
	prime32  = 16777619
)

// HashNew initializes a new fnv64a hash value.
func HashNew() uint32 {
	return offset32
}

// HashAdd adds a string to a fnv32a hash value, returning the updated hash.
func HashAdd(h uint32, s string) uint32 {
	for _, char := range []byte(s) {
		h ^= uint32(char)
		h *= prime32
	}
	return h
}

func AtomicAddFloat64(dst *float64, n float64) {
	for {
		i := 0
		bits := atomic.LoadUint64((*uint64)(unsafe.Pointer(dst)))
		now := math.Float64bits(math.Float64frombits(bits) + n)
		if atomic.CompareAndSwapUint64((*uint64)(unsafe.Pointer(dst)), bits, now) {
			return
		}
		i++
		if i > 1024 {
			runtime.Gosched()
			i = 0
		}
	}
}

func Contains(s []int, e int) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func FloatContains(s []float64, e float64) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func StrContains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func ContainsTags(tags []metrics_codec.Tag, t metrics_codec.Tag) bool {
	for _, tag := range tags {
		if tag.Key == t.Key && tag.Value == t.Value {
			return true
		}
	}
	return false
}

func ContainsTagKey(tags []metrics_codec.Tag, tagKey string) bool {
	for _, tag := range tags {
		if tag.Key == tagKey {
			return true
		}
	}
	return false
}

func UniqueStrings(ss []string) (bool, error) {
	seen := make(map[string]bool, len(ss))
	for _, s := range ss {
		if _, ok := seen[s]; !ok {
			seen[s] = true
		} else {
			return false, fmt.Errorf("found duplicate string: %s", s)
		}
	}
	return true, nil
}

func Fnv32aWithIndices(tags []metrics_codec.Tag) uint32 {
	if len(tags) == 0 {
		return 0
	}
	h := uint32(offset32)

	for _, s := range tags {
		for _, char := range []byte(s.Key) {
			h ^= uint32(char)
			h *= prime32
		}

		for _, char := range []byte(s.Value) {
			h ^= uint32(char)
			h *= prime32
		}
	}
	return h
}

type jsonBuf struct {
	buf []byte
}

func (b *jsonBuf) Write(p []byte) (n int, err error) {
	b.buf = append(b.buf, p...)
	return len(p), err
}

func Str(conf *apm_vendor_interface.MetricsConfig) string {
	buf := &jsonBuf{}
	e := json.NewEncoder(buf)
	e.SetEscapeHTML(false)
	err := e.Encode(conf)
	if err != nil {
		return fmt.Sprintf("%#v", conf)
	}
	// encode would write '\n' in the end, remove it

	buf.buf = buf.buf[:len(buf.buf)-1]
	if conf == nil {
		return "nil"
	}

	return string(buf.buf)
}

func TimeUnixMilli(ts time.Time) int64 {
	return ts.UnixNano() / int64(time.Millisecond/time.Nanosecond)
}
