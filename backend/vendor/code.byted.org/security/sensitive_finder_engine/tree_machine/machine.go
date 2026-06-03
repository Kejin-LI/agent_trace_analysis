package tree_machine

import (
	"encoding/json"
	"fmt"
	"strings"

	"code.byted.org/security/sensitive_finder_engine/regexp"
	"code.byted.org/security/sensitive_finder_engine/static"
	"code.byted.org/security/sensitive_finder_engine/string_finder/goahocorasick"
	"code.byted.org/security/sensitive_finder_engine/utils"
)

func getMatcher(kinds []string, customKey, customValue map[string][]string) (*matcher, error) {
	// fetch key
	var r struct {
		Data map[string][]string `json:"data"`
	}
	data, err := static.Asset("static/sensitive_engine_rules.json")
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(data, &r)
	if err != nil {
		return nil, err
	}
	// group system key
	keyMap := r.Data
	keyTreeMap := make(map[string]int)
	kindMap := make(map[string]string)
	for key, values := range keyMap {
		if kinds != nil && len(kinds) != 0 && !utils.StrInSlice(key, kinds) {
			continue
		}
		for _, value := range values {
			kindMap[value] = key
			keyTreeMap[value] = 1
		}
	}
	// group custom key
	for kind, keys := range customKey {
		if kinds != nil && len(kinds) != 0 && !utils.StrInSlice(kind, kinds) {
			continue
		}
		for _, key := range keys {
			kindMap[key] = kind
			keyTreeMap[key] = 1
			_, ok := keyMap[kind]
			if !ok {
				keyMap[kind] = []string{}
			}
			keyMap[kind] = append(keyMap[kind], key)
		}
	}
	// generate key
	var keyTreeSlice []string
	for key := range keyTreeMap {
		keyTreeSlice = append(keyTreeSlice, key)
	}
	// generate key machine
	m := new(goahocorasick.Machine)
	if err = m.Build(keyTreeSlice); err != nil {
		return nil, err
	}
	// generate key pattern
	keyPattern, err := regexp.Compile(fmt.Sprintf("(%s)", strings.Join(keyTreeSlice, "|")))
	if err != nil {
		return nil, err
	}
	// generate value pattern
	km, err := getRegexp(kinds, customValue)
	if err != nil {
		return nil, err
	}
	// todo: check kind in key and value
	return &matcher{
		m:          m,
		keyMap:     keyMap,
		kindMap:    kindMap,
		KeySlice:   keyTreeSlice,
		keyPattern: keyPattern,
		keyMatcher: km,
	}, nil
}

type matcher struct {
	m          *goahocorasick.Machine    // key match
	keyMap     map[string][]string       // kind-key map
	kindMap    map[string]string         // key-kind map
	KeySlice   []string                  // key slice
	keyPattern *regexp.Regexp            // key match pattern
	keyMatcher map[string]*regexp.Regexp // value match pattern
}

type SensitiveRule struct {
	Kind   string   `json:"kind"`
	Keys   []string `json:"keys"`
	Values []string `json:"values"`
	RuleID int      `json:"rule_id"`
	TagID  int      `json:"tag_id"`
}

type SensitiveRuleData struct {
	Data []SensitiveRule `json:"data"`
}

func getRegexp(kinds []string, customValue map[string][]string) (map[string]*regexp.Regexp, error) {

	rules := make(map[string]*regexp.Regexp)
	handleKind := make(map[string]int)

	var sensitiveRules SensitiveRuleData
	data, err := static.Asset("static/sensitive_rules.json")
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(data, &sensitiveRules)
	if err != nil {
		return nil, err
	}

	for _, r := range sensitiveRules.Data {

		// kind 过滤
		if kinds != nil && len(kinds) != 0 && !utils.StrInSlice(r.Kind, kinds) {
			continue
		}

		values := r.Values
		if customValue != nil {
			customValues, ok := customValue[r.Kind]
			if ok {
				for _, v := range customValues {
					values = append(values, v)
				}
				handleKind[r.Kind] = 1
			}
		}

		rrr1 := generateRule(r.Kind, values)
		if len(values) == 0 || rrr1 == nil {
			continue
		}

		if r.Kind == "telephone_number" {
			rules["mobile_phone_number"] = rrr1
		}
		rules[r.Kind] = rrr1
	}

	// custom rule
	for key, value := range customValue {
		_, ok := handleKind[key]
		if ok {
			continue
		}
		rule := generateRule(key, value)
		if rule == nil {
			continue
		}
		rules[key] = rule
	}

	return rules, nil
}

func generateRule(kind string, values []string) *regexp.Regexp {
	var nValues []string
	for _, v := range values {
		nValues = append(nValues, fmt.Sprintf("(%s)", v))
	}

	/*
		eg:
		abc=233
		abc: 233
		abc": "233
		abc\": \"233
	*/
	var newRule string
	if kind == "telephone_number" || kind == "email_address" || strings.Contains(kind, "right_space") {
		newRule = fmt.Sprintf("^([\\w_-]{0,8}\\\\?[\"'`]?\\s{0,5}[:=]?\\s{0,5}(map\\[)?(\\[\\]string\\{)?\\[?u?\\\\?[\"'`]?(%s))",
			strings.Join(nValues, "|"))
	} else {
		newRule = fmt.Sprintf("^(s?\\\\?[\"'`]?\\s{0,5}[:=]?\\s{0,5}(map\\[)?(\\[\\]string\\{)?\\[?u?\\\\?[\"'`]?(%s))",
			strings.Join(nValues, "|"))
	}

	// 保存新的规则
	rrr1, err := regexp.Compile(newRule)
	if err != nil {
		utils.LogsErrorf("parse regexp error, reason=%v, exp=%v", err, newRule)
	}

	return rrr1
}
