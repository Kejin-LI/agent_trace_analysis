package bytedmysql

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	bt "code.byted.org/bytedtrace/interface-go"
	bte "code.byted.org/bytedtrace/interface-go/ext"
	"github.com/go-sql-driver/mysql"
	"github.com/opentracing/opentracing-go"
)

const (
	meshToAddr = "mysql.sock"

	peerPortTagKey = "peer.port"

	meshModeStr    = "1"
	notMeshModeStr = "0"
)

func init() {
	_ = bt.AppendSpanMetricTags(bt.ClientSpanType,
		string(bte.MeshTag),
		string(bte.TableTag),
		string(bte.ComponentVersionTag),
		string(bte.LanguageTag),
		peerPortTagKey,
	)
}

var posttraceIgnoreErr = map[int]struct{}{
	MysqlDropRequest: {},
}

type reqTrace struct {
	// opentracing
	ospan opentracing.Span
	// bytedtrace
	bspan bt.Span

	// start time
	start time.Time

	// err
	err error
	// err code
	errCode uint16
	// is error
	isError bool

	// rows affected
	rowsAffected int64

	// sql
	sql string
	// operation
	// begin/prepare/select/insert/update/delete...
	operation string
	// table name
	tableName string
}

type bytedtraceMiddleWare struct{}

func (b *bytedtraceMiddleWare) doBefore(ctx context.Context, conn *Connection) {
	reqTrace := conn.reqTrace

	opts := make([]bt.StartSpanOption, 0, 3)
	opts = append(opts, bt.EnableEmitRemoteAddr)

	if conn.enableSpanLog {
		opts = append(opts, bt.EnableEmitSpanLog)
	} else {
		opts = append(opts, bt.DisableEmitSpanLog)
	}

	if conn.enableSpanMetrics {
		opts = append(opts, bt.EnableEmitSpanMetrics)
	} else {
		opts = append(opts, bt.DisableEmitSpanMetrics)
	}

	cfg := conn.Cfg

	span, _ := bt.StartClientSpan(ctx, getPeerService(conn.DBPSM, cfg.DBName), opts...)
	if span == nil {
		return
	}

	reqTrace.bspan = span

	bt.SetToMethod(span, reqTrace.operation)
	bt.SetComponent(span, "bytedmysql")
	bt.SetServiceType(span, "mysql")

	h, p := conn.getToAddr()

	if len(h) != 0 {
		bt.SetToAddr(span, h)
	}

	if len(p) != 0 {
		span.SetTag(peerPortTagKey, p)
	}

	if cfg.DBName != "" {
		span.SetTag("db.instance", cfg.DBName)
	}

	if cfg.User != "" {
		span.SetTag("db.user", cfg.User)
	}

	if conn.enableSpanStatementRecord {
		span.SetTag("db.statement", reqTrace.sql)
	}
	if meshMode(cfg) {
		bte.MeshTag.Set(span, meshModeStr)
		if len(conn.toDc) == 0 {
			if parts := strings.Split(conn.DBPSM, ".service."); len(parts) == 2 {
				conn.toDc = parts[1]
			} else {
				conn.toDc = os.Getenv("IDC_NAME")
			}
		}
		bt.SetToDc(span, conn.toDc)
	} else {
		bte.MeshTag.Set(span, notMeshModeStr)
	}

	bte.TableTag.Set(span, reqTrace.tableName)
	bte.ComponentVersionTag.Set(span, Version)
	bte.LanguageTag.Set(span, "go")
}

func (b *bytedtraceMiddleWare) doAfter(conn *Connection) {
	span := conn.reqTrace.bspan
	if span == nil {
		return
	}

	defer span.Finish()

	err := conn.reqTrace.err

	// check need posttrace
	if needPostTrace(span, conn) {
		span.SetPostTrace(nil)
	}

	span.SetTag("rows_affected", conn.reqTrace.rowsAffected)

	bt.SetIsError(span, conn.reqTrace.isError)

	if err != nil {
		code := conn.reqTrace.errCode
		bt.SetStatusCode(span, int32(code))
		span.AddEvents(bt.NewErrorLogEvent(fmt.Sprintf("err-%d", code)).SetContent(err.Error()).SetEmitLog(false))
	}
}

func needPostTrace(span bt.Span, conn *Connection) bool {
	if span.IsSampled() {
		return false
	}

	reqTrace := conn.reqTrace

	err := reqTrace.err
	if reqTrace.isError && !isPosttraceIgnoreErr(err) {
		return true
	}

	if conn.slowRequestThreshold != 0 && time.Since(reqTrace.start) > conn.slowRequestThreshold {
		return true
	}

	return false
}

// isPosttraceIgnoreErr.
func isPosttraceIgnoreErr(err error) bool {
	mysqlErr, ok := err.(*mysql.MySQLError)
	if !ok {
		return false
	}

	if _, exist := posttraceIgnoreErr[int(mysqlErr.Number)]; exist {
		return true
	}

	return false
}

// getPeerService get db psm
// param dbpsm is ip:port when ip:port is specified in dsn
// convert to ${db_name}_port
func getPeerService(dbpsm string, dbName string) string {
	arr := strings.Split(dbpsm, ":")
	if len(arr) == 1 {
		return dbpsm
	}

	if len(arr) != 2 || len(dbName) == 0 {
		return dbpsm
	}

	return dbName + "_" + arr[1]
}

// testConvertErr will be mocked  in test and returns real err.
func testConvertErr(err error) error {
	return err
}

func isDuplicateEntryErrCode(errCode uint16) bool {
	return errCode == MysqlDuplicateEntry
}
