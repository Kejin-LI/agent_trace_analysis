/*
edit from https://github.com/anknown/ahocorasick
*/
package goahocorasick

import (
	"fmt"
)

import (
	"code.byted.org/security/sensitive_finder_engine/string_finder/darts"
)

const FAIL_STATE = -1
const ROOT_STATE = 1

type Machine struct {
	trie            *godarts.DoubleArrayTrie
	failure         []int
	output          map[int][]string
	outputTerms     map[int][]Term
	outputTermSlice [][]Term
}

type Term struct {
	Len     int
	WordLen int
	Word    string
}

func (m *Machine) Build(keywords []string) (err error) {
	if len(keywords) == 0 {
		return fmt.Errorf("empty keywords")
	}

	d := new(godarts.Darts)

	trie := new(godarts.LinkedListTrie)
	m.trie, trie, err = d.Build(keywords)
	if err != nil {
		return err
	}

	m.output = make(map[int][]string, 0)
	for idx, val := range d.Output {
		m.output[idx] = append(m.output[idx], val)
	}
	m.outputTerms = make(map[int][]Term, 0)
	for idx, val := range d.Output {
		m.outputTerms[idx] = append(m.outputTerms[idx], Term{
			Word:    val,
			Len:     len(val) - 1,
			WordLen: len(val),
		})
	}

	queue := make([](*godarts.LinkedListTrieNode), 0)
	m.failure = make([]int, len(m.trie.Base))
	for _, c := range trie.Root.Children {
		m.failure[c.Base] = godarts.ROOT_NODE_BASE
	}
	queue = append(queue, trie.Root.Children...)

	for {
		if len(queue) == 0 {
			break
		}

		node := queue[0]
		for _, n := range node.Children {
			if n.Base == godarts.END_NODE_BASE {
				continue
			}
			inState := m.f(node.Base)
		set_state:
			outState := m.g(inState, n.Code-godarts.ROOT_NODE_BASE)
			if outState == FAIL_STATE {
				inState = m.f(inState)
				goto set_state
			}
			if _, ok := m.output[outState]; ok != false {
				copyOutState := make([]string, 0)
				copyOutTerms := make([]Term, 0)
				for _, o := range m.output[outState] {
					copyOutState = append(copyOutState, o)
				}
				for _, o := range m.outputTerms[outState] {
					copyOutTerms = append(copyOutTerms, o)
				}
				m.output[n.Base] = append(copyOutState, m.output[n.Base]...)
				m.outputTerms[n.Base] = append(copyOutTerms, m.outputTerms[n.Base]...)
			}
			m.setF(n.Base, outState)
		}
		queue = append(queue, node.Children...)
		queue = queue[1:]
	}

	var maxKey int
	for k := range m.outputTerms {
		if k > maxKey {
			maxKey = k
		}
	}
	m.outputTermSlice = make([][]Term, maxKey+1)
	for k := range m.outputTerms {
		if k < 0 {
			continue
		}
		m.outputTermSlice[k] = m.outputTerms[k]
	}

	return nil
}

func (m *Machine) g(inState int, input uint8) (outState int) {
	if inState == FAIL_STATE {
		return ROOT_STATE
	}

	t := inState + int(input) + godarts.ROOT_NODE_BASE
	if t >= len(m.trie.Base) {
		if inState == ROOT_STATE {
			return ROOT_STATE
		}
		return FAIL_STATE
	}
	if inState == m.trie.Check[t] {
		return m.trie.Base[t]
	}

	if inState == ROOT_STATE {
		return ROOT_STATE
	}

	return FAIL_STATE
}

func (m *Machine) f(index int) (state int) {
	return m.failure[index]
}

func (m *Machine) setF(inState, outState int) {
	m.failure[inState] = outState
}

type PosTerm struct {
	Terms []Term
	Pos   int
}

func (m *Machine) MultiPatternSearch(content string, returnImmediately bool) []PosTerm {
	var terms []PosTerm
	var newState int
	state := ROOT_STATE
	for pos := range content {
	start:
		newState = m.g(state, content[pos])
		if newState == FAIL_STATE {
			state = m.f(state)
			goto start
		} else {
			state = newState
			if state == ROOT_STATE || state == FAIL_STATE || (state > 0 && m.outputTermSlice[state] == nil) {
				continue
			}
			if state > 0 {
				if val := m.outputTermSlice[state]; val != nil {
					terms = append(terms, PosTerm{
						Pos:   pos,
						Terms: val,
					})
					if returnImmediately {
						return terms
					}
				}
			} else {
				if val, ok := m.outputTerms[state]; ok != false {
					terms = append(terms, PosTerm{
						Pos:   pos,
						Terms: val,
					})
					if returnImmediately {
						return terms
					}
				}
			}
		}
	}

	return terms
}

//func (m *Machine) ExactSearch(content []uint8) [](*Term) {
//	if m.trie.ExactMatchSearch(content, 0) {
//		t := new(Term)
//		t.Word = string(content)
//		t.Pos = 0
//		return [](*Term){t}
//	}
//
//	return nil
//}
