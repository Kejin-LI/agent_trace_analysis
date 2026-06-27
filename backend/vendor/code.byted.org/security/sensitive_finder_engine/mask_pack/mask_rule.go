package mask_pack

import (
	"code.byted.org/security/sensitive_finder_engine"
	"code.byted.org/security/sensitive_finder_engine/masker"
)

const (
	MEmail  = "email"
	MMobile = "mobile"
)

type MaskFunc func(data string) string

func initMaskRule() {
	maskFunc = map[string]MaskFunc{
		MEmail: func(data string) string {
			return masker.MaskDataWithKind(data, sensitive_finder_engine.SensitiveKindEmailAddress)
		},
		MMobile: func(data string) string {
			return masker.MaskDataWithKind(data, sensitive_finder_engine.SensitiveKindMobilePhoneNumber)
		},
	}
}

func SetMaskFunc(kind string, function MaskFunc) {
	maskFunc[kind] = function
}

func maskStringByTag(tag string, str string) string {
	function, ok := maskFunc[tag]
	if ok {
		return function(str)
	}
	return masker.MaskDataWithKind(str, tag)
}
