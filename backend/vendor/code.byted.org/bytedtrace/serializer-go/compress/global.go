// Created by nzb on 2020-10-30

package compress

import "sync"

var (
	globalCom = newGlobalCom()
)

type globalCompressors struct {
	compressors Compressors
	lock        sync.RWMutex
}

func newGlobalCom() globalCompressors {
	cps := make(map[byte]Compressor)
	// 默认0为不压缩
	cps[None] = NewNoneCompressor()
	return globalCompressors{compressors: Compressors{compressors: cps}}
}

func RegisterCompressor(compressorType byte, compressor Compressor) {
	globalCom.lock.Lock()
	globalCom.compressors.RegisterCompressor(compressorType, compressor)
	globalCom.lock.Unlock()
}

func GetCompressors() *Compressors {
	globalCom.lock.Lock()
	defer globalCom.lock.Unlock()
	cps := make(map[byte]Compressor)
	for key, value := range globalCom.compressors.compressors {
		cps[key] = value.Copy()
	}
	return &Compressors{compressors: cps}
}
