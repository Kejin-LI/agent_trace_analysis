package masker

import (
	"bytes"
	"code.byted.org/security/sensitive_finder_engine/utils"
	"fmt"
	"strings"
)

// refer: https://bytedance.feishu.cn/docs/doccnbKXDaC4qxTbqUhx576cNhd#aZ2YBN
const (
	SensitiveKindTelephoneNumber    = "telephone_number"    // 固定电话
	SensitiveKindMobilePhoneNumber  = "mobile_phone_number" // 移动电话
	SensitiveKindIdCard             = "id_card"             // 身份证
	SensitiveKindBankAccountNumber  = "bank_account_number" // 银行账号
	SensitiveKindExitEntryPermit    = "exit_entry_permit"   // 港澳台通行证
	SensitiveKindChinesePassport    = "chinese_passport"    // CN护照
	SensitiveKindOfficerCertificate = "officer_certificate" // 军官证
	SensitiveKindForeignPassport    = "foreign_passport"    // 外国护照
	SensitiveKindEmailAddress       = "email_address"       // 电子邮箱
	SensitiveKindUnknown            = "unknown"             // 未知
	// custom - 暂无脱敏规范
	SensitiveKindPassword = "password" // 密码
	SensitiveKindAddress  = "address"  // 住址
	SensitiveKindSession  = "session"
	// business - 国际支付
	SensitiveKindCardBin         = "card_bin"
	SensitiveKindRealName        = "name"
	SensitiveKindBusinessAddress = "addr"
	SensitiveKindId              = "id"
	SensitiveKindCard            = "card"
	SensitiveKindCVC             = "cvc"
)

type DynamicPositionFunc func(s string) (int, int)

type MaskPosition struct {
	Start            int                 // 从开头数哪里开始脱敏
	End              int                 // 从末尾数到哪里结束脱敏
	UnmarkedChar     []string            // 忽略对哪些字符进行脱敏
	UnmarkedCharRune map[rune]bool       // 忽略对哪些字符进行脱敏
	SplitChar        string              // 按照间隔符号分组脱敏
	DynamicPosition  DynamicPositionFunc // 动态确认脱敏位置的函数
	NotMaskLeast     bool                // 长度小于开始长度是否脱敏
	StartOffset      int                 // start 偏移量
	EndOffset        int                 // end 偏移量
}

var SensitiveMarkPosition = map[string]MaskPosition{
	"telephone_number": {
		Start:           3,
		End:             2,
		DynamicPosition: mobilePosition,
	},
	"mobile_phone_number": {
		Start:           3,
		End:             2,
		DynamicPosition: mobilePosition,
	},
	"id_card": {
		Start: 1,
		End:   1,
	},
	"bank_account_number": {
		Start: 4,
		End:   0,
	},
	"exit_entry_permit": {
		Start: 2,
		End:   2,
	},
	"chinese_passport": {
		Start: 2,
		End:   2,
	},
	"officer_certificate": {
		Start: 2,
		End:   2,
	},
	"foreign_passport": {
		Start: 2,
		End:   2,
	},
	"email_address": {
		Start:        1,
		End:          0,
		UnmarkedChar: []string{"@"},
		UnmarkedCharRune: map[rune]bool{
			rune(64): true,
		},
	},
	"unknown": {
		Start: 2,
		End:   2,
	},
	"card_bin": {
		Start:        6,
		End:          0,
		NotMaskLeast: true,
	},
	"cvc": {
		Start: 0,
		End:   0,
	},
	"name": {
		Start:           1,
		End:             1,
		SplitChar:       " ",
		DynamicPosition: namePosition,
	},
	"id": {
		Start: 2,
		End:   2,
	},
	"addr": {
		Start: 6,
		End:   0,
	},
	"card": {
		Start:           0,
		End:             4,
		DynamicPosition: cardPosition,
	},
	"password": {
		Start: 0,
		End:   0,
	},
}

//var ht = map[rune]int{72: 1}

func MaskDataWithKind(data, kind string) string {
	position, ok := SensitiveMarkPosition[kind]
	if !ok {
		position = SensitiveMarkPosition[SensitiveKindUnknown]
	}
	return MaskDataCustom(data, position)
}

func GetStartAndEnd(data string, position MaskPosition) (int, int) {
	var start, end int
	if position.DynamicPosition == nil {
		start, end = position.Start, position.End
	} else {
		start, end = position.DynamicPosition(data)
	}
	return start + position.StartOffset, end + position.EndOffset
}

func MaskDataCustom(data string, position MaskPosition) string {

	//utils.EmitCounter(utils.MetricMarkSensitive, 1, utils.MaskerCustomMetric)

	if position.SplitChar != "" {
		if segs := strings.Split(data, position.SplitChar); len(segs) > 1 {
			tmp := make([]string, len(segs))
			for idx, str := range segs {
				tmp[idx] = MaskDataCustom(str, position)
			}
			return strings.Join(tmp, position.SplitChar)
		}
	}

	start, end := GetStartAndEnd(data, position)
	if len(data) == len([]rune(data)) {
		if start+end > len(data) {
			if position.NotMaskLeast {
				return data
			} else {
				return strings.Repeat("*", len(data))
			}
		} else {
			b := make([]byte, len(data))
			copy(b[:start], data[:start])
			copy(b[start:len(data)-end], StringRepeatWithUnrepeatedChar(data[start:len(data)-end], "*", position.UnmarkedChar))
			//copy(b[start:len(data)-end], strings.Repeat("*", len(data)-end-start))
			copy(b[len(data)-end:], data[len(data)-end:])
			return utils.BytesToString(b)
		}
	} else {
		l := len([]rune(data))
		if start+end > l {
			if position.NotMaskLeast {
				return data
			} else {
				return strings.Repeat("*", len([]rune(data)))
			}
		} else {
			var b bytes.Buffer
			b.WriteString(string([]rune(data)[:start]))
			StringRepeatWithUnrepeatedCharRune([]rune(data), "*", position.UnmarkedChar, start, len([]rune(data))-end, &b)
			b.WriteString(string([]rune(data)[len([]rune(data))-end:]))
			return b.String()
		}
	}
}

//自定义脱敏函数接口
type MaskFunc func(data string, position MaskPosition) string

// 长度不变
func MaskDataCustomByte(data string, position MaskPosition) string {

	// fixme: do batch emit
	//utils.EmitCounter(utils.MetricMarkSensitive, 1, utils.MaskerCustomByteMetric)

	if position.SplitChar != "" {
		if segs := strings.Split(data, position.SplitChar); len(segs) > 1 {
			tmp := make([]string, len(segs))
			for idx, str := range segs {
				tmp[idx] = MaskDataCustomByte(str, position)
			}
			return strings.Join(tmp, position.SplitChar)
		}
	}

	start, end := GetStartAndEnd(data, position)
	var markData string
	if start+end > len([]rune(data)) {
		if position.NotMaskLeast {
			return data
		}
		markData = strings.Repeat("*", len(data))
	} else {
		if position.UnmarkedCharRune != nil && len(position.UnmarkedCharRune) > 0 {
			markData = fmt.Sprintf("%s%s%s", string([]rune(data)[:start]),
				RuneRepeatWithUnrepeatedChar([]rune(data)[start:len([]rune(data))-end], "*", position.UnmarkedCharRune),
				string([]rune(data)[len([]rune(data))-end:]))
		} else {
			markData = fmt.Sprintf("%s%s%s", string([]rune(data)[:start]),
				strings.Repeat("*", len(string([]rune(data)[start:len([]rune(data))-end]))),
				string([]rune(data)[len([]rune(data))-end:]))
		}
	}
	return markData
}

// todo: fix string join
func StringRepeatWithUnrepeatedCharRune(data []rune, c string, chars []string, start, end int, b *bytes.Buffer) {
	ht := make(map[string]int, len(chars))
	for _, char := range chars {
		ht[char] = 1
	}
	for _, char := range data[start:end] {
		_, ok := ht[string(char)]
		if ok {
			b.WriteRune(char)
		} else {
			b.WriteString(c)
		}
	}
}

func StringRepeatWithUnrepeatedChar(data string, c string, chars []string) string {
	ht := make(map[string]int, len(chars))
	for _, char := range chars {
		ht[char] = 1
	}
	var b bytes.Buffer
	for _, char := range []rune(data) {
		_, ok := ht[string(char)]
		if ok {
			b.WriteRune(char)
		} else {
			b.WriteString(c)
		}
	}
	return b.String()
}

func RuneRepeatWithUnrepeatedChar(data []rune, c string, chars map[rune]bool) string {
	var b bytes.Buffer
	for _, char := range data {
		_, ok := chars[char]
		if ok {
			b.WriteRune(char)
		} else {
			b.WriteString(c)
		}
	}
	return b.String()
}

func namePosition(s string) (int, int) {
	var end int
	if len([]rune(s)) <= 3 {
		end = 0
	} else {
		end = len([]rune(s)) - 4
	}
	return 1, end
}

func cardPosition(s string) (int, int) {
	var end int
	if len(s) < 8 {
		end = 2
	} else {
		end = 4
	}
	return 0, end
}

func mobilePosition(s string) (int, int) {
	if len(s) <= 3 {
		return 1, 0
	}
	var start int
	var suffixLen int
	if strings.Index(s, " ") > 0 {
		segments := strings.SplitN(s, " ", 2)
		if len(segments) == 2 {
			start = len(segments[0]) + 1
			suffixLen = len(segments[1])
		}
	} else {
		if strings.HasPrefix(s, "+") {
			start = 3
			suffixLen = len(s) - start
		} else if strings.HasPrefix(s, "(+") {
			start = strings.Index(s, ")") + 1
			suffixLen = len(s) - start
		} else {
			// avoid copy
			return 3, 2
		}
	}
	if suffixLen < 6 {
		return 1 + start, 1
	} else if suffixLen < 8 {
		return 2 + start, 2
	} else {
		return 3 + start, 2
	}
}
