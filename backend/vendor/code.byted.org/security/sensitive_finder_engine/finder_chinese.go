package sensitive_finder_engine

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"code.byted.org/security/sensitive_finder_engine/masker"
	"code.byted.org/security/sensitive_finder_engine/utils"
)

type ChineseFinder struct {
	e *RuleEngine
}

func NewChineseFinder() (*ChineseFinder, error) {
	// e, err := NewRuleEngine(PublicToken)
	// if err != nil {
	//	return nil, err
	// }
	return &ChineseFinder{e: nil}, nil
}

func (ChineseFinder) Name() string {
	return "chinese_text"
}

func (f *ChineseFinder) FindSensitive(value string) []FinderResult {

	var results, resultsRaw []FinderResult
	var existFlag bool
	for _, rule := range chineseFinderRules {
		resultsRaw = append(resultsRaw, rule.Match(value)...)
	}

	sort.Slice(resultsRaw, func(i, j int) bool {
		return resultsRaw[i].Loc.Start < resultsRaw[j].Loc.Start
	})

	for _, result := range resultsRaw {
		existFlag = false
		for _, r := range results {
			if result.Loc.Start <= r.Loc.End && result.Loc.End >= r.Loc.Start {
				existFlag = true
				break
			}
		}
		if !existFlag {
			results = append(results, result)
		}
	}

	return results
}

func (f *ChineseFinder) MaskSensitive(value string) string {

	var lastEnd int
	var valuePart []string
	results := f.FindSensitive(value)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Loc.Start < results[j].Loc.Start
	})
	for _, result := range results {
		valuePart = append(valuePart, string([]rune(value)[lastEnd:result.Loc.Start]))
		valuePart = append(valuePart, MaskDataWithFinderResult(result))
		lastEnd = result.Loc.End
	}
	valuePart = append(valuePart, string([]rune(value)[lastEnd:]))
	return strings.Join(valuePart, "")
}

func MaskDataWithFinderResult(result FinderResult) string {
	position, ok := masker.SensitiveMarkPosition[result.Kind]
	if !ok {
		position = masker.SensitiveMarkPosition[masker.SensitiveKindUnknown]
	}
	return masker.MaskDataCustom(result.Content, position)
	// return fmt.Sprintf("%s%s%s",
	//	string([]rune(text)[:result.Loc.Start]),
	//	m,
	//	string([]rune(text)[result.Loc.End:]))
}

type ChineseRulePattern struct {
	ID              int            `json:"id"`
	Kind            string         `json:"kind"`
	Exp             string         `json:"exp"`
	SpecialChar     []string       `json:"special_char"`
	ContainsLetter  bool           `json:"contains_letter"`
	ContainsChinese bool           `json:"contains_chinese"`
	Func            string         `json:"func"`
	Pattern         *regexp.Regexp `json:"-"`
}

func (r *ChineseRulePattern) Match(text string) []FinderResult {

	if r.Pattern == nil {
		var err error
		r.Pattern, err = regexp.Compile(r.Exp)
		if err != nil {
			utils.LogsErrorf("parse chinese rule failed: %v", err)
		}
	}

	var results []FinderResult
	for _, t := range GroupText(text, r.SpecialChar, r.ContainsLetter, r.ContainsChinese) {
		var matchResult bool
		if r.Pattern != nil {
			matchResult = r.Pattern.Match([]byte(t))
		} else {
			matchResult, _ = regexp.Match(r.Exp, []byte(t))
		}
		if matchResult {
			for _, start := range GetPosition(text, t) {
				//
				start = len([]rune(text[:start]))
				var vStart, vEnd int
				if r.Func == "find" && r.Pattern != nil {
					for _, result := range r.Pattern.FindAllIndex([]byte(t), -1) {
						vStart = result[0]
						vEnd = result[1]
						r := GetFinderResult(start, r.Kind, t, vStart, vEnd, r.Func)
						// fmt.Println(r, MaskDataWithFinderResult(text, r))
						results = append(results, r)
					}
				} else {
					vEnd = len([]rune(t))
					results = append(results, GetFinderResult(start, r.Kind, t, vStart, vEnd, r.Func))
				}
			}
		}
	}

	// fmt.Println(strings.Join(GroupText(text, r.SpecialChar, r.ContainsLetter, r.ContainsChinese), "//"))

	return results
}

func GetFinderResult(start int, kind, content string, vStart, vEnd int, funcName string) FinderResult {
	var rStart, rEnd int
	var rContent string
	rStart = len([]rune(content[:vStart]))
	rEnd = len([]rune(content[:vEnd]))
	if funcName != "find" {
		rContent = content
	} else {
		rContent = content[vStart:vEnd]
	}
	return FinderResult{
		Content: rContent,
		Kind:    kind,
		Loc: FinderResultLoc{
			Start: start + rStart,
			End:   start + rEnd,
		},
	}
}

var (
	NumberPattern, _  = regexp.Compile("\\d")
	ChinesePattern, _ = regexp.Compile("[\u4E00-\u9FA5]")
	LetterPattern, _  = regexp.Compile("[a-zA-Z]")
)

func GroupText(text string, specialChars []string, containsLetter, containsChinese bool) []string {

	unionKind := []string{"number", "char"}
	if containsLetter {
		unionKind = append(unionKind, "letter")
	}
	if containsChinese {
		unionKind = append(unionKind, "chinese")
	}

	var l []string
	var ls, lk, k string

	for _, t := range []rune(text) {

		if ElementInSlice(string(t), []string{" ", "\t", "\n", "\b"}) {
			continue
		}

		if NumberPattern.Match([]byte(string(t))) {
			k = "number"
		} else if LetterPattern.Match([]byte(string(t))) {
			k = "letter"
		} else if ChinesePattern.Match([]byte(string(t))) {
			k = "chinese"
		} else if ElementInSlice(string(t), specialChars) {
			k = "char"
		} else {
			k = "unknown"
		}

		if lk == "" {
			ls = fmt.Sprintf("%s%s", ls, string(t))
			lk = k
			continue
		}

		if k == lk || (ElementInSlice(k, unionKind) && ElementInSlice(lk, unionKind)) {
			ls = fmt.Sprintf("%s%s", ls, string(t))
		} else {
			l = append(l, ls)
			ls = string(t)
			lk = k
		}
	}

	if ls != "" {
		l = append(l, ls)
	}

	return l
}

func ElementInSlice(e string, el []string) bool {
	for _, ee := range el {
		if ee == e {
			return true
		}
	}
	return false
}

func GetPosition(value, substr string) []int {
	s := value
	var posResult []int
	var index int
	index = strings.Index(s, substr)
	preCount := 0
	for index >= 0 {
		posResult = append(posResult, preCount+index)
		s = s[len(substr)+index:]
		preCount = preCount + len(substr) + index
		index = strings.Index(s, substr)
	}
	return posResult
}
