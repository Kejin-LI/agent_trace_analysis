package sensitive_finder_engine

import (
	"encoding/json"

	"code.byted.org/security/sensitive_finder_engine/utils"
)

const chineseRules = `
[
  {
    "id": 1,
    "kind": "id_card",
    "exp": "^[1-9]\\d{5}(18|19|20)\\d{2}((0[1-9])|(1[0-2]))(([0-2][1-9])|10|20|30|31)\\d{3}[0-9Xx]$",
    "special_char": [],
    "contains_letter": true,
    "contains_chinese": false,
	"func": "match"
  },
  {
    "id": 2,
    "kind": "email_address",
    "exp": "^([A-Za-z0-9_\\-\\.])+\\@([A-Za-z0-9_\\-\\.])+\\.([A-Za-z]{2,8})$",
    "special_char": ["_", "-", ".", "@"],
    "contains_letter": true,
    "contains_chinese": false,
	"func": "match"
  },
  {
    "id":3,
    "kind": "loc_addr",
    "exp": "((.{2,6}?(省|自治区))|(.{1,6}?(市|自治区|自治州))|(.{1,6}?(县|区|镇|乡))){1,3}((.{1,6}(路|街|里|街道|村|屯|组))|(.{1,6}(小区|大厦|号|广场))){1,3}((.{1,6}(号楼))|(.{1,6}(单元))|(.{1,6}(层|室|户))){0,3}",
    "special_char": [],
    "contains_letter": true,
    "contains_chinese": true,
	"func": "find"
  },
  {
    "id": 4,
    "kind": "mobile_phone_number",
    "exp": "^[1](([3][0-9])|([4][5-9])|([5][0-3,5-9])|([6][5,6])|([7][0-8])|([8][0-9])|([9][1,8,9]))[0-9]{8}$",
    "special_char": [],
    "contains_letter": false,
    "contains_chinese": false,
	"func": "match"
  },
  {
    "id": 5,
    "kind": "telephone_number",
    "exp": "^0\\d{2,3}-?\\d{7,8}\\d{4}?$",
    "special_char": ["-"],
    "contains_letter": false,
    "contains_chinese": false,
	"func": "match"
  },
  {
    "id": 6,
    "kind": "bank_account_number",
    "exp": "^\\d{16,19}$",
    "special_char": [],
    "contains_letter": false,
    "contains_chinese": false,
	"func": "match"
  },
  {
    "id": 7,
    "kind": "exit_entry_permit",
    "exp": "^([Cc][a-zA-Z0-9]\\d{7})|([Ll]\\d{8})$",
    "special_char": [],
    "contains_letter": true,
    "contains_chinese": false,
	"func": "match"
  },
  {
    "id": 8,
    "kind": "chinese_passport",
    "exp": "^((EHeh)[a-zA-Z0-9]\\d{7})|([EDSPGKHedspgkh]\\d{8})|([(SE)|(DE)|(PE)|(MA)|(KJ)|(se)|(de)|(pe)|(ma)|(kj)]\\d{7})$",
    "special_char": [],
    "contains_letter": true,
    "contains_chinese": false,
	"func": "match"
  },
  {
    "id": 9,
    "kind": "foreign_passport",
    "exp": "^[A-Za-z0-9]{7}$",
    "special_char": [],
    "contains_letter": true,
    "contains_chinese": false,
	"func": "match"
  },
  {
    "id": 10,
    "kind": "officer_certificate",
    "exp": "^[0-9a-zA-Z]{4,8}$",
    "special_char": [],
    "contains_letter": true,
    "contains_chinese": false,
	"func": "match"
  },
  {
    "id": 11,
    "kind": "unknown",
    "exp": "^[0-9a-zA-Z]{4,32}$",
    "special_char": ["-", "_"],
    "contains_letter": true,
    "contains_chinese": false,
	"func": "match"
  }
]
`

var chineseFinderRules []*ChineseRulePattern

func init() {
	err := json.Unmarshal([]byte(chineseRules), &chineseFinderRules)
	if err != nil {
		utils.LogsErrorf("init chinese rules failed, reason=%v", err)
	}

	initLogRateLimit()
	// fmt.Printf("%+v \n", chineseFinderRules)
}
