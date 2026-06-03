package utils

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"code.byted.org/gopkg/env"

	"code.byted.org/gopkg/ctxvalues"
	"code.byted.org/security/sensitive_finder_engine/mask_pack"
)

const (
	modeMaskDefault = iota // 默认关闭状态
	modeMaskOn             // 开启状态
)

var maskMode = modeMaskDefault

func SetMaskMode(flag bool) {
	if flag {
		maskMode = modeMaskOn
	} else {
		maskMode = modeMaskDefault
	}
}

func FmtSprintf(format string, v ...interface{}) string {
	if maskMode == modeMaskOn {
		v = mask_pack.MaskInterfaceSlice(v...)
	}

	return fmt.Sprintf(format, v...)
}

func Value2Str(v interface{}) string {
	switch tv := v.(type) {
	case nil:
		return ""
	case bool:
		if tv == true {
			return "true"
		} else {
			return "false"
		}
	case string:
		return tv
	case []byte:
		return string(tv)
	case error:
		return tv.Error()
	case int:
		return strconv.Itoa(tv)
	case int16:
		return strconv.FormatInt(int64(tv), 10)
	case int32:
		return strconv.FormatInt(int64(tv), 10)
	case int64:
		return strconv.FormatInt(int64(tv), 10)
	case uint:
		return strconv.FormatUint(uint64(tv), 10)
	case uint16:
		return strconv.FormatUint(uint64(tv), 10)
	case uint32:
		return strconv.FormatUint(uint64(tv), 10)
	case uint64:
		return strconv.FormatUint(uint64(tv), 10)
	case float32:
		return strconv.FormatFloat(float64(tv), 'f', 3, 32)
	case float64:
		return strconv.FormatFloat(float64(tv), 'f', 3, 32)
	default:
		if maskMode == modeMaskOn {
			v = mask_pack.MaskInterface(v)
		}
		return fmt.Sprint(v)
	}
}

// Deprecated: please use ctxvalues.LogIDDefault for logid getter
var LogIDFromContext = ctxvalues.LogIDDefault

func SpanIDFromContext(ctx context.Context) uint64 {
	if ctx == nil {
		return 0
	}
	// K_SPANID is injected by bytedtrace (https://code.byted.org/bytedtrace/bytedtrace-client-go)
	val := ctx.Value("K_SPANID")
	if val != nil {
		spanID, valid := val.(uint64)
		if valid {
			return spanID
		}
	}
	return 0
}

func IsString(v interface{}) bool {
	switch v.(type) {
	case string, fmt.Stringer:
		return true
	default:
		return false
	}
}

func IsStrNumeric(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

func IsStrNumDot(s string) bool {
	if len(s) == 0 {
		return false
	}
	dotFound := false
	for _, v := range s {
		if v == '.' {
			if dotFound {
				return false
			}
			dotFound = true
		} else if v < '0' || v > '9' {
			return false
		}
	}
	return true
}

type RegionType int32

const (
	REGION_NORMAL RegionType = iota
	REGION_TTP
	REGION_GCP
)

func DetermineRegion() RegionType {
	if env.Region() == env.R_USTTP || env.Region() == env.R_USTTP2 ||
		os.Getenv("LOGS_IN_TTP") == "true" {
		return REGION_TTP
	} else if env.IDC() == "useast2a" ||
		os.Getenv("LOGS_IN_GCP") == "true" {
		return REGION_GCP
	}
	return REGION_NORMAL
}
