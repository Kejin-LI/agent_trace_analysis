package bytedmysql

import (
	"context"
	"database/sql/driver"
	"errors"
	"net"
	"time"

	"github.com/go-sql-driver/mysql"
)

var errNotMysqlConnection = errors.New("[bytedmysql] wrapped connection is not mysqlConnection")

// mysqlConnection is the interface collection that the wrapped driver connection implement(go-sql-driver/mysql v1.4.1).
type mysqlConnection interface {
	driver.Conn
	driver.Execer
	driver.Queryer

	driver.ConnBeginTx
	driver.ConnPrepareContext
	driver.ExecerContext
	driver.QueryerContext

	driver.NamedValueChecker
	driver.Pinger
	driver.SessionResetter
}

// 实现了 Driver 的各种接口
type Connection struct {
	WrappedConn mysqlConnection
	Cfg         *mysql.Config
	DBPSM       string
	meshConn    *meshConnection

	// bytedtrace param
	enableSpanLog             bool          // default false
	enableSpanMetrics         bool          // default true
	enableSpanStatementRecord bool          // default false
	slowRequestThreshold      time.Duration // no default value
	toDc                      string

	reqTrace *reqTrace
}

// Begin .
func (conn *Connection) Begin() (tx driver.Tx, err error) {
	defer func() {
		conn.recordErr(err)
		conn.sqlAfter()
	}()
	conn.sqlBefore(context.Background(), "begin", false)

	//nolint:staticcheck
	tx, err = conn.WrappedConn.Begin()

	if err != nil {
		return nil, err
	}
	return tx, nil
}

// Close .
func (conn *Connection) Close() (err error) {
	return conn.WrappedConn.Close()
}

// Prepare .
func (conn *Connection) Prepare(query string) (si driver.Stmt, err error) {
	query = interpolate(context.Background(), query)

	defer func() {
		conn.recordErr(err)
		conn.sqlAfter()
	}()
	conn.sqlBefore(context.Background(), query, true)

	si, err = conn.WrappedConn.Prepare(query)
	if err != nil {
		return nil, err
	}

	ms, ok := si.(mysqlStmt)
	if !ok {
		return nil, errNotMysqlStmt
	}

	return newStmt(conn, ms, query), err
}

// Exec .
func (conn *Connection) Exec(query string, args []driver.Value) (ri driver.Result, err error) {
	query = interpolate(context.Background(), query)

	if len(args) == 0 || conn.Cfg.InterpolateParams {
		defer func() {
			conn.recordErr(err)

			if ri != nil {
				if rf, rfErr := ri.RowsAffected(); rfErr == nil {
					conn.incrRowsAffected(rf)
				}
			}

			conn.sqlAfter()
		}()

		conn.sqlBefore(context.Background(), query, false)
	}

	ri, err = conn.WrappedConn.Exec(query, args)

	if err != nil {
		return nil, err
	}
	return ri, err
}

// Query .
func (conn *Connection) Query(query string, args []driver.Value) (ri driver.Rows, err error) {
	query = interpolate(context.Background(), query)

	if len(args) == 0 || conn.Cfg.InterpolateParams {
		defer func() {
			conn.recordErr(err)

			if err != nil {
				conn.sqlAfter()
			}
		}()

		conn.sqlBefore(context.Background(), query, false)
	}

	ri, err = conn.WrappedConn.Query(query, args)
	if err != nil {
		return nil, err
	}

	mr, ok := ri.(mysqlRows)
	if !ok {
		return nil, errNotMysqlRows
	}

	return newRows(conn, mr, conn.sqlAfter), err
}

// Ping .
func (conn *Connection) Ping(ctx context.Context) (err error) {
	defer func() {
		conn.recordErr(err)
		conn.sqlAfter()
	}()
	conn.sqlBefore(ctx, "ping", false)

	err = conn.WrappedConn.Ping(ctx)

	return err
}

// BeginTx .
func (conn *Connection) BeginTx(ctx context.Context, opts driver.TxOptions) (ti driver.Tx, err error) {
	defer func() {
		conn.recordErr(err)
		conn.sqlAfter()
	}()
	conn.sqlBefore(ctx, "begin", false)

	ti, err = conn.WrappedConn.BeginTx(ctx, opts)

	if err != nil {
		return nil, err
	}
	return ti, nil
}

// QueryContext .
func (conn *Connection) QueryContext(
	ctx context.Context,
	query string,
	args []driver.NamedValue,
) (ri driver.Rows, err error) {
	query = interpolate(ctx, query)

	// else query was not executed.
	if len(args) == 0 || conn.Cfg.InterpolateParams {
		defer func() {
			conn.recordErr(err)
			if err != nil {
				conn.sqlAfter()
			}
		}()

		conn.sqlBefore(ctx, query, false)
	}

	ri, err = conn.WrappedConn.QueryContext(ctx, query, args)
	if err != nil {
		return nil, err
	}

	mr, ok := ri.(mysqlRows)
	if !ok {
		return nil, errNotMysqlRows
	}
	return newRows(conn, mr, conn.sqlAfter), err
}

// ExecContext .
func (conn *Connection) ExecContext(ctx context.Context,
	query string,
	args []driver.NamedValue,
) (ri driver.Result, err error) {
	query = interpolate(ctx, query)

	// else query was not executed.
	if len(args) == 0 || conn.Cfg.InterpolateParams {
		defer func() {
			conn.recordErr(err)
			if ri != nil {
				if rf, rfErr := ri.RowsAffected(); rfErr == nil {
					conn.incrRowsAffected(rf)
				}
			}

			conn.sqlAfter()
		}()
		conn.sqlBefore(ctx, query, false)
	}

	ri, err = conn.WrappedConn.ExecContext(ctx, query, args)

	if err != nil {
		return nil, err
	}
	return ri, err
}

// PrepareContext .
func (conn *Connection) PrepareContext(ctx context.Context, query string) (si driver.Stmt, err error) {
	query = interpolate(ctx, query)

	defer func() {
		conn.recordErr(err)
		conn.sqlAfter()
	}()
	conn.sqlBefore(ctx, query, true)

	si, err = conn.WrappedConn.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}

	ms, ok := si.(mysqlStmt)
	if !ok {
		return nil, errNotMysqlStmt
	}

	return newStmt(conn, ms, query), err
}

// CheckNamedValue .
func (conn *Connection) CheckNamedValue(nv *driver.NamedValue) (err error) {
	return conn.WrappedConn.CheckNamedValue(nv)
}

// ResetSession .
func (conn *Connection) ResetSession(ctx context.Context) error {
	return conn.WrappedConn.ResetSession(ctx)
}

func (conn *Connection) recordErr(err error) {
	if conn.reqTrace == nil {
		return
	}

	conn.reqTrace.err = err
	conn.reqTrace.errCode = getErrorCode(err)
	conn.reqTrace.isError = err != nil && !isDuplicateEntryErrCode(conn.reqTrace.errCode)
}

func (conn *Connection) incrRowsAffected(num int64) {
	if conn.reqTrace == nil {
		return
	}

	conn.reqTrace.rowsAffected += num
}

func (conn *Connection) getToAddr() (host, port string) {
	if conn.Cfg == nil {
		return
	}

	if meshMode(conn.Cfg) {
		return meshToAddr, ""
	}

	h, p, err := net.SplitHostPort(conn.Cfg.Addr)
	if err != nil {
		return
	}

	return h, p
}
