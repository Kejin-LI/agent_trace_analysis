package bytedmysql

import (
	"database/sql/driver"

	"github.com/go-sql-driver/mysql"
)

const (
	errUnAssignedErrorCode = 100

	// mesh error code
	// see https://bytedance.feishu.cn/wiki/wikcnwSA5vc0wZ1gK44JY1pYrAb

	// Duplicate Entry
	MysqlDuplicateEntry = 1062 // insert 主键重复
	// MysqlHandshakeErr ...
	MysqlHandshakeErr = 1104 // 握手失败
	// MysqlConnMissToken ...
	MysqlConnMissToken = 2000 // 鉴权缺少token
	// MysqlConnAuthFail ...
	MysqlConnAuthFail = 2001 // 鉴权失败
	// MysqlConnUpstreamNotFound ...
	MysqlConnUpstreamNotFound = 2002 // 发现不了mysql实例
	// MysqlConnAllUpstreamFailed ...
	MysqlConnAllUpstreamFailed = 2003 // 连接不上mysql
	// MysqlConnRefuse ...
	MysqlConnRefuse = 2004 // 连接的时候被mysql拒绝
	// MysqlConnTimeout ...
	MysqlConnTimeout = 2005 // 连接mysql超时
	// MysqlRequestTimeout ...
	MysqlRequestTimeout = 2021 // 请求超时
	// MysqlDropRequest ...
	MysqlDropRequest = 2022 // 请求降级
	// MysqlFaultInjection ...
	MysqlFaultInjection = 2023 // 故障注入
)

func getErrorCode(err error) uint16 {
	if err == nil {
		return 0
	}

	if err == driver.ErrSkip {
		return 0
	}

	if mysqlError, ok := err.(*mysql.MySQLError); ok {
		return mysqlError.Number
	}
	return errUnAssignedErrorCode
}
