package downloader

import (
	"bytes"
	"io"
	"sync"
)

var (
	bfPool sync.Pool
	once   sync.Once
)

func initBufferPool(size int) {
	if size == 0 {
		size = 16 * 1024 * 1024
	}
	once.Do(func() {
		bfPool = sync.Pool{New: func() interface{} {
			return bytes.NewBuffer(make([]byte, size))
		}}
	})
}
func poolReadAll(r io.Reader) ([]byte, error) {
	buffer := bfPool.Get().(*bytes.Buffer)
	buffer.Reset()
	defer func() {
		//避免不小心已经放回buffer
		if buffer != nil {
			bfPool.Put(buffer)
			//确保没有引用
			buffer = nil
		}
	}()
	_, err := io.Copy(buffer, r)
	if err != nil {
		return nil, err
	}
	dstBytes := make([]byte, buffer.Len())
	copy(dstBytes, buffer.Bytes())
	return dstBytes, nil
}
