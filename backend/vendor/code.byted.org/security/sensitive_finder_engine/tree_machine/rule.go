package tree_machine

import (
	"code.byted.org/security/sensitive_finder_engine/masker"
)

func InitConnect() {
	connect = NewTree()
	a := connect.AddNode1([]interface{}{Pattern{
		Elements: []uint8("s"),
		IsAnti:   false,
		IsNumber: false,
		IsLetter: false,
		IsSpace:  false,
		IsAny:    false,
		MinCount: 0,
		MaxCount: 1,
	}, Pattern{
		Elements: []uint8("\\"),
		IsAnti:   false,
		IsNumber: false,
		IsLetter: false,
		IsSpace:  false,
		IsAny:    false,
		MinCount: 0,
		MaxCount: 1,
	}, Pattern{
		Elements: []uint8("\"'`"),
		IsAnti:   false,
		IsNumber: false,
		IsLetter: false,
		IsSpace:  false,
		IsAny:    false,
		MinCount: 0,
		MaxCount: 1,
	}, Pattern{
		Elements: nil,
		IsAnti:   false,
		IsNumber: false,
		IsLetter: false,
		IsSpace:  true,
		IsAny:    false,
		MinCount: 0,
		MaxCount: 5,
	}, Pattern{
		Elements: []uint8(":="),
		IsAnti:   false,
		IsNumber: false,
		IsLetter: false,
		IsSpace:  false,
		IsAny:    false,
		MinCount: 0,
		MaxCount: 1,
	}, Pattern{
		Elements: nil,
		IsAnti:   false,
		IsNumber: false,
		IsLetter: false,
		IsSpace:  true,
		IsAny:    false,
		MinCount: 0,
		MaxCount: 5,
	}, []interface{}{
		"map[", "[]string{",
		Pattern{
			Elements: []uint8("\\"),
			IsAnti:   false,
			IsNumber: false,
			IsLetter: false,
			IsSpace:  false,
			IsAny:    false,
			MinCount: 0,
			MaxCount: 1,
		},
	}, Pattern{
		Elements: []uint8("["),
		IsAnti:   false,
		IsNumber: false,
		IsLetter: false,
		IsSpace:  false,
		IsAny:    false,
		MinCount: 0,
		MaxCount: 1,
	}, Pattern{
		Elements: []uint8("u"),
		IsAnti:   false,
		IsNumber: false,
		IsLetter: false,
		IsSpace:  false,
		IsAny:    false,
		MinCount: 0,
		MaxCount: 1,
	}, Pattern{
		Elements: []uint8("\\"),
		IsAnti:   false,
		IsNumber: false,
		IsLetter: false,
		IsSpace:  false,
		IsAny:    false,
		MinCount: 0,
		MaxCount: 1,
	}, Pattern{
		Elements: []uint8("\"'`"),
		IsAnti:   false,
		IsNumber: false,
		IsLetter: false,
		IsSpace:  false,
		IsAny:    false,
		MinCount: 0,
		MaxCount: 1,
	}}, "")
	for _, aa := range a {
		aa.TreeMode = modeNodeConnect
		aa.IsLeaf = true
	}
}

func getIDCardTree() *TreeNode {
	t := NewTree()
	bb := t.AddNode1([]interface{}{
		Pattern{
			Elements: []uint8("123456789"),
			IsAnti:   false,
			IsNumber: true,
			IsLetter: false,
			IsSpace:  false,
			IsAny:    false,
			MinCount: 1,
			MaxCount: 1,
		}, Pattern{
			Elements: nil,
			IsAnti:   false,
			IsNumber: true,
			IsLetter: false,
			IsSpace:  false,
			IsAny:    false,
			MinCount: 5,
			MaxCount: 5,
		}, []interface{}{
			"18", "19", "20",
		}, Pattern{
			Elements: nil,
			IsAnti:   false,
			IsNumber: true,
			IsLetter: false,
			IsSpace:  false,
			IsAny:    false,
			MinCount: 2,
			MaxCount: 2,
		}, []interface{}{
			"01", "02", "03", "04", "05", "06", "07", "08", "09",
			"10", "11", "12",
		}, []interface{}{
			"01", "02", "03", "04", "05", "06", "07", "08", "09",
			"11", "12", "13", "14", "15", "16", "17", "18", "19",
			"21", "22", "23", "24", "25", "26", "27", "28", "29",
			"10", "20", "30", "31",
		}, Pattern{
			Elements: nil,
			IsAnti:   false,
			IsNumber: true,
			IsLetter: false,
			IsSpace:  false,
			IsAny:    false,
			MinCount: 3,
			MaxCount: 3,
		}, Pattern{
			Elements: []uint8("xX"),
			IsAnti:   false,
			IsNumber: true,
			IsLetter: false,
			IsSpace:  false,
			IsAny:    false,
			MinCount: 1,
			MaxCount: 1,
		},
	}, masker.SensitiveKindIdCard)
	for _, b := range bb {
		b.TreeMode = modeNodeValue
		b.IsLeaf = true
	}
	return t
}

func getBankCardTree() *TreeNode {
	t := NewTree()
	bb := t.AddNode1([]interface{}{"62", Pattern{
		Elements: nil,
		IsAnti:   false,
		IsNumber: true,
		IsLetter: false,
		IsSpace:  false,
		IsAny:    false,
		MinCount: 14,
		MaxCount: 17,
	}}, masker.SensitiveKindBankAccountNumber)
	for _, b := range bb {
		b.TreeMode = modeNodeValue
		b.IsLeaf = true
	}
	return t
}

func getPassTree() *TreeNode {
	t := NewTree()
	bb := t.AddNode1([]interface{}{[]interface{}{
		[]interface{}{
			"1", Pattern{
				Elements: []uint8("45"),
				IsAnti:   false,
				IsNumber: false,
				IsLetter: false,
				IsSpace:  false,
				IsAny:    false,
				MinCount: 1,
				MaxCount: 1,
			}, Pattern{
				Elements: nil,
				IsAnti:   false,
				IsNumber: true,
				IsLetter: false,
				IsSpace:  false,
				IsAny:    false,
				MinCount: 7,
				MaxCount: 7,
			},
		},
		[]interface{}{
			Pattern{
				Elements: []uint8("PpSs"),
				IsAnti:   false,
				IsNumber: false,
				IsLetter: false,
				IsSpace:  false,
				IsAny:    false,
				MinCount: 1,
				MaxCount: 1,
			}, Pattern{
				Elements: nil,
				IsAnti:   false,
				IsNumber: true,
				IsLetter: false,
				IsSpace:  false,
				IsAny:    false,
				MinCount: 7,
				MaxCount: 7,
			},
		},
		[]interface{}{
			Pattern{
				Elements: []uint8("SsGgEe"),
				IsAnti:   false,
				IsNumber: false,
				IsLetter: false,
				IsSpace:  false,
				IsAny:    false,
				MinCount: 1,
				MaxCount: 1,
			}, Pattern{
				Elements: nil,
				IsAnti:   false,
				IsNumber: true,
				IsLetter: false,
				IsSpace:  false,
				IsAny:    false,
				MinCount: 8,
				MaxCount: 8,
			},
		},
		[]interface{}{
			[]interface{}{
				"Gg", "Tt", "Ss", "Ll", "Qq", "Dd", "Aa", "Ff",
			}, Pattern{
				Elements: nil,
				IsAnti:   false,
				IsNumber: true,
				IsLetter: false,
				IsSpace:  false,
				IsAny:    false,
				MinCount: 8,
				MaxCount: 8,
			},
		},
		[]interface{}{
			[]interface{}{
				"H", "h", "M", "m",
			}, Pattern{
				Elements: nil,
				IsAnti:   false,
				IsNumber: true,
				IsLetter: false,
				IsSpace:  false,
				IsAny:    false,
				MinCount: 8,
				MaxCount: 10,
			},
		},
	}}, masker.SensitiveKindChinesePassport)
	for _, b := range bb {
		b.TreeMode = modeNodeValue
		b.IsLeaf = true
	}
	return t
}

func getPhoneTree() *TreeNode {
	t := NewTree()
	bb := t.AddNode1([]interface{}{"1", []interface{}{
		"30", "31", "32", "33", "34", "35", "36", "37", "38", "39",
		"45", "46", "47", "48", "49",
		"50", "51", "52", "53", "55", "56", "57", "58", "59",
		"65", "66",
		"70", "71", "72", "73", "74", "75", "76", "77", "78",
		"80", "81", "82", "83", "84", "85", "86", "87", "88", "89",
		"91", "98", "99",
	}, Pattern{
		Elements: nil,
		IsAnti:   false,
		IsNumber: true,
		IsLetter: false,
		IsSpace:  false,
		IsAny:    false,
		MinCount: 8,
		MaxCount: 8,
	}}, masker.SensitiveKindMobilePhoneNumber)
	for _, b := range bb {
		b.TreeMode = modeNodeValue
		b.IsLeaf = true
	}
	return t
}

func getPhoneTree1() *TreeNode {
	t := NewTree()
	bb := t.AddNode1([]interface{}{[]interface{}{
		[]interface{}{
			"0", Pattern{
				Elements: nil,
				IsAnti:   false,
				IsNumber: true,
				IsLetter: false,
				IsSpace:  false,
				IsAny:    false,
				MinCount: 2,
				MaxCount: 3,
			},
		}, "400",
	}, Pattern{
		Elements: []uint8("-"),
		IsAnti:   false,
		IsNumber: false,
		IsLetter: false,
		IsSpace:  false,
		IsAny:    false,
		MinCount: 0,
		MaxCount: 1,
	}, Pattern{
		Elements: nil,
		IsAnti:   false,
		IsNumber: true,
		IsLetter: false,
		IsSpace:  false,
		IsAny:    false,
		MinCount: 3,
		MaxCount: 3,
	}, Pattern{
		Elements: []uint8("-"),
		IsAnti:   false,
		IsNumber: false,
		IsLetter: false,
		IsSpace:  false,
		IsAny:    false,
		MinCount: 0,
		MaxCount: 1,
	}, Pattern{
		Elements: nil,
		IsAnti:   false,
		IsNumber: true,
		IsLetter: false,
		IsSpace:  false,
		IsAny:    false,
		MinCount: 3,
		MaxCount: 4,
	}}, masker.SensitiveKindMobilePhoneNumber)
	for _, b := range bb {
		b.TreeMode = modeNodeValue
		b.IsLeaf = true
	}
	return t
}

func getSessionTree() *TreeNode {
	t := NewTree()
	bb := t.AddNode1([]interface{}{Pattern{
		Elements: []uint8("?<>\\'\"!@%#$~&*():;"),
		IsAnti:   true,
		IsNumber: false,
		IsLetter: false,
		IsSpace:  true,
		IsAny:    false,
		MinCount: 6,
		MaxCount: 32,
	}}, masker.SensitiveKindSession)
	for _, b := range bb {
		b.TreeMode = modeNodeValue
		b.IsLeaf = true
	}
	return t
}

func getEmailTree() *TreeNode {
	t := NewTree()
	// todo: 支持多级域名
	bb := t.AddNode1([]interface{}{Pattern{
		Elements: []uint8("_-."),
		IsAnti:   false,
		IsNumber: true,
		IsLetter: true,
		IsSpace:  false,
		IsAny:    false,
		MinCount: 1,
		MaxCount: 100,
	}, "@", Pattern{
		Elements: []uint8("_-"),
		IsAnti:   false,
		IsNumber: true,
		IsLetter: true,
		IsSpace:  false,
		IsAny:    false,
		MinCount: 1,
		MaxCount: 100,
	}, ".", Pattern{
		Elements: nil,
		IsAnti:   false,
		IsNumber: true,
		IsLetter: true,
		IsSpace:  false,
		IsAny:    false,
		MinCount: 2,
		MaxCount: 8,
	}}, masker.SensitiveKindEmailAddress)
	for _, b := range bb {
		b.TreeMode = modeNodeValue
		b.IsLeaf = true
	}
	return t
}

func getPasswordTree() *TreeNode {
	t := NewTree()
	bb := t.AddNode1([]interface{}{Pattern{
		Elements: []uint8("-=\\[;,./~!@#$%^*()_+}{:?"),
		IsAnti:   false,
		IsNumber: true,
		IsLetter: true,
		IsSpace:  false,
		IsAny:    false,
		MinCount: 6,
		MaxCount: 32,
	}}, masker.SensitiveKindPassword)
	for _, b := range bb {
		b.TreeMode = modeNodeValue
		b.IsLeaf = true
	}
	return t
}

// fixme: not support address
//func getAddressTree() *TreeNode {
//	t := NewTree()
//	bb := t.AddNode1([]interface{}{
//		[]interface{}{
//			Pattern{
//				Elements: nil,
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  false,
//				IsAny:    true,
//				MinCount: 1,
//				MaxCount: 6,
//			},
//			Pattern{
//				Elements: []rune("省"),
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  false,
//				IsAny:    true,
//				MinCount: 0,
//				MaxCount: 1,
//			},
//			Pattern{
//				Elements: nil,
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  false,
//				IsAny:    true,
//				MinCount: 1,
//				MaxCount: 6,
//			},
//			Pattern{
//				Elements: []rune("市"),
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  false,
//				IsAny:    true,
//				MinCount: 0,
//				MaxCount: 1,
//			},
//			Pattern{
//				Elements: nil,
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  false,
//				IsAny:    true,
//				MinCount: 1,
//				MaxCount: 6,
//			},
//			Pattern{
//				Elements: []rune("区镇"),
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  false,
//				IsAny:    true,
//				MinCount: 0,
//				MaxCount: 1,
//			},
//			Pattern{
//				Elements: nil,
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  false,
//				IsAny:    true,
//				MinCount: 1,
//				MaxCount: 6,
//			},
//			Pattern{
//				Elements: []rune("村乡"),
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  false,
//				IsAny:    true,
//				MinCount: 0,
//				MaxCount: 1,
//			},
//			Pattern{
//				Elements: nil,
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  false,
//				IsAny:    true,
//				MinCount: 1,
//				MaxCount: 6,
//			},
//			Pattern{
//				Elements: []rune("路街里组屯"),
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  false,
//				IsAny:    true,
//				MinCount: 0,
//				MaxCount: 1,
//			},
//			Pattern{
//				Elements: nil,
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  false,
//				IsAny:    true,
//				MinCount: 1,
//				MaxCount: 6,
//			},
//			Pattern{
//				Elements: []rune("路街里组屯"),
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  false,
//				IsAny:    true,
//				MinCount: 0,
//				MaxCount: 1,
//			},
//			Pattern{
//				Elements: nil,
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  false,
//				IsAny:    true,
//				MinCount: 1,
//				MaxCount: 6,
//			},
//			Pattern{
//				Elements: []rune("号区楼厦"),
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  false,
//				IsAny:    true,
//				MinCount: 0,
//				MaxCount: 1,
//			},
//			Pattern{
//				Elements: nil,
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  false,
//				IsAny:    true,
//				MinCount: 1,
//				MaxCount: 6,
//			},
//			Pattern{
//				Elements: []rune("元"),
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  false,
//				IsAny:    true,
//				MinCount: 0,
//				MaxCount: 1,
//			},
//			Pattern{
//				Elements: nil,
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  false,
//				IsAny:    true,
//				MinCount: 0,
//				MaxCount: 6,
//			},
//			Pattern{
//				Elements: []rune("层市户号"),
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  false,
//				IsAny:    true,
//				MinCount: 0,
//				MaxCount: 1,
//			},
//		},
//		[]interface{}{
//			Pattern{
//				Elements: nil,
//				IsAnti:   false,
//				IsNumber: true,
//				IsLetter: false,
//				IsSpace:  false,
//				IsAny:    false,
//				MinCount: 1,
//				MaxCount: 4,
//			},
//			Pattern{
//				Elements: nil,
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  true,
//				IsAny:    false,
//				MinCount: 1,
//				MaxCount: 1,
//			},
//			Pattern{
//				Elements: nil,
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: true,
//				IsSpace:  false,
//				IsAny:    false,
//				MinCount: 2,
//				MaxCount: 16,
//			},
//			Pattern{
//				Elements: nil,
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  true,
//				IsAny:    false,
//				MinCount: 0,
//				MaxCount: 1,
//			},
//			Pattern{
//				Elements: nil,
//				IsAnti:   false,
//				IsNumber: true,
//				IsLetter: false,
//				IsSpace:  false,
//				IsAny:    false,
//				MinCount: 2,
//				MaxCount: 16,
//			},
//			Pattern{
//				Elements: nil,
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  true,
//				IsAny:    false,
//				MinCount: 0,
//				MaxCount: 1,
//			},
//			[]interface{}{
//				"Avenue", "Lane", "Road", "Boulevard", "Drive", "Street",
//				"Ave", "Dr", "Rd", "Blvd", "Ln", "St",
//			},
//			Pattern{
//				Elements: nil,
//				IsAnti:   false,
//				IsNumber: false,
//				IsLetter: false,
//				IsSpace:  false,
//				IsAny:    true,
//				MinCount: 0,
//				MaxCount: 1,
//			},
//		},
//	}, sensitive_finder_engine.SensitiveKindAddress)
//	for _, b := range bb {
//		b.TreeMode = modeNodeValue
//		b.IsLeaf = true
//	}
//	return t
//}
