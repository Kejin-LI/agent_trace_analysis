package bytedmysql

import (
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
)

var errNotMysqlRows = errors.New("[bytedmysql] wrapped rows is not mysqlRows")

// mysqlRows is the interface collection that the wrapped driver rows implement(go-sql-driver/mysql v1.4.1)
type mysqlRows interface {
	driver.Rows
	// driver.RowsColumnTypeDatabaseTypeName
	ColumnTypeDatabaseTypeName(index int) string
	// driver.RowsColumnTypeNullable
	ColumnTypeNullable(index int) (nullable, ok bool)
	// driver.RowsColumnTypePrecisionScale
	ColumnTypePrecisionScale(index int) (precision, scale int64, ok bool)
	// driver.RowsColumnTypeScanType
	ColumnTypeScanType(index int) reflect.Type
	// driver.RowsNextResultSet
	HasNextResultSet() bool
	NextResultSet() error
}

type rows struct {
	mysqlRows
	conn *Connection
	// finishHook is not nil if rows is create by Query
	finishHook func()
}

func newRows(conn *Connection, ri mysqlRows, finishHook func()) driver.Rows {
	return &rows{
		mysqlRows:  ri,
		conn:       conn,
		finishHook: finishHook,
	}
}

// Next .
func (r *rows) Next(dest []driver.Value) (err error) {
	defer func() {
		if r.conn == nil || err == io.EOF {
			return
		}
		if err != nil {
			r.conn.recordErr(err)
		}
		r.conn.incrRowsAffected(1)
	}()

	return r.mysqlRows.Next(dest)
}

// Close .
func (r *rows) Close() error {
	if r.finishHook != nil {
		r.finishHook()
		r.finishHook = nil
	}
	return r.mysqlRows.Close()
}
