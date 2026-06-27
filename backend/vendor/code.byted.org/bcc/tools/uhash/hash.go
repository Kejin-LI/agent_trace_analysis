package uhash

import (
	"hash/adler32"
	"hash/crc32"
	"hash/crc64"
	"hash/fnv"
)

func Crc32(src []byte) uint32 {
	h := crc32.NewIEEE()
	_, _ = h.Write(src)
	return h.Sum32()
}

func Crc64(src []byte) uint64 {
	h := crc64.New(crc64.MakeTable(crc64.ISO))
	_, _ = h.Write(src)
	return h.Sum64()
}

func Adler32(src []byte) uint32 {
	h := adler32.New()
	_, _ = h.Write(src)
	return h.Sum32()
}

func Fnv32(src []byte) uint32 {
	h := fnv.New32()
	_, _ = h.Write(src)
	return h.Sum32()
}

func Fnv32a(src []byte) uint32 {
	h := fnv.New32a()
	_, _ = h.Write(src)
	return h.Sum32()
}

func Fnv64(src []byte) uint64 {
	h := fnv.New64()
	_, _ = h.Write(src)
	return h.Sum64()
}

func Fnv64a(src []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(src)
	return h.Sum64()
}

//{"fnv128", func() hash.Hash { return fnv.New128() }, fromHex("666e760561587a70a0f66d7981dc980e2cabbaf7")},
//{"fnv128a", func() hash.Hash { return fnv.New128a() }, fromHex("666e7606a955802b0136cb67622b461d9f91e6ff")},
