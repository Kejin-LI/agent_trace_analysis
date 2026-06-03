package logid

import (
	"strconv"
	"strings"

	"code.byted.org/gopkg/ctxvalues"
	"github.com/bytedance/gopkg/lang/fastrand"
)

const (
	// 目前版本为 02
	version    = "02"
	length     = 53
	maxRandNum = 1<<24 - 1<<20
)

// LogID represents a logID generator
type LogID struct{}

// NewLogID create a new LogID instance
func NewLogID() LogID {
	return LogID{}
}

// GenLogID return a new logID string
func (l LogID) GenLogID() string {
	r := fastrand.Uint32n(maxRandNum) + 1<<20
	sb := strings.Builder{}
	sb.Grow(length)
	sb.WriteString(version)
	sb.WriteString(strconv.FormatUint(uint64(getMSTimestamp()), 10))
	sb.Write(localIP)
	sb.WriteString(strconv.FormatUint(uint64(r), 16))
	return sb.String()
}

var defaultLogID LogID

func init() {
	defaultLogID = NewLogID()
}

// GenLogID return a new logID
func GenLogID() string {
	return defaultLogID.GenLogID()
}

// CtxLogIDKey ByteDance Ctx Key of LogID
var CtxLogIDKey = ctxvalues.CTXKeyLogID

// GetLogIDFromCtx return logID from context
var GetLogIDFromCtx = ctxvalues.LogID
