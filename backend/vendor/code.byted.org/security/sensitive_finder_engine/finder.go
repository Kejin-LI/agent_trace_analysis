package sensitive_finder_engine

import (
	"code.byted.org/security/sensitive_finder_engine/masker"
)

type FinderResult struct {
	Content string
	Kind    string
	Loc     FinderResultLoc
	Rules   []RuleDocker
}

type FinderResultLoc struct {
	Start int
	End   int
}

type Finder struct {
	e *RuleEngine
}

func NewFinder() (*Finder, error) {
	e, err := NewRuleEngine()
	if err != nil {
		return nil, err
	}
	return &Finder{e: e}, nil
}

func (Finder) Name() string {
	return "structure_data"
}

func (f *Finder) FindSensitive(value string) *FinderResult {
	return f.FindSensitiveWithKey("", value)
}

func (f *Finder) MaskSensitive(value string) string {
	return f.MaskSensitiveWithKey("", value)
}

func (f *Finder) FindSensitiveWithKey(key, value string) *FinderResult {

	results := f.e.Match(Element{
		Key:         key,
		Value:       []string{value},
		Description: "",
		Type:        "",
	})
	if len(results) == 0 {
		return nil
	} else {
		k, ok := TagIDToKind[results[0].TagID]
		if !ok {
			k = masker.SensitiveKindUnknown
		}
		return &FinderResult{
			Content: value,
			Kind:    k,
			Rules:   results,
		}
	}
}

func (f *Finder) MaskSensitiveWithKey(key, value string) string {

	result := f.FindSensitiveWithKey(key, value)
	if result == nil {
		return value
	}
	return masker.MaskDataWithKind(value, result.Kind)
}
