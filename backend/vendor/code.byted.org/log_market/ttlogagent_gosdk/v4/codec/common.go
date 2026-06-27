package codec

import (
	"errors"
	"fmt"
)

const (
	// 私有协议内部用
	StringType byte = 0 //长度小于255的string
	BoolType   byte = 1
	IntType    byte = 2  //4字节, 8,16,32位有符号,byte,8,16位无符号
	LongType   byte = 3  //8字节, 64位有符号,int
	Uint64Type byte = 4  //8字节, 32,64位无符号
	DoubleType byte = 5  //8字节, float32,float64
	DateType   byte = 20 //暂不支持
	Ipv4Type   byte = 21
	Ipv6Type   byte = 22
	TextType   byte = 23 //长度大于255的string
	BytesType  byte = 24 //二进制字符流
	UUIdType   byte = 25 //128位
)

const (
	DefaultTenant        = "default"
	DefaultLogStreamName = "default"
)

const (
	StringSplitByte          byte = 0x00
	LENGTH_BYTES                  = 4
	BYTELOG_IPV4_BYTES            = 4
	BYTELOG_IPV6_BYTES            = 16
	UUID_LENGTH                   = 32
	SEQ_ID_LENGTH                 = 16
	BYTELOG_FLAG_BYTES            = 4
	SHORT_STRING_MAX_LEN          = 0xFF
	LONG_STRING_MAX_LEN           = 0xFFFFFFFF
	oneMessageLimitByte           = 128 << 10 // set to 128k due to uds limit
	oneMessageLimitLogNumber      = 4096
	largeMessageLimitByte         = 10 << 20 // 10MB
	// BATCH_ID_LENGTH               = UUID_LENGTH + SEQ_ID_LENGTH
)

const (
	SDK_MAGIC_NUM_STR                           = "SLOG"
	SDK_VERSION_NUM                             = 0
	SDK_HEADER_LENGTH_POS                       = 0
	SDK_HEADER_TIMESTAMP_POS                    = 8
	SDK_HEADER_LOG_COUNT_POS                    = 16
	SDK_HEADER_FIXED_LEN                        = 20 /* 4 + 4 + 8 + 2 + 1 + 1*/
	BYTELOG_HEADER_COMPRESSION_TYPE_POS         = 2
	BYTELOG_HEADER_RESERVED_FLAG_POS            = 3
	BYTELOG_HEADER_TOTAL_LENGTH_POS             = 4
	BYTELOG_HEADER_COMP_HEADER_LENGTH_POS       = 8
	BYTELOG_HEADER_ORIN_HEADER_LENGTH_POS       = 12
	BYTELOG_HEADER_CHECKSUM_POS                 = 16
	BYTELOG_HEADER_RESERVED_POS                 = 20
	BYTELOG_HEADER_LEN                          = 24
	CONTENT_COMPRESSED_LEN_POS                  = 0
	CONTENT_ORIGIN_LEN_POS                      = 4
	CONTENT_COMPRESSION_TYPE_POS                = 8
	CONTENT_COMPRESS_INFO_LEN                   = 9
	DISTANCE_BTW_BODY_LEN_POS_AND_BODY          = 20
	DISTANCE_BTW_HEADER_COMP_LEN_POS_AND_HEADER = 16
	DISTANCE_BTW_HEADER_ORIN_LEN_POS_AND_HEADER = 12
	// COMMONHEADER_SEQID_POS                      = 49 // 4 + 1 + 1 + 8 (key_batchid) + 1 + 1 + 1 + 32(uuid)
	// COMMONHEADER_FLAG_POS                       = 76 // 4 + 62 (batchid) + 1 + 1 + 6 (_flags) + 1 + 1
)

const (
	// Common Headers
	KEY_PSM      = "_psm"
	KEY_IDC      = "_idc"
	KEY_STAGE    = "_stage"
	KEY_CLUSTER  = "_cluster"
	KEY_PODNAME  = "_podname"
	KEY_TASKNAME = "_taskname"
	KEY_LANGUAGE = "_language"
	KEY_IPV4     = "_ipv4"
	KEY_IPV6     = "_ipv6"
	KEY_BATCH_ID = "_batchid"
	KEY_FLAGS    = "_flags"

	// Customized Headers
	KEY_LEVEL     = "_level"
	KEY_LOCATION  = "_location"
	KEY_LOGID     = "_logid" /* reuse the preset member __logid in Log Header */
	KEY_SPANID    = "_spanid"
	KEY_TIMESTAMP = "_timestamp" /* reuse the preset member __timestamp in Log Header */
	KEY_MSG       = "_msg"
)

const (
	Trace   = "Trace"
	Debug   = "Debug"
	Info    = "Info"
	Notice  = "Notice"
	Warn    = "Warn"
	Warning = "Warning"
	Error   = "Error"
	Fatal   = "Fatal"
)

var (
	BYTELOG_VERSION             byte   = 0
	BYTELOG_PROTOTYPE           byte   = 0
	BYTELOG_RESERVED_FLAG       byte   = 0
	BYTELOG_CHECKSUM            uint32 = 0
	BYTELOG_RESERVED            uint32 = 0
	BYTELOG_COMPRESSION_NONE    byte   = 0
	DEFAULT_HEADER_COMPRESSION         = BYTELOG_COMPRESSION_NONE
	DEFAULT_CONTENT_COMPRESSION        = BYTELOG_COMPRESSION_NONE
)

const (
	FlagMaskContainsErrorLog byte = 0x01
	FlagMaskQuota75Percent   byte = 0x02
	FlagMaskQuota100Percent  byte = 0x04
)

var (
	emptyStrForByteLog = make([]byte, 3)
	emptyFourBytesData = make([]byte, 4)
)

type (
	Ipv4 uint32
	Ipv6 [16]byte
)

var (
	ErrEmptyBatch                = errors.New("empty batch")
	ErrNoEnoughBytes             = errors.New("no enough bytes")
	ErrNilLogHeaders             = errors.New("nil log headers")
	ErrNilLogContent             = errors.New("nil log content")
	ErrNilCommonHeaders          = errors.New("nil common headers")
	ErrInvalidLogHeader          = errors.New("invalid Log Header")
	ErrNilDataPack               = errors.New("invalid Data Pack")
	ErrTooLargeDataPack          = errors.New("too Large Data Pack")
	ErrFailToTruncateDataPack    = errors.New("failed to truncate the log")
	ErrNegativeSizeLimit         = errors.New("negative size limit")
	ErrLengthInSDKHeaderMismatch = errors.New("log length not match")
	ErrExceedOneMessageSizeLimit = errors.New("exceed the size limit for one message")
	ErrTooManyLogs               = errors.New("too many logs in this batch")
	ErrDifferentTimeWindows      = errors.New("not the same time window")
	// ErrDifferentLevelForErrorLog means this log's level is different from this batch which logs' level are all
	// error or not. And error log contain warn, warning, error and fetal level log.
	ErrDifferentLevelForErrorLog = errors.New("not the same log level for error log")
	ErrNilObject                 = errors.New("nil object")
	ErrTooLongKV                 = errors.New("too long kv")
	ErrTooLongLogStreamId        = errors.New("too long log stream id")
	ErrMagicNumNotMatch          = errors.New("magic number not match")

	ErrHeadersRegionLengthNotMatch = errors.New("headersRegion length not match")
	ErrLogHeadersLengthNotMatch    = errors.New("logHeaders length not match")
	ErrLogContentsLengthNotMatch   = errors.New("logContents length not match")
	ErrCompressTypeNotSupport      = errors.New("compress type not support")
	ErrChecksumVerifyFailed        = errors.New("verify checksum failed")
	ErrPatternDicNotSupport        = errors.New("pattern with dic not support")
)

func (ip Ipv4) isEmpty() bool {
	return ip == 0
}

func (ip Ipv4) String() string {
	return fmt.Sprintf("%d.%d.%d.%d", byte(ip), byte(ip>>8), byte(ip>>16), byte(ip>>24))
}

func (ip *Ipv6) isEmpty() bool {
	for _, eachByte := range *ip {
		if eachByte != 0 {
			return false
		}
	}
	return true
}

// net.IP String()
func (ip *Ipv6) String() string {
	// Find longest run of zeros.
	e0 := -1
	e1 := -1
	for i := 0; i < 16; i += 2 {
		j := i
		for j < 16 && ip[j] == 0 && ip[j+1] == 0 {
			j += 2
		}
		if j > i && j-i > e1-e0 {
			e0 = i
			e1 = j
			i = j
		}
	}
	// The symbol "::" MUST NOT be used to shorten just one 16 bit 0 field.
	if e1-e0 <= 2 {
		e0 = -1
		e1 = -1
	}

	b := make([]byte, 0, maxLen)

	// Print with possible :: in place of run of zeros
	for i := 0; i < 16; i += 2 {
		if i == e0 {
			b = append(b, ':', ':')
			i = e1
			if i >= 16 {
				break
			}
		} else if i > 0 {
			b = append(b, ':')
		}
		b = appendHex(b, (uint32(ip[i])<<8)|uint32(ip[i+1]))
	}
	return string(b)
}

const hexDigit = "0123456789abcdef"
const maxLen = len("ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff")

func appendHex(dst []byte, i uint32) []byte {
	if i == 0 {
		return append(dst, '0')
	}
	for j := 7; j >= 0; j-- {
		v := i >> uint(j*4)
		if v > 0 {
			dst = append(dst, hexDigit[v&0xf])
		}
	}
	return dst
}
