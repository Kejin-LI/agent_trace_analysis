package api

import "strings"

// safeSessionTitleForDB 对 session_title 做最保守的数据库降级:
// 1. 规范非法 UTF-8
// 2. 仅保留可见 ASCII，避免线上历史非 utf8mb4 列字符集写入失败
// 3. 折叠多余空白并截断到指定长度
func safeSessionTitleForDB(s string, maxLen int) string {
	s = strings.TrimSpace(strings.ToValidUTF8(s, ""))
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	lastSpace := false
	for _, r := range s {
		switch {
		case r >= 0x20 && r <= 0x7e:
			b.WriteRune(r)
			lastSpace = false
		case r == '\n' || r == '\r' || r == '\t':
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		default:
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		}
	}
	return trimLen(strings.Join(strings.Fields(b.String()), " "), maxLen)
}
