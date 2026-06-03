package sensitive_finder_engine

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Knetic/govaluate"
	jsoniter "github.com/json-iterator/go"

	"code.byted.org/security/sensitive_finder_engine/masker"
	"code.byted.org/security/sensitive_finder_engine/static"
	"code.byted.org/security/sensitive_finder_engine/utils"
)

const (
	KindPlaintext        = "plaintext"
	KindCiphertext       = "ciphertext"
	KindDesensitizedText = "desensitized_text"
	KindBlankText        = "blank_text"
	KindUnknownText      = "unknown_text"
	RuleFlagPlain        = "p"
	RuleFlagCipher       = "c"
	RuleFlagBlank        = "b"
	RuleFlagSensitive    = "s"
	RuleFlagAll          = "a"
)

var TagIDToKind = map[int]string{
	19:  masker.SensitiveKindAddress,
	20:  masker.SensitiveKindTelephoneNumber,
	21:  masker.SensitiveKindEmailAddress,
	23:  masker.SensitiveKindIdCard,
	24:  masker.SensitiveKindChinesePassport,
	29:  masker.SensitiveKindPassword,
	59:  masker.SensitiveKindTelephoneNumber,
	60:  masker.SensitiveKindEmailAddress,
	62:  masker.SensitiveKindIdCard,
	63:  masker.SensitiveKindChinesePassport,
	68:  masker.SensitiveKindPassword,
	73:  masker.SensitiveKindBankAccountNumber,
	230: masker.SensitiveKindSession,
}

var matchValueToJson bool

func SetMatchJsonValue(flag bool) {
	matchValueToJson = flag
}

func SetTagIDToKind(tag2kind map[int]string) {
	TagIDToKind = tag2kind
}

type Element struct {
	Key         string
	Value       []string
	Description string
	Type        string
	Path        string
}

type RuleEngine struct {
	token string
	rules []Rule
}

type RuleElement struct {
	Index    string         `json:"index"`
	Key      string         `json:"key"`
	Operator string         `json:"operator"`
	Value    string         `json:"value"`
	Pattern  *regexp.Regexp `json:"-"`
}

type Rule struct {
	ID                  int                            `json:"id"`
	Name                string                         `json:"name"`
	Field               int                            `json:"field"`
	Rules               []RuleElement                  `json:"-"`
	RuleRaw             string                         `json:"rules"`
	Expression          string                         `json:"expression"`
	IsSystem            bool                           `json:"is_system"`
	Editors             string                         `json:"editors"`
	UseAtRaw            string                         `json:"use_at"`
	UseAt               []string                       `json:"-"`
	Version             int                            `json:"version"`
	DesensitizeIndexRaw string                         `json:"desensitive_index"`
	DesensitizeIndex    []string                       `json:"-"`
	PlaintextIndexRaw   string                         `json:"plaintext_index"`
	PlaintextIndex      []string                       `json:"-"`
	AntiIndexRaw        string                         `json:"anti_index"`
	AntiIndex           []string                       `json:"-"`
	NewRulesRaw         string                         `json:"new_rules"`
	NewExpression       string                         `json:"new_expression"`
	ParsedExpression    *govaluate.EvaluableExpression `json:"-"`
	NewRules            []RuleElement                  `json:"-"`
	IsLooseMode         bool                           `json:"is_loose_mode"`
}

type RuleDocker struct {
	RuleID   int    `json:"rule_id"`
	RuleName string `json:"rule_name"`
	TagID    int    `json:"tag_id"`
	IsSystem bool   `json:"is_system"`
	Kind     string `json:"kind"`
	KeyName  string `json:"key_name"`
	Version  int    `json:"version"`
}

func NewRuleEngine() (*RuleEngine, error) {
	e := &RuleEngine{
		token: "",
		rules: nil,
	}
	err := e.fetchRules(false)
	return e, err
}

func (e *RuleEngine) fetchRules(useConsul bool) error {
	body, err := static.Asset("static/system_rules.json")
	if err != nil {
		return err
	}

	var r struct {
		Status string `json:"status"`
		Data   []Rule `json:"data"`
	}

	err = json.Unmarshal(body, &r)
	if err != nil {
		err = fmt.Errorf("Failed to unmarshal the system rule JSON string due to the error:%v. data=%v", err, string(body))
	}
	ruleMap := make(map[int]Rule)

	for _, ruleElement := range r.Data {
		ruleMap[ruleElement.ID] = ruleElement
	}

	var errRule error
	for _, ruleElement := range ruleMap {
		// 只处理规则不为空 && 版本相同
		if ruleElement.NewExpression == "" ||
			ElementInSlice(ruleElement.NewRulesRaw, []string{"[]", ",", ""}) {
			continue
		}
		// 切割用途
		ruleElement.UseAt = strings.Split(ruleElement.UseAtRaw, ",")
		// 提取规则
		// var rr []RuleElement
		// errRule = json.Unmarshal([]byte(ruleElement.RuleRaw), &rr)
		// if errRule != nil {
		//	utils.LogsWarnf("parse rule failed, jump: id=%v, err=%v", ruleElement.ID, err)
		//	continue
		// }
		var nrr, nrr1 []RuleElement
		errRule = json.Unmarshal([]byte(ruleElement.NewRulesRaw), &nrr)
		if errRule != nil {
			utils.LogsWarnf("parse rule failed, jump: id=%v, err=%v", ruleElement.ID, err)
			continue
		}
		var compileErrMsg string
		for _, rr := range nrr {
			if rr.Operator == "match" {
				pattern, e := regexp.Compile(rr.Value)
				if e != nil {
					compileErrMsg = fmt.Sprintf("parse rule regexp failed, jump: id=%v, data=%s, err=%v",
						ruleElement.ID, rr.Value, err)
				}
				rr.Pattern = pattern
			}
			nrr1 = append(nrr1, rr)
		}
		if compileErrMsg != "" {
			utils.LogsWarnf(compileErrMsg)
			continue
		}
		var expression *govaluate.EvaluableExpression
		expression, errRule = govaluate.NewEvaluableExpression(
			strings.Replace(strings.Replace(ruleElement.NewExpression, "|", "||", -1),
				"&", "&&", -1))
		if err != nil {
			utils.LogsWarnf("parse expresion failed, jump: id=%v, err=%v", ruleElement.ID, err)
			continue
		}
		// 保存数据
		ruleElement.NewRules = nrr1
		ruleElement.DesensitizeIndex = strings.Split(ruleElement.DesensitizeIndexRaw, ",")
		ruleElement.PlaintextIndex = strings.Split(ruleElement.PlaintextIndexRaw, ",")
		ruleElement.AntiIndex = strings.Split(ruleElement.AntiIndexRaw, ",")
		ruleElement.ParsedExpression = expression
		e.rules = append(e.rules, ruleElement)
	}

	return err
}

// 所有规则的检测
func (e RuleEngine) Match(data Element) []RuleDocker {
	return e.MatchWithFlags(data, "")
}

func (e RuleEngine) MatchWithFlags(data Element, flags string) []RuleDocker {
	var existsRuleID map[string]bool
	var ruleDocker []RuleDocker
	if matchValueToJson {
		for _, v := range data.Value {
			if jsoniter.Valid([]byte(v)) {
				if existsRuleID == nil {
					existsRuleID = map[string]bool{}
				}
				lJsonData := utils.ParseNestedJson(v)
				// filter parse error
				if len(lJsonData) == 1 {
					_, ok := lJsonData["data"]
					if ok {
						continue
					}
				}
				for kk, rr := range lJsonData {
					rrResult := e.matchSingleWithFlags(Element{
						Key:         kk,
						Value:       rr,
						Description: data.Description,
						Type:        data.Type,
					}, flags)
					if len(rrResult) != 0 {
						for _, lrrResult := range rrResult {
							key := fmt.Sprintf("%s:%d", kk, lrrResult.RuleID)
							_, ok := existsRuleID[key]
							if !ok {
								lrrResult.KeyName = kk
								existsRuleID[key] = true
								ruleDocker = append(ruleDocker, lrrResult)
							}
						}
					}
				}
			}
		}
	}
	dataResult := e.matchSingleWithFlags(data, flags)
	if len(dataResult) != 0 {
		ruleDocker = append(ruleDocker, dataResult...)
	}
	return ruleDocker
}

// 所有规则的检测，
func (e RuleEngine) matchSingleWithFlags(data Element, flags string) []RuleDocker {

	var plaintextFlag, ciphertextFlag, blanktextFlag, sensitivertextFlag, allFlag bool
	switch flags {
	case RuleFlagPlain:
		// 只要明文
		plaintextFlag = true
	case RuleFlagCipher:
		// 只要密文
		ciphertextFlag = true
	case RuleFlagBlank:
		// 只要空值
		blanktextFlag = true
	case RuleFlagSensitive:
		// 只要密文
		sensitivertextFlag = true
	case RuleFlagAll:
		// 兼容密文、空值的情况
		allFlag = true
	}

	var results []RuleDocker
	for _, rule := range e.rules {
		ruleResult := map[string]interface{}{}
		// var looseMode bool
		for _, ruleElement := range rule.NewRules {
			var resultFlag bool
			// 如果只配置了明文规则，而且明文规则一定要为true，那么是可以做密文/脱敏检测的
			// 宽松模式的规则不适用
			if (rule.ID == 111 || rule.ID == 178) &&
				(ciphertextFlag == true || blanktextFlag == true || allFlag == true) &&
				(ElementInSlice(ruleElement.Index, rule.PlaintextIndex) || ElementInSlice(ruleElement.Index, rule.DesensitizeIndex)) {
				resultFlag = true
				// looseMode = true
			} else {
				resultFlag = ruleElement.Match(data)
			}
			ruleResult[ruleElement.Index] = resultFlag
		}
		// 如果运算表达式结果为 true
		calculateResult, err := calculateExpression(ruleResult, rule.ParsedExpression)
		if err != nil {
			utils.LogsErrorf("calculateExpression failed: err=%v, rule_id=%v", err, rule.ID)
		}
		if calculateResult == true {
			// 限制宽松模式
			// if looseMode && () {
			//	continue
			// }
			// 获取文本类型
			var kind string
			if IsBlankSlice(data.Value) == true {
				kind = KindBlankText
			} else if rule.MatchPlaintext(data) == true {
				kind = KindPlaintext
			} else if rule.MatchDesensitizedText(data) == true {
				kind = KindDesensitizedText
			} else if isEncryptedSlice(data.Value) == true {
				kind = KindCiphertext
			} else {
				kind = KindUnknownText
			}
			// 检测类型
			if (plaintextFlag == true && kind != KindPlaintext) || (ciphertextFlag == true && kind != KindCiphertext) ||
				(sensitivertextFlag == true && kind != KindDesensitizedText) {
				continue
			}
			// 保存结果
			results = append(results, RuleDocker{
				RuleID:   rule.ID,
				RuleName: rule.Name,
				TagID:    rule.Field,
				IsSystem: rule.IsSystem,
				Kind:     kind,
				Version:  rule.Version,
			})
		}
	}
	return results
}

// 明文检测
func (r Rule) MatchPlaintext(data Element) bool {
	for _, sonRule := range r.NewRules {
		if ElementInSlice(sonRule.Index, r.PlaintextIndex) {
			if sonRule.Match(data) {
				return true
			}
		}
	}
	return false
}

// 脱敏检测
func (r Rule) MatchDesensitizedText(data Element) bool {
	for _, sonRule := range r.NewRules {
		if ElementInSlice(sonRule.Index, r.DesensitizeIndex) {
			if sonRule.Match(data) {
				return true
			}
		}
	}
	return false
}

// 密文检测
func (r Rule) MatchCipherText(data Element, useKMS bool) bool {
	if r.MatchPlaintext(data) || r.MatchDesensitizedText(data) {
		return false
	}
	if useKMS == false {
		return true
	} else {
		for _, v := range data.Value {
			if v != "" && IsKMSEncrypted(v) {
				return true
			}
		}
		return false
	}
}

// 单条规则的检测
func (r RuleElement) Match(data Element) bool {
	var f func(matchData, refData string, r *regexp.Regexp) bool
	switch r.Operator {
	case "eq":
		f = func(matchData, refData string, r *regexp.Regexp) bool {
			return strings.ToLower(matchData) == refData
		}
	case "startsWith":
		f = func(matchData, refData string, r *regexp.Regexp) bool {
			return strings.HasPrefix(strings.ToLower(matchData), refData)
		}
	case "endsWith":
		f = func(matchData, refData string, r *regexp.Regexp) bool {
			return strings.HasSuffix(strings.ToLower(matchData), refData)
		}
	case "contains":
		f = func(matchData, refData string, r *regexp.Regexp) bool {
			return strings.Contains(strings.ToLower(matchData), refData)
		}
	case "match":
		f = func(matchData, refData string, r *regexp.Regexp) bool {
			return r.MatchString(matchData)
		}
	default:
		utils.LogsWarnf("unknown match op: %v", r.Operator)
		return false
	}
	switch r.Key {
	case "name":
		return f(data.Key, r.Value, r.Pattern)
	case "value":
		if len(data.Value) == 0 {
			return false
		}
		validCount := 0
		for _, v := range data.Value {
			if f(v, r.Value, r.Pattern) == true {
				validCount += 1
			}
		}
		return float64(validCount)/float64(len(data.Value)) >= 0.5
	case "description":
		return f(data.Description, r.Value, r.Pattern)
	case "type":
		return f(data.Type, r.Value, r.Pattern)
	default:
		utils.LogsWarnf("unknown match kind: %v", r.Key)
	}
	return false
}

func calculateExpression(results map[string]interface{}, expression *govaluate.EvaluableExpression) (bool, error) {
	if expression == nil {
		return false, fmt.Errorf("expression is nil, ep=%+v, data=%+v", expression, results)
	}
	if results == nil {
		return false, fmt.Errorf("input result is nil, ep=%+v, data=%+v", expression, results)
	}
	r, err := expression.Evaluate(results)
	if err != nil {
		return false, fmt.Errorf("%v, ep=%+v, data=%+v", err, expression, results)
	}
	return r.(bool), nil
}

func CalculateExpression(results map[string]interface{}, expression string) bool {
	r, err := govaluate.NewEvaluableExpression(expression)
	if err != nil {
		utils.LogsErrorf("new expression failed: %v, ep=%+v, data=%+v", err, expression, results)
		return false
	}
	expressionResult, err := calculateExpression(results, r)
	if err != nil {
		utils.LogsErrorf("use expression failed: %v, ep=%+v, data=%+v", err, expression, results)
	}
	return expressionResult
}

func FetchTags() ([]Tag, error) {
	return fetchTags()
}

func fetchTags() ([]Tag, error) {
	var r struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Data    []Tag  `json:"data"`
	}

	body, err := static.Asset("static/tags.json")
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(body, &r)

	if r.Status != "success" {
		return nil, fmt.Errorf("fetch tags failed, reason=%v", r.Message)
	}

	return r.Data, err
}

type Tag struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	Level           int    `json:"level"`
	Classify        string `json:"classify"`
	EnglishName     string `json:"name_en"`
	EnglishClassify string `json:"classify_en"`
}

// 粗略判断数据是否经过 KMS 加密
func IsKMSEncrypted(text string) bool {
	if strings.HasPrefix(text, "@") {
		text = strings.TrimPrefix(text, "@")
	}
	_, err := base64.StdEncoding.DecodeString(text)
	return err == nil
}

func isEncryptedSlice(texts []string) bool {
	for _, text := range texts {
		if text != "" && IsKMSEncrypted(text) == false {
			return false
		}
	}
	return true
}

// 空文本检测
func IsBlankSlice(texts []string) bool {
	for _, text := range texts {
		if text != "" {
			return false
		}
	}
	return true
}
