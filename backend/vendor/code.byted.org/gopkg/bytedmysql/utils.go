package bytedmysql

import (
	"strconv"
	"strings"
	"time"
)

type mapwrap map[string]string

// get from map and remove key if exist.
func (m mapwrap) get(key string) string {
	v := m[key]
	delete(m, key)
	return v
}

func (m mapwrap) getBool(key string) (retV bool, retOk bool) {
	v, exist := m[key]
	if !exist {
		return
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return
	}

	return b, true
}

func (m mapwrap) getDuration(key string) (du time.Duration, retOk bool) {
	v, exist := m[key]
	if !exist {
		return
	}

	du, err := time.ParseDuration(v)
	if err != nil {
		return
	}

	return du, true
}

func (m mapwrap) getSplice(key string, sep string) (s []string, retOk bool) {
	v, exist := m[key]
	if !exist {
		return
	}

	return strings.Split(v, sep), true
}

func isUnixDomainSocket(addr string) bool {
	return strings.HasPrefix(addr, "/")
}
