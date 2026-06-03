package bytedmysql

import (
	"context"
	"math/big"
	"net"
	"strconv"

	"code.byted.org/gopkg/env"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	olog "github.com/opentracing/opentracing-go/log"
)

// METHODKEY .
const METHODKEY = "K_METHOD"

type opentracingMiddleWare struct{}

func (o *opentracingMiddleWare) doBefore(ctx context.Context, conn *Connection) {
	// check parent span
	parent := opentracing.SpanFromContext(ctx)
	if !isSampled(parent) {
		return
	}

	reqTrace := conn.reqTrace

	serviceName, svrHandler := env.PSM(), "-"
	if len(serviceName) == 0 || serviceName == "-" {
		serviceName = "toutiao.unknown.unknown"
	}

	if method, ok := ctx.Value(METHODKEY).(string); ok && len(method) != 0 {
		svrHandler = method
	}
	normOperation := serviceName + "::" + svrHandler

	span, _ := opentracing.StartSpanFromContext(ctx, normOperation)
	if span == nil || !isSampled(span) {
		return
	}

	reqTrace.ospan = span
	cfg := conn.Cfg

	// set span tags
	ext.Component.Set(span, "bytedmysql")
	ext.DBType.Set(span, "mysql")
	ext.DBInstance.Set(span, cfg.DBName)
	ext.SpanKindRPCClient.Set(span)
	ext.DBUser.Set(span, cfg.User)
	// addr info
	if host, port, err := net.SplitHostPort(cfg.Addr); err == nil {
		ip := net.ParseIP(host)
		if ip.To4() != nil {
			ext.PeerHostIPv4.Set(span, InetAtoN(ip))
		}
		if ip.To16() != nil {
			ext.PeerHostIPv6.Set(span, ip.String())
		}
		if portNum, err := strconv.Atoi(port); err == nil {
			ext.PeerPort.Set(span, uint16(portNum))
		}
	}
	span.SetTag("idc", env.IDC())
	span.SetTag("cluster", env.Cluster())
	span.SetTag("address", env.IPStr())
	if meshMode(cfg) {
		span.SetTag("meshMode", meshModeStr)
	} else {
		span.SetTag("meshMode", notMeshModeStr)
	}

	ext.DBStatement.Set(span, reqTrace.sql)
	ext.PeerService.Set(span, getPeerService(conn.DBPSM, cfg.DBName)+"::"+reqTrace.operation)

	span.SetTag("table", reqTrace.tableName)
	span.SetTag("component_version", Version)
}

func (o *opentracingMiddleWare) doAfter(conn *Connection) {
	span := conn.reqTrace.ospan
	if span == nil {
		return
	}

	err := conn.reqTrace.err

	ext.Error.Set(span, err != nil)
	// err code
	if err != nil {
		span.SetTag("retCode", getErrorCode(err))
		span.LogFields(olog.String("error.kind", err.Error()))
	}

	span.SetTag("rows_affected", conn.reqTrace.rowsAffected)

	span.Finish()
}

// InetAtoN .
func InetAtoN(ip net.IP) uint32 {
	ret := big.NewInt(0)
	ret.SetBytes(ip.To4())
	return uint32(ret.Uint64())
}

func isSampled(span opentracing.Span) bool {
	if span == nil {
		return false
	}

	// SpanWithIsSampled .
	type SpanWithIsSampled interface {
		IsSampled() bool
	}
	if jSpan, ok := span.(SpanWithIsSampled); ok {
		return jSpan.IsSampled()
	}
	return false
}
