// Created by nzb on 2020-10-30

package compress

import "errors"

const (
	// 压缩器类型
	None byte = 0
	//Gzip      byte = 1
	ZSTD_FAST byte = 2
	//ZSTD      byte = 3
	//ZSTD_HIGH byte = 4
	//Snappy    byte = 5
	//LZ4       byte = 6
)

var (
	CanNotFindCompressorError = errors.New("can not find such compressor")
)

type Compressor interface {
	Compress(data []byte) ([]byte, error)

	DeCompress(data []byte) ([]byte, error)

	Copy() Compressor
}

type noneCompressor struct {
}

func NewNoneCompressor() Compressor {
	return &noneCompressor{}
}

func (n *noneCompressor) Compress(data []byte) ([]byte, error) {
	return data, nil
}

func (n *noneCompressor) DeCompress(data []byte) ([]byte, error) {
	return data, nil
}

func (n *noneCompressor) Copy() Compressor {
	return &noneCompressor{}
}

type Compressors struct {
	compressors map[byte]Compressor
}

func (c *Compressors) RegisterCompressor(compressorType byte, compressor Compressor) {
	if c.compressors == nil {
		c.compressors = make(map[byte]Compressor)
	}
	c.compressors[compressorType] = compressor
}

func (c *Compressors) Compress(compressorType byte, data []byte) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}
	compressor, ok := c.compressors[compressorType]
	if !ok {
		return nil, CanNotFindCompressorError
	}
	return compressor.Compress(data)
}

func (c *Compressors) DeCompress(compressorType byte, data []byte) ([]byte, error) {
	compressor, ok := c.compressors[compressorType]
	if !ok {
		return nil, CanNotFindCompressorError
	}
	return compressor.DeCompress(data)
}
