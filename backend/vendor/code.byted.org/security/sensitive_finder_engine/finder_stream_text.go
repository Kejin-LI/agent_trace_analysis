package sensitive_finder_engine

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"code.byted.org/security/sensitive_finder_engine/masker"
	regexp2 "code.byted.org/security/sensitive_finder_engine/regexp"
	"code.byted.org/security/sensitive_finder_engine/static"
	"code.byted.org/security/sensitive_finder_engine/utils"
)

var (
	cycle = 1 * time.Hour
	dash  = []string{":", "=", " "}
)

type StreamRule struct {
	R             Rule
	Exp           *regexp.Regexp
	ExpCodeSearch *regexp2.Regexp
	Kind          string
}

func (s StreamRule) ToDocker() RuleDocker {
	return RuleDocker{
		RuleID:   s.R.ID,
		RuleName: s.R.Name,
		TagID:    s.R.Field,
		IsSystem: s.R.IsSystem,
		Kind:     s.Kind,
	}
}

type StreamTextFinder struct {
	Kind        []string
	CustomRules []StreamTextFindCustomRule
	ruleModule  *StreamRuleModule
	//自定义
	CustomMasker func(kind string) *masker.MaskFunc
}

type StreamRuleModule struct {
	rules []StreamRule
}

type StreamTextFindCustomRule struct {
	Kind string `json:"kind"`
	Rule string `json:"rule"`
}

func NewStreamTextFinder() (*StreamTextFinder, error) {
	return NewStreamTextFinderWithKindAndRulesAndCustomMasker(nil, nil, nil)
}

func NewStreamTextFinderWithKind(kind []string) (*StreamTextFinder, error) {
	return NewStreamTextFinderWithKindAndRulesAndCustomMasker(kind, nil, nil)
}

func NewStreamTextFinderWithRules(rules []StreamTextFindCustomRule) (*StreamTextFinder, error) {
	return NewStreamTextFinderWithKindAndRulesAndCustomMasker(nil, rules, nil)
}

func NewStreamTextFinderWithCustomMasker(customMaskFunc func(kind string) *masker.MaskFunc) (*StreamTextFinder, error) {
	return NewStreamTextFinderWithKindAndRulesAndCustomMasker(nil, nil, customMaskFunc)
}

func NewStreamTextFinderWithKindAndRules(kind []string, customRules []StreamTextFindCustomRule) (*StreamTextFinder, error) {
	return NewStreamTextFinderWithKindAndRulesAndCustomMasker(kind, customRules, nil)
}

func NewStreamTextFinderWithKindAndRulesAndCustomMasker(kind []string, customRules []StreamTextFindCustomRule,
	customMaskFunc func(kind string) *masker.MaskFunc) (*StreamTextFinder, error) {
	m, err := newStreamRuleModule(kind, customRules)
	if err != nil {
		return nil, err
	}
	s := &StreamTextFinder{
		Kind:         kind,
		CustomRules:  customRules,
		ruleModule:   m,
		CustomMasker: customMaskFunc,
	}
	ticker := time.NewTicker(cycle)
	go func() {
		for t := range ticker.C {
			utils.LogsInfo("stream text finder update rule at %s", t)
			s.updateRules()
		}
	}()
	return s, err
}

func newStreamRuleModule(kind []string, customRules []StreamTextFindCustomRule) (*StreamRuleModule, error) {
	rules, err := getRules(kind)
	if err != nil {
		return nil, err
	}
	m := &StreamRuleModule{
		rules: rules,
	}
	err = m.AddRules(customRules)
	if err != nil {
		return m, err
	}
	return m, nil
}

func (m *StreamRuleModule) AddRules(rules []StreamTextFindCustomRule) error {
	//m.Lock()
	//defer m.Unlock()
	for _, r := range rules {
		rrr, err := regexp.Compile(r.Rule)
		if err != nil {
			return err
		}
		rrr1, err := regexp2.Compile(r.Rule)
		if err != nil {
			return err
		}
		m.rules = append(m.rules, StreamRule{
			R: Rule{
				ID:       0,
				Name:     fmt.Sprintf("custom_rules_%s", rrr),
				Field:    0,
				IsSystem: false,
			},
			Exp:           rrr,
			ExpCodeSearch: rrr1,
			Kind:          r.Kind,
		})
	}
	return nil
}

func (s *StreamTextFinder) SetCustomRules(rules []StreamTextFindCustomRule) {
	s.CustomRules = rules
	s.updateRules()
}

func (StreamTextFinder) Name() string {
	return "stream_text"
}

func getRules(kind []string) ([]StreamRule, error) {

	var rules []StreamRule
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
		if kind != nil && len(kind) != 0 {
			var exf bool
			for _, kindElement := range kind {
				if kindElement == r.Kind {
					exf = true
					break
				}
			}
			if !exf {
				continue
			}
		}

		// 把正则组织在一起
		// todo：允许针对值/键的规则
		if len(r.Keys) == 0 || len(r.Values) == 0 {
			continue
		}

		var nValues []string
		for _, v := range r.Values {
			nValues = append(nValues, fmt.Sprintf("(%s)", v))
		}

		/*
			eg:
			abc=233
			abc: 233
			abc": "233
			abc\": \"233
		*/
		newRule := fmt.Sprintf("(?i)(%s)s?\\\\?[\"'`]?\\s{0,5}[:=]?\\s{0,5}(map\\[)?(\\[\\]string\\{)?\\[?u?\\\\?[\"'`]?(%s)",
			strings.Join(r.Keys, "|"),
			strings.Join(nValues, "|"))
		// 保存新的规则

		rrr, err := regexp.Compile(newRule)
		if err != nil {
			utils.LogsErrorf("parse regexp error, reason=%v, exp=%v", err, newRule)
			continue
		}
		rrr1, err := regexp2.Compile(newRule)
		if err != nil {
			utils.LogsErrorf("parse regexp error, reason=%v, exp=%v", err, newRule)
			continue
		}

		//fmt.Println(r.ID, newRule)
		rules = append(rules, StreamRule{
			R: Rule{
				ID:    r.RuleID,
				Field: r.TagID,
			},
			Exp:           rrr,
			ExpCodeSearch: rrr1,
			Kind:          r.Kind,
		})
	}
	return rules, nil
}

func (s StreamRule) Match(data []byte) bool {
	return s.ExpCodeSearch.MatchString(utils.BytesToString(data), true, true) > 0
}

func (s *StreamTextFinder) match(data []byte) []RuleDocker {

	rm := s.ruleModule

	var results []RuleDocker
	for _, rule := range rm.rules {
		if rule.Match(data) {
			results = append(results, rule.ToDocker())
		}
	}

	return results
}

func (s *StreamTextFinder) Match(data []byte) []string {
	var results []string
	for _, r := range s.match(data) {
		kind := getSensitiveKindByRuleDocker(r)

		if !ElementInSlice(kind, results) {
			results = append(results, kind)
		}
	}
	return results
}

func (s *StreamTextFinder) MatchWithTagReturn(data []byte, tags []Tag) []Tag {
	var results []Tag
	for _, rule := range s.match(data) {
		for _, tag := range tags {
			if tag.ID == rule.TagID {
				results = append(results, tag)
			}
		}
	}
	return results
}

func (s *StreamTextFinder) MatchWithRuleReturn(data []byte) []RuleDocker {
	return s.match(data)
}

func (s *StreamTextFinder) FindSensitive(data string) []FinderResult {

	rm := s.ruleModule

	var findResults, newFindResults []FinderResult
	for _, r := range rm.rules {

		// important: 确定存在后再 find index
		if r.Match([]byte(data)) == false {
			continue
		}

		// important: data无变化，可能重复匹配（比如同一数据被匹配、同一规则多次匹配）
		results := r.Exp.FindAllIndex([]byte(data), -1)
		if len(results) != 0 {

			kind := getSensitiveKindByRuleDocker(r.ToDocker())
			if kind == masker.SensitiveKindUnknown {
				// important: only handle sensitive kind data
				continue
			}

			for _, result := range results {
				findResults = append(findResults, FinderResult{
					Content: data[result[0]:result[1]],
					Kind:    kind,
					Loc: FinderResultLoc{
						Start: result[0],
						End:   result[1],
					},
					Rules: []RuleDocker{
						{
							RuleID:   r.R.ID,
							RuleName: r.R.Name,
							TagID:    r.R.Field,
							IsSystem: r.R.IsSystem,
							Kind:     "",
						},
					},
				})
			}
		}
	}

	// important: 去重
	for _, result := range findResults {
		exists := false
		for _, r := range newFindResults {
			if result.Loc.Start <= r.Loc.End && result.Loc.End >= r.Loc.Start {
				exists = true
			}
		}
		if !exists {
			newFindResults = append(newFindResults, result)
		}
	}

	return newFindResults
}

func (s *StreamTextFinder) MaskSensitive(data string) string {
	return s.MatchAndMask(data)
}

func (s *StreamTextFinder) MatchAndMask(data string) string {
	_, newData, _ := s.matchAndMask(data, false)
	return newData
}

func (s *StreamTextFinder) MatchAndMaskWithResult(data string) (string, bool) {
	_, newData, ok := s.matchAndMask(data, false)
	return newData, ok
}

func (s *StreamTextFinder) MatchAndMaskWithKindReturn(data string) ([]string, string) {
	kindList, data, _ := s.matchAndMask(data, true)
	keys := make([]string, len(kindList))
	i := 0
	for k := range kindList {
		keys[i] = k
		i++
	}
	return keys, data
}

func (s *StreamTextFinder) matchAndMask(data string, logKind bool) (map[string]int, string, bool) {

	var isMask bool
	var kindList map[string]int
	if logKind == true {
		kindList = map[string]int{}
	}

	rm := s.ruleModule

	newData := data
	for _, r := range rm.rules {
		// important: 确定存在后再 find index
		if r.Match([]byte(newData)) == false {
			continue
		}
		results := r.Exp.FindAllIndex([]byte(newData), -1)
		if len(results) != 0 {

			kind := getSensitiveKindByRuleDocker(r.ToDocker())
			if kind == masker.SensitiveKindUnknown {
				// important: only handle sensitive kind data
				continue
			}
			if logKind == true {
				kindList[kind]++
			}
			isMask = true

			for _, result := range results {
				matchData := newData[result[0]:result[1]]
				var maskMatchData string
				if s.CustomMasker != nil {
					maskMatchData = MaskInKeyAndValue(matchData, kind, s.CustomMasker(kind))
				} else {
					maskMatchData = MaskInKeyAndValue(matchData, kind, nil)
				}
				newData = fmt.Sprintf("%s%s%s",
					newData[:result[0]], maskMatchData, newData[result[1]:])
			}
		}
	}

	return kindList, newData, isMask
}

func MaskInKeyAndValue(data, kind string, maskFunc *masker.MaskFunc) string {
	var split string
	if strings.Contains(data, ":") {
		split = ":"
	} else if strings.Contains(data, "=") {
		split = "="
	} else if strings.Contains(data, " ") {
		split = ""
	} else {
		split = " "
	}

	position, ok := masker.SensitiveMarkPosition[kind]
	if !ok {
		position = masker.SensitiveMarkPosition[masker.SensitiveKindUnknown]
	}

	var result []string
	if split != "" {
		result = strings.SplitN(data, split, 2)
	} else {
		result = []string{data}
	}

	if len(result) == 1 {
		if maskFunc != nil {
			return (*maskFunc)(result[0], position)
		}
		return masker.MaskDataCustomByte(result[0], position)
	} else {
		if strings.HasPrefix(result[1], "\"") || strings.HasPrefix(result[1], "'") ||
			strings.HasPrefix(result[1], "[") || strings.HasPrefix(result[1], " ") {
			position.StartOffset += 1
		}
		if strings.HasPrefix(result[1], " \"") || strings.HasPrefix(result[1], " '") ||
			strings.HasPrefix(result[1], " [") {
			position.StartOffset += 1
		}
		if strings.HasPrefix(result[1], " [\"") || strings.HasPrefix(result[1], " ['") ||
			strings.HasPrefix(result[1], "[\"") || strings.HasPrefix(result[1], "['") {
			position.StartOffset += 1
		}
		if strings.HasSuffix(result[1], "\"") || strings.HasSuffix(result[1], "'") {
			position.EndOffset += 1
		}
		if strings.HasPrefix(result[1], "\\\"") || strings.HasPrefix(result[1], " u'") {
			position.StartOffset += 2
		}
		if strings.HasPrefix(result[1], "map[") {
			position.StartOffset += 3
		}
		if strings.HasPrefix(result[1], "[]string{") {
			position.StartOffset += 9
		}

		var b strings.Builder
		b.WriteString(result[0])
		b.WriteString(split)
		if maskFunc != nil {
			b.WriteString((*maskFunc)(result[1], position))
		} else {
			b.WriteString(masker.MaskDataCustomByte(result[1], position))
		}
		return b.String()
	}
}

func (s *StreamTextFinder) GetRuleName() []string {

	rm := s.ruleModule

	var results []string
	for _, r := range rm.rules {
		results = append(results, r.R.Name)
	}
	return results
}

func (s *StreamTextFinder) updateRules() {
	m, err := newStreamRuleModule(s.Kind, s.CustomRules)
	if err != nil {
		utils.LogsWarnf("stream text finder update rule error: %v", err)
		return
	}
	s.ruleModule = m
}

func getSensitiveKindByRuleDocker(r RuleDocker) string {
	kind, ok := TagIDToKind[r.TagID]
	if !ok {
		kind = masker.SensitiveKindUnknown
	}
	if r.Kind != "" {
		kind = r.Kind
	}
	return kind
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
