package tree_machine

import (
	"code.byted.org/security/sensitive_finder_engine/masker"
	"strings"
)

var dash = []string{":", "=", " "}

func MaskInKeyAndValueWithBytes(data, kind string, b []byte, start, end int) {
	position, ok := masker.SensitiveMarkPosition[kind]
	if !ok {
		position = masker.SensitiveMarkPosition[masker.SensitiveKindUnknown]
	}

	l := len(data)
	for _, d := range dash {
		i := strings.Index(data, d)
		if i < 0 || i == l {
			continue
		}
		// first char: i+1:i+2
		if i+2 <= l && (data[i+1:i+2] == "[" || data[i+1:i+2] == "'" || data[i+1:i+2] == "\"" || data[i+1:i+2] == "\\" || data[i+1:i+2] == " ") {
			position.StartOffset += 1
			if i+3 <= l && (data[i+2:i+3] == "[" || data[i+2:i+3] == "'" || data[i+2:i+3] == "\"") {
				position.StartOffset += 1
				if i+4 <= l && (data[i+3:i+4] == "[" || data[i+3:i+4] == "'" || data[i+3:i+4] == "\"") {
					position.StartOffset += 1
				}
			}
		}
		if i+4 <= l && data[i+1:i+4] == " u'" {
			position.StartOffset += 2
		}

		if strings.HasSuffix(data[i+1:], "\"") || strings.HasSuffix(data[i+1:], "'") {
			position.EndOffset += 1
		}
		if strings.HasPrefix(data[i+1:], "map[") {
			position.StartOffset += 3
		}
		if strings.HasPrefix(data[i+1:], "[]string{") {
			position.StartOffset += 9
		}

		copy(b[start:start+i+1], data[:i+1])
		maskDataCustomByteWithBytes(data[i+1:], position, b, start+i+1, end)
		return
	}

	maskDataCustomByteWithBytes(data, position, b, start, end)
}

func maskDataCustomByteWithBytes(data string, position masker.MaskPosition, b []byte, startB, endB int) {

	start, end := masker.GetStartAndEnd(data, position)

	l := len([]rune(data))
	if l == len(data) {
		if start+end > l {
			if position.NotMaskLeast {
				copy(b[startB:endB], data)
			}
			copy(b[startB:endB], strings.Repeat("*", l))
		} else {
			copy(b[startB:startB+start], data[:start])
			copy(b[startB+start:endB-end], strings.Repeat("*", l-end-start))
			copy(b[endB-end:endB], data[l-end:])
		}
	} else {
		if start+end > l {
			if position.NotMaskLeast {
				copy(b[startB:endB], data)
			}
			copy(b[startB:endB], strings.Repeat("*", l))
		} else {
			copy(b[startB:], string([]rune(data)[:start]))
			copy(b[startB+len([]byte(string([]rune(data)[:start]))):], strings.Repeat("*", l-end-start))
			copy(b[endB-len([]byte(string([]rune(data)[l-end:]))):endB], string([]rune(data)[l-end:]))
		}
	}
	//for _, char := range position.UnmarkedChar {
	//	// strings.Index 返回 byte index
	//	if strings.Index(copyData, char) >= 0 {
	//		index := strings.Index(copyData, char)
	//		backIndex := index + len(char)
	//		if index > len(markData) || backIndex > len(markData) {
	//			logs.Errorf("sensitive_finder_engine: markData index out of range, data=%v, char=%v, index=%v",
	//				markData, char, index)
	//		} else {
	//			markData = markData[:index] + char + markData[backIndex:]
	//		}
	//	}
	//}
}
