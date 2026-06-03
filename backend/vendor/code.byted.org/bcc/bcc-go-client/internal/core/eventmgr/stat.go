package eventmgr

import (
	"code.byted.org/bcc/bcc-go-client/internal/core/common"
	"code.byted.org/bcc/bcc-go-client/internal/util"
)

type stat struct {
	statFlows map[common.DownloadItemSource]int //带宽统计（每30秒更新一次）
	statKeys  map[common.DownloadItemSource]int //key统计（每30秒更新一次）
}

func newStat() *stat {
	return &stat{
		statFlows: make(map[common.DownloadItemSource]int),
		statKeys:  make(map[common.DownloadItemSource]int),
	}
}

func (s *stat) update(key *KeyInfo) {
	s.statKeys[key.source()]++
	s.statFlows[key.source()] += int(key.ValueSize)
}

func (s *stat) emit() {
	if len(s.statFlows) > 0 {
		for source, value := range s.statFlows {
			util.EmitStatFlow(source.String(), value)
		}
		s.statFlows = make(map[common.DownloadItemSource]int, 0)
	}
	if len(s.statKeys) > 0 {
		for source, value := range s.statKeys {
			util.EmitStatKey(source.String(), value)
		}
		s.statKeys = make(map[common.DownloadItemSource]int, 0)
	}
}
