package common

const (
	StatusStop = iota
	StatusRunning
)

const (
	_ int8 = iota // 0 also represents zlib for historical reasons.
	Zlib
	Zstd
	Gzip
	Lz4
	Snappy
)

const (
	CodecVersionOffset = 8
	CodecV3Code        = 3
	CodecV4Code        = 4
)
