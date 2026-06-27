package tree_machine

import (
	"code.byted.org/security/sensitive_finder_engine/masker"
	"time"
)

var (
	connect      *TreeNode              // 连接符号
	rootValue    map[string][]*TreeNode // 值的符号
	cycle        = 1 * time.Hour
	useLeftSpace = true
	otherKind    = []string{
		"mobile_phone_number",
		"email_address",
	}
)

// 用于init
func Build() {}

func init() {
	InitConnect()
	InitRootValue()
}

func InitRootValue() {
	rootValue = map[string][]*TreeNode{
		masker.SensitiveKindEmailAddress: {
			getEmailTree(),
		},
		masker.SensitiveKindIdCard: {
			getIDCardTree(),
		},
		masker.SensitiveKindBankAccountNumber: {
			getBankCardTree(),
		},
		masker.SensitiveKindPassword: {
			getPasswordTree(),
		},
		masker.SensitiveKindSession: {
			getSessionTree(),
		},
		masker.SensitiveKindMobilePhoneNumber: {
			getPhoneTree(), getPhoneTree1(),
		},
		masker.SensitiveKindChinesePassport: {
			getPassTree(),
		},
		"others": {
			getEmailTree(), getPhoneTree(), getPhoneTree1(),
		},
	}
	//appendNode(rootValue[sensitive_finder_engine.SensitiveKindMobilePhoneNumber], getPhoneTree1())
	//appendNode(rootValue[sensitive_finder_engine.SensitiveKindAddress], getAddressTree())
}

func UseLeftSpace(flag bool) {
	useLeftSpace = flag
}
