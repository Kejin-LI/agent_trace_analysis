package sensitive_finder_engine

import "code.byted.org/security/sensitive_finder_engine/masker"

// copy from code.byted.org/security/sensitive_finder_engine/masker
const (
	SensitiveKindTelephoneNumber    = masker.SensitiveKindTelephoneNumber
	SensitiveKindMobilePhoneNumber  = masker.SensitiveKindMobilePhoneNumber
	SensitiveKindIdCard             = masker.SensitiveKindIdCard
	SensitiveKindBankAccountNumber  = masker.SensitiveKindBankAccountNumber
	SensitiveKindExitEntryPermit    = masker.SensitiveKindExitEntryPermit
	SensitiveKindChinesePassport    = masker.SensitiveKindChinesePassport
	SensitiveKindOfficerCertificate = masker.SensitiveKindOfficerCertificate
	SensitiveKindForeignPassport    = masker.SensitiveKindForeignPassport
	SensitiveKindEmailAddress       = masker.SensitiveKindEmailAddress
	SensitiveKindUnknown            = masker.SensitiveKindUnknown
	// custom - 暂无脱敏规范
	SensitiveKindPassword = masker.SensitiveKindPassword
	SensitiveKindAddress  = masker.SensitiveKindAddress
	SensitiveKindSession  = masker.SensitiveKindSession
	// business - 国际支付
	SensitiveKindCardBin         = masker.SensitiveKindCardBin
	SensitiveKindRealName        = masker.SensitiveKindRealName
	SensitiveKindBusinessAddress = masker.SensitiveKindBusinessAddress
	SensitiveKindId              = masker.SensitiveKindId
	SensitiveKindCard            = masker.SensitiveKindCard
	SensitiveKindCVC             = masker.SensitiveKindCVC
)

type MaskPosition = masker.MaskPosition
type MaskFunc = masker.MaskFunc
