package api

import "strings"

const (
	sessionAggregateTitleMaxLen  = 1024
	sessionDisplayTitleSoftLimit = 120
	sessionDisplayTitleHardLimit = 160
)

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

func trimDBString(s string, maxChars int) string {
	s = strings.TrimSpace(strings.ToValidUTF8(s, ""))
	if maxChars <= 0 || s == "" {
		return ""
	}
	if len([]rune(s)) <= maxChars {
		return s
	}
	out := make([]rune, 0, maxChars)
	for _, r := range s {
		if len(out) >= maxChars {
			break
		}
		out = append(out, r)
	}
	return string(out)
}

func summarizeSessionTitle(raw, fallback string) string {
	title := normalizePromptText(strings.ToValidUTF8(raw, ""))
	if title == "" {
		title = normalizePromptText(strings.ToValidUTF8(fallback, ""))
	}
	if title == "" {
		return ""
	}
	runes := []rune(title)
	if len(runes) <= sessionDisplayTitleHardLimit {
		return title
	}
	if cut := titleCutIndex(runes, sessionDisplayTitleSoftLimit); cut > 0 {
		return strings.TrimSpace(string(runes[:cut]))
	}
	return strings.TrimSpace(string(runes[:sessionDisplayTitleHardLimit])) + "..."
}

func titleCutIndex(runes []rune, max int) int {
	limit := min(max, len(runes))
	if limit <= 0 {
		return 0
	}
	for i := 12; i < limit; i++ {
		switch runes[i] {
		case '。', '！', '？', ';', '；', '.', '!', '?', ':', '：', ',', '，':
			return i
		}
	}
	return 0
}
