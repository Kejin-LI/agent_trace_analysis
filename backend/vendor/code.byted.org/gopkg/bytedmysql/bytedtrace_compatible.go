package bytedmysql

import (
	"context"

	"code.byted.org/bytedtrace/interface-go/compatible"
)

type traceCompatibleMiddleWare struct {
	opentracingMW *opentracingMiddleWare
	bytedtraceMW  *bytedtraceMiddleWare
}

func (w *traceCompatibleMiddleWare) doBefore(ctx context.Context, conn *Connection) {
	if ctx == nil || conn == nil || conn.Cfg == nil || conn.reqTrace == nil {
		return
	}

	tp := compatible.TraceTypeFromContext(ctx)
	if tp == compatible.TraceTypeOnlyOpentracing {
		w.opentracingMW.doBefore(ctx, conn)
	} else {
		w.bytedtraceMW.doBefore(ctx, conn)
	}
}

func (w *traceCompatibleMiddleWare) doAfter(conn *Connection) {
	if conn == nil || conn.reqTrace == nil {
		return
	}

	reqTrace := conn.reqTrace

	if reqTrace.ospan != nil {
		w.opentracingMW.doAfter(conn)
	}

	if reqTrace.bspan != nil {
		w.bytedtraceMW.doAfter(conn)
	}
}
