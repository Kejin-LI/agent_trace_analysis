package msgpack

const (
	StringHeaderSize = 3
	ArrayHeaderSize  = 3
)

// https://github.com/msgpack/msgpack/blob/master/spec.md#formats-array
/*
array 16 stores an array whose length is upto (2^16)-1 elements:
+--------+--------+--------+~~~~~~~~~~~~~~~~~+
|  0xdc  |YYYYYYYY|YYYYYYYY|    N objects    |
+--------+--------+--------+~~~~~~~~~~~~~~~~~+
*/
func FillArrayHeader(b []byte, offset int, n uint16) {
	//return append(b, 0xdc, byte(n>>8), byte(n))
	b[offset] = 0xdc
	b[offset+1] = byte(n >> 8)
	b[offset+2] = byte(n)
}

// https://github.com/msgpack/msgpack/blob/master/spec.md#formats-array
/*
array 16 stores an array whose length is upto (2^16)-1 elements:
+--------+--------+--------+~~~~~~~~~~~~~~~~~+
|  0xdc  |YYYYYYYY|YYYYYYYY|    N objects    |
+--------+--------+--------+~~~~~~~~~~~~~~~~~+
*/
func AppendArrayHeader(b []byte, n uint16) []byte {
	return append(b, 0xdc, byte(n>>8), byte(n))
}

// https://github.com/msgpack/msgpack/blob/master/spec.md#formats-str
/*
str 16 stores a byte array whose length is upto (2^16)-1 bytes:
+--------+--------+--------+========+
|  0xda  |ZZZZZZZZ|ZZZZZZZZ|  data  |
+--------+--------+--------+========+
*/
func FillStringHeader(b []byte, offset int, n uint16) {
	b[offset] = 0xda
	b[offset+1] = byte(n >> 8)
	b[offset+2] = byte(n)
}

// https://github.com/msgpack/msgpack/blob/master/spec.md#formats-str
/*
str 16 stores a byte array whose length is upto (2^16)-1 bytes:
+--------+--------+--------+========+
|  0xda  |ZZZZZZZZ|ZZZZZZZZ|  data  |
+--------+--------+--------+========+
*/
func AppendStringHeader(b []byte, n uint16) []byte {
	return append(b, 0xda, byte(n>>8), byte(n))
}

func AppendString(b []byte, s string) []byte {
	b = AppendStringHeader(b, uint16(len(s)))
	return append(b, s...)
}
