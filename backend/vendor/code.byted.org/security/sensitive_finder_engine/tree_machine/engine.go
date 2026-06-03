package tree_machine

import (
	"fmt"
	"sort"
	"time"
	"unsafe"

	"code.byted.org/security/sensitive_finder_engine/utils"
)

type Finder struct {
	mmm            *matcher
	kinds          []string
	useTreeMachine bool
	addKey         map[string][]string
	addValue       map[string][]string // custom value have to has key
}

func NewFinder(kinds []string) *Finder {
	return NewFinderCustom(kinds, nil, nil)
}

func NewFinderCustom(kinds []string, customKey, customValue map[string][]string) *Finder {
	var mmm *matcher
	var err error
	mmm, err = getMatcher(kinds, customKey, customValue)
	if err != nil {
		utils.LogsErrorf("create secdate engine failed: %v", err)
		return nil
	}
	f := &Finder{
		mmm:      mmm,
		kinds:    kinds,
		addKey:   customKey,
		addValue: customValue,
	}
	ticker := time.NewTicker(cycle)
	go func() {
		for t := range ticker.C {
			utils.LogsInfo("stream text engine update rule at %s", t)
			f.update()
		}
	}()
	return f
}

func (f *Finder) SetUseTreeMachine(flag bool) *Finder {
	f.useTreeMachine = flag
	return f
}

func (f *Finder) MaskSensitive(data string) string {
	return f.MatchAndMask(data)
}

func (f *Finder) MatchAndMaskWithResult(data string) (string, bool) {
	return f.MatchAndMaskWithFlag(data)
}

func (f Finder) Name() string {
	return "secdata_engine_finder"
}

func (f *Finder) update() {
	lm, err := getMatcher(f.kinds, f.addKey, f.addValue)
	if err != nil {
		utils.LogsErrorf("sensitive finder engine update failed: %v", err)
	} else {
		f.mmm = lm
	}
}

type StringFinder interface {
	next(text string) int
}

type Result struct {
	Start int
	End   int
	Kind  string
	Data  string
}

func (f *Finder) Match(input string) []Result {
	if !f.matchUseGrep(input) {
		return nil
	}
	if f.useTreeMachine {
		return f.matchUseAHO(input)
	}
	return f.matchUseRegexp(input)
}

func (f *Finder) MatchAndMask(input string) string {
	result, _ := f.MatchAndMaskWithFlag(input)
	return result
}

func (f *Finder) MatchAndMaskWithFlag(input string) (string, bool) {
	results := f.Match(input)
	if len(results) == 0 {
		return input, false
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Start < results[j].Start
	})
	var endIndex int
	//var b strings.Builder
	b := make([]byte, len(input))
	for _, result := range results {
		if result.Start < endIndex {
			continue
		}
		copy(b[endIndex:result.Start], input[endIndex:result.Start])
		//copy(bs[result.Start:result.End], input[result.Start:result.End])
		MaskInKeyAndValueWithBytes(input[result.Start:result.End], result.Kind, b, result.Start, result.End)
		endIndex = result.End
		//copy(b[result.End:], input[result.End:])
		//matchData := input[result.Start:result.End]
		//b.WriteString(input[:result.Start])
		//sensitive_finder_engine.MaskInKeyAndValueWithBytes(input[result.Start:result.End], result.Kind, nil, &b)
		//b.WriteString(input[result.End:])
		//newData = fmt.Sprintf("%s%s%s", newData[:result.Start], matchData, newData[result.End:])
		//newData = fmt.Sprintf("%s%s%s", newData[:result.Start], sensitive_finder_engine.MaskInKeyAndValue(matchData, result.Kind, nil), newData[result.End:])
	}
	if endIndex < len(input) {
		copy(b[endIndex:len(input)], input[endIndex:])
	}
	//return b.String()
	return *(*string)(unsafe.Pointer(&b)), true
}

func (f *Finder) matchUseGrep(input string) bool {
	//for _, p := range valuePattern{
	//	r := p.Match(input, true, true)
	//	if r > 0 {
	//		return true
	//	}
	//}
	//return false
	return f.mmm.keyPattern.MatchString(input, true, true) > 0
}

func (f *Finder) matchUseRegexp(input string) []Result {
	//var c cache
	var results []Result
	for _, terms := range f.mmm.m.MultiPatternSearch(input, false) {
		for _, term := range terms.Terms {
			if f.mmm.kindMap[term.Word] == "others" {
				for index := range otherKind {
					_, ok := f.mmm.keyMatcher[otherKind[index]]
					if !ok {
						continue
					}
					var end int
					if terms.Pos-term.Len+60 > len(input) {
						end = f.mmm.keyMatcher[otherKind[index]].MatchStringIndex(input[terms.Pos+1:], true, true)
					} else {
						end = f.mmm.keyMatcher[otherKind[index]].MatchStringIndex(input[terms.Pos+1:terms.Pos-term.Len+60], true, true)
					}
					// match index
					if end > 0 {
						indexEnd := terms.Pos + 1 + end
						if indexEnd <= len(input) {
							for offset := range input[indexEnd:] {
								if isNumber(input[indexEnd+offset]) || isLetter(input[indexEnd+offset]) {
									end += 1
								} else {
									break
								}
							}
						}
						results = append(results, Result{
							Start: terms.Pos - term.Len,
							End:   terms.Pos + 1 + end,
							Kind:  otherKind[index],
						})
						break
					}
				}
			} else {
				_, ok := f.mmm.keyMatcher[f.mmm.kindMap[term.Word]]
				if !ok {
					continue
				}
				var end int
				if terms.Pos-term.Len+60 > len(input) {
					end = f.mmm.keyMatcher[f.mmm.kindMap[term.Word]].MatchStringIndex(input[terms.Pos+1:], true, true)
				} else {
					end = f.mmm.keyMatcher[f.mmm.kindMap[term.Word]].MatchStringIndex(input[terms.Pos+1:terms.Pos-term.Len+60], true, true)
				}
				// match index
				if end > 0 {
					for offset := range input[terms.Pos+1+end:] {
						if terms.Pos+1+end+offset < len(input) &&
							(isNumber(input[terms.Pos+1+end+offset]) || isLetter(input[terms.Pos+1+end+offset])) {
							end += 1
						} else {
							break
						}
					}
					results = append(results, Result{
						Start: terms.Pos - term.Len,
						End:   terms.Pos + 1 + end,
						Kind:  f.mmm.kindMap[term.Word],
					})
				}
			}
		}
	}
	return results
}

func (f *Finder) matchUseAHO(input string) []Result {
	//var c cache
	var results []Result
	for _, terms := range f.mmm.m.MultiPatternSearch(input, false) {
		for _, term := range terms.Terms {
			cacheState := NewMatchCache(f.mmm.kindMap[term.Word], connect)
			var r *Result
			if useLeftSpace {
				var al int
				for al = 0; al < getLeftSpace(term.Word); al++ {
					if al+terms.Pos+1 >= len(input) {
						break
					}
					if input[al+terms.Pos+1] == 45 || input[al+terms.Pos+1] == 95 ||
						(97 <= input[al+terms.Pos+1] && input[al+terms.Pos+1] <= 122) ||
						(65 <= input[al+terms.Pos+1] && input[al+terms.Pos+1] <= 90) {
						al += 1
					} else {
						break
					}
				}
				r = MatchWithNode(input, terms.Pos-term.Len, term.WordLen+al, cacheState)
			} else {
				r = MatchWithNode(input, terms.Pos-term.Len, term.WordLen, cacheState)
			}
			if r != nil {
				results = append(results, *r)
			}
		}
	}
	return results
}

func MatchWithNode(input string, start int, offset int, cacheState *MatchCache) *Result {
	//defer func() {
	//	for _, c := range cacheState.State.Children {
	//		mapPool.Put(c)
	//	}
	//}()
	if start+offset > len(input) {
		return nil
	}
	for index, _ := range input[start+offset:] {
		//fmt.Println(string(value), input[start+offset:])
		cacheState.IsEnd = index+1+start+offset == len(input)
		matchValueOnce(cacheState, nil, input[index+start+offset])
		if cacheState.Done {
			end := index + 1 + cacheState.IndexAdd + start + offset
			cacheState.EndIndex = end
			cacheState.Length = end - start
			return &Result{
				Start: start,
				End:   end,
				Kind:  cacheState.Kind,
			}
		}
		if !cacheState.State.Valid() {
			return nil
		}
	}
	return nil
}

func handleNode(n *TreeNode, cs *MatchCache) bool {
	handleRightNode(n, cs)
	return cs.Done == true
}

func matchValueOnceNode(cs *MatchCache, node *TreeNode, value uint8) {
	// 非叶子节点、非命中需要看子节点能否命中
	for nodeKey11, nodeValue11 := range node.Children {
		switch nodeKey11.(type) {
		case uint8:
			if value == nodeKey11.(uint8) {
				// 允许跳出
				if nodeValue11.IsLeaf {
					//logs.Error("handle node 1")
					handleNode(nodeValue11, cs)
				} else {
					// 添加子集
					cs.State.AddNodeAllChild(nodeValue11)
				}
			}
		default:
			{
				pattern11, _ := nodeValue11.Pattern.(Pattern)
				matchFlag11 := pattern11.Match(value)
				if matchFlag11 == true {
					// 是否到达末尾：已经匹配最多、到达序列末尾
					if 1 == pattern11.MaxCount || cs.IsEnd {
						if nodeValue11.IsLeaf == true {
							handleNode(nodeValue11, cs)
						} else {
							// 不是叶子节点时候已经到了尽头，处理子节点，添加children
							cs.State.AddNodeAllChild(nodeValue11)
						}
					} else {
						// 正常匹配
						cs.State.AddNodeChild(node, nodeKey11)
					}
					return
				} else {
					if 0 >= pattern11.MinCount && 0 <= pattern11.MaxCount {
						if nodeValue11.IsLeaf == true {
							// 根节点则本次结束
							// todo: 感觉不对
							if nodeValue11.TreeMode == modeNodeConnect {
								// 此时connect添加，必需继续匹配，否则会丢失
								for _, node := range rootValue[cs.Kind] {
									matchValueOnceNode(cs, node, value)
								}
							} else {
								if handleNode(nodeValue11, cs) {
									cs.IndexAdd = -1
									return
								}
							}
						} else {
							// 无法命中时候需要看children
							matchValueOnceNode(cs, nodeValue11, value)
							if cs.Done {
								cs.IndexAdd = -1
								return
							}
						}
					}
				}
			}
		}
	}
}

func matchValueOnce(cs *MatchCache, node *TreeNode, value uint8) {
	// 非叶子节点、非命中需要看子节点能否命中
	//var children nextChildren
	//children = cs.State
	l := cs.State.childLength
	for i := 0; i < l; i++ {
		child := cs.State.Children[i]
		if child.Count < 0 {
			continue
		}
		//if cs.State.Nodes[child.NodeIndex] == nil {
		//	continue
		//}
		childNode := cs.State.Nodes[child.NodeIndex].Children[child.ChildName]
		switch child.ChildName.(type) {
		case uint8:
			if value == child.ChildName.(uint8) {
				// 允许跳出
				if childNode.IsLeaf {
					if handleNode(childNode, cs) {
						return
					}
				} else {
					// 添加子集
					cs.State.AddNodeAllChild(childNode)
				}
			} else {
				// 没命中就删除
				child.Count = -1
			}
		default:
			pattern, _ := childNode.Pattern.(Pattern)
			matchFlag := pattern.Match(value)
			if matchFlag {
				// 是否到达末尾：已经匹配最多、到达序列末尾
				if child.Count+1 == pattern.MaxCount || cs.IsEnd {
					if childNode.IsLeaf {
						if handleNode(childNode, cs) {
							return
						}
					} else {
						cs.State.AddNodeAllChild(childNode)
					}
					child.Count = -1
				} else {
					child.Count += 1
				}
			} else {
				// :jin  连接符号进入了这里
				if child.Count >= pattern.MinCount && child.Count <= pattern.MaxCount {
					if childNode.IsLeaf {
						// 处理叶子节点
						if handleNode(childNode, cs) {
							// 子节点无法命中，因此--
							cs.IndexAdd--
							return
						}
						// todo: else 的情况
					} else {
						// not leaf
						// :jin  连接符号进入了这里，一个都没命中
						matchValueOnceNode(cs, childNode, value)
						if cs.Done {
							return
						}
					}
					child.Count = -1
				} else {
					child.Count = -1
				}
			}
		}
	}
}

func handleRightNode(node *TreeNode, c *MatchCache) {
	if node.TreeMode == modeNodeConnect {
		// choose value pattern
		for _, node := range rootValue[c.Kind] {
			c.State.AddNodeAllChild(node)
		}
	} else if node.TreeMode == modeNodeValue {
		c.Done = true
		if node.Value != nil {
			c.Kind = node.Value.(string)
		}
	} else {
		panic(fmt.Sprintf("unknown kind: %+v", node.TreeMode))
		//c.State.AddNode(node)
	}
}

func getLeftSpace(k string) int {
	switch k {
	case "phone", "mail", "Phone", "mobile", "Mobile":
		return 7
	}
	return 0
}
