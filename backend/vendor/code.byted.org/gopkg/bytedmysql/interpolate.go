package bytedmysql

import (
	"context"
	"fmt"
	"os"
	"strings"

	"code.byted.org/gopkg/ctxvalues"
	"code.byted.org/gopkg/env"
)

const (
	unknownIP            = "unknown"
	maxLogIDLength       = 1000
	maxAllowedPacketSize = 4 << 20 // 4 MiB 待确定是跟官方库还是我们自定义
	unknownLogID         = "-"
)

var (
	internalInterpolatedHintSuffix string
	internalInterpolatedHintFormat string
	interpolatedStmt               = map[string]bool{
		// Data Manipulation Statements
		"delete":  true,
		"insert":  true,
		"select":  true,
		"update":  true,
		"replace": true,
	}
)

// 过滤 MySQL 的关键字
func validate(str string) string {
	str = strings.TrimSpace(str)
	str = strings.ToLower(str)
	str = strings.Replace(str, "*", "#", -1)
	str = strings.Replace(str, "delete", "de##te", -1)
	str = strings.Replace(str, "drop", "dr#p", -1)
	str = strings.Replace(str, "update", "up##te", -1)
	return str
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func getOperation(sql string) (string, int) {
	// skip leading space characters
	start := 0
	for start < len(sql) && isSpace(sql[start]) {
		start++
	}

	// find the first separating character
	pos := start
	for pos < len(sql) && isSpace(sql[pos]) == false {
		pos++
	}

	operation := strings.ToLower(sql[start:pos])
	return operation, pos
}
func checkLogID(logID string) bool {
	if len(logID) > maxLogIDLength {
		return false
	}
	if strings.Contains(logID, "*") || strings.Contains(logID, "/") {
		return false
	}
	return true
}

func getInterpolatedHint(ctx context.Context) string {
	var logID string
	if logID = ctxvalues.LogIDDefault(ctx); logID != unknownLogID {
		if !checkLogID(logID) {
			logID = unknownLogID
		}
	}

	kEnv := ctxvalues.EnvDefault(ctx)
	return fmt.Sprintf(internalInterpolatedHintFormat, logID, kEnv)
}

func init() {
	serviceName := env.PSM()
	if serviceName == env.PSMUnknown {
		serviceName = "toutiao.known.known"
	}
	if len(serviceName) > 200 {
		serviceName = serviceName[:200]
	}
	serviceName = validate(serviceName)

	ip := env.HostIP()
	ip = validate(ip)
	idc := env.IDC()
	cluster := env.Cluster()
	tceEnv := env.Env()

	internalInterpolatedHintSuffix = fmt.Sprintf("ip=%s, tce_env=%s, cluster=%s, idc=%s, pid=%d */ ", ip, tceEnv, cluster, idc, os.Getpid())    // get-table-name 的时候，用固定字符串 index，不能包含 logid
	internalInterpolatedHintFormat = fmt.Sprintf(" /* psm=%s, logid=%s, k_env=%s, %s", serviceName, "%s", "%s", internalInterpolatedHintSuffix) // 完整注入 sql format
}

// 将 PSM，IP，pid 等信息注入 SQL 中，如果注入出错，则返回原 SQL
func interpolate(ctx context.Context, sql string) string {
	if strings.Index(sql, "/*") > 0 &&
		strings.Index(sql, "*/") > 0 &&
		strings.Index(sql, "psm=") > 0 {
		return sql
	}

	interpolatedHint := getInterpolatedHint(ctx)

	if len(sql)+len(interpolatedHint) > maxAllowedPacketSize {
		return sql
	}

	operation, pos := getOperation(sql)
	if _, ok := interpolatedStmt[operation]; ok {
		sql = sql[:pos] + interpolatedHint + sql[pos:]
	}
	return sql
}
