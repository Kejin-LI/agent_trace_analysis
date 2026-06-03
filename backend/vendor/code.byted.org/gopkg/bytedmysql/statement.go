package bytedmysql

import (
	"context"
	"database/sql/driver"
	"errors"
)

// mysqlStmt is the interface collection that the wrapped driver statement implement(go-sql-driver/mysql v1.4.1)
type mysqlStmt interface {
	driver.Stmt
	driver.StmtQueryContext
	driver.StmtExecContext
	driver.ColumnConverter
}

type stmt struct {
	mysqlStmt
	conn *Connection

	sql string // that create the stmt
}

// ErrNotMysqlStmt.
var errNotMysqlStmt = errors.New("[bytedmysql] wrapped stmt is not mysqlStmt")

func newStmt(conn *Connection, si mysqlStmt, sql string) driver.Stmt {
	return &stmt{
		conn:      conn,
		mysqlStmt: si,
		sql:       sql,
	}
}

// ExecContext .
func (s *stmt) ExecContext(ctx context.Context, args []driver.NamedValue) (ri driver.Result, err error) {
	conn := s.conn
	defer func() {
		err = testConvertErr(err)
		conn.recordErr(err)
		if ri != nil {
			if rf, rfErr := ri.RowsAffected(); rfErr == nil {
				conn.incrRowsAffected(rf)
			}
		}
		conn.sqlAfter()
	}()
	conn.sqlBefore(ctx, s.sql, false)

	ri, err = s.mysqlStmt.ExecContext(ctx, args)
	if err != nil {
		return nil, err
	}
	return ri, nil
}

// QueryContext .
func (s *stmt) QueryContext(ctx context.Context, args []driver.NamedValue) (ri driver.Rows, err error) {
	conn := s.conn

	defer func() {
		conn.recordErr(err)
		if err != nil {
			conn.sqlAfter()
		}
	}()
	conn.sqlBefore(ctx, s.sql, false)

	ri, err = s.mysqlStmt.QueryContext(ctx, args)
	if err != nil {
		return nil, err
	}

	mr, ok := ri.(mysqlRows)
	if !ok {
		return nil, errNotMysqlRows
	}

	return newRows(s.conn, mr, conn.sqlAfter), nil
}
