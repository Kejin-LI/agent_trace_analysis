package bytedmysql

import (
	"context"
	"time"
)

//nolint:gochecknoglobals
var middlewares = []middleware{
	&traceCompatibleMiddleWare{
		opentracingMW: &opentracingMiddleWare{},
		bytedtraceMW:  &bytedtraceMiddleWare{},
	},
	&metricsMiddleWare{},
	&meshMiddleWare{},
}

// middleware
type middleware interface {
	doBefore(ctx context.Context, conn *Connection)
	// doAfter
	doAfter(conn *Connection)
}

func (conn *Connection) sqlBefore(ctx context.Context, sql string, isPrepare bool) {
	if ctx == nil || conn == nil || conn.Cfg == nil || len(sql) == 0 {
		return
	}

	conn.reqTrace = &reqTrace{
		start: time.Now(),
		sql:   sql,
	}
	// prepare 时, operation=prepare, sqlOperation=select/update/insert/delete
	sqlOperation, _ := getOperation(sql)
	operation := "prepare"
	if !isPrepare {
		operation = sqlOperation
	}

	conn.reqTrace.operation = operation
	conn.reqTrace.tableName = getTableName(sqlOperation, sql)

	for _, mw := range middlewares {
		mw.doBefore(ctx, conn)
	}
}

func (conn *Connection) sqlAfter() {
	if conn.reqTrace == nil {
		return
	}
	for i := len(middlewares) - 1; i >= 0; i-- {
		middlewares[i].doAfter(conn)
	}

	conn.reqTrace = nil
}
