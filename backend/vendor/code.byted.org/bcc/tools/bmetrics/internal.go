package bmetrics

import (
	"sync"

	"code.byted.org/gopkg/env"
)

var (
	allMu     sync.RWMutex
	allClient = make(map[string]*ClientV2)
	defClient *ClientV2
)

func getClient(psm string) *ClientV2 {
	allMu.Lock()
	defer allMu.Unlock()

	c := allClient[psm]
	if c != nil {
		return c
	}
	c = newClientV2(psm, 100)
	allClient[psm] = c
	return c
}

func init() {
	defClient = getClient(env.PSM())
}

// ----------------------------------------
func formatEmpty(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func formatAny(s string) string {
	// 所有打点的字符串（包括 name，tag k，tag v）
	// 长度限制是1-255， 允许字符是a-zA-Z0-9._-/:
	// 不可以为空， 也不能包括空格和换行，否则会导致打点数据不能查询，包括 metric name, tag key/value， 不支持中文
	var b []byte
	for i := 0; i < len(s); i++ {
		if !globalAllowChars[s[i]] {
			if b == nil {
				b = []byte(s)
			}
			b[i] = '_'
		}
	}
	if b != nil {
		s = string(b)
	}
	s = formatEmpty(s)
	if len(s) > 254 {
		s = s[:254]
	}
	return s
}

var globalAllowChars [255]bool

func init() {
	for i := 'a'; i <= 'z'; i++ {
		globalAllowChars[i] = true
	}
	for i := 'A'; i <= 'Z'; i++ {
		globalAllowChars[i] = true
	}
	for i := '0'; i <= '9'; i++ {
		globalAllowChars[i] = true
	}
	globalAllowChars[int('.')] = true
	globalAllowChars[int('_')] = true
	globalAllowChars[int('-')] = true
	globalAllowChars[int('/')] = true
	globalAllowChars[int(':')] = true
}
