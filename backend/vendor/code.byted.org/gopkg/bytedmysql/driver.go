package bytedmysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"log"
	"math/rand"
	"net"
	"os"

	"github.com/go-sql-driver/mysql"

	"code.byted.org/gopkg/consul"
)

const (
	// EnableSpanLogKey .
	EnableSpanLogKey = "enableSpanLog"
	// EnableSpanMetricsKey .
	EnableSpanMetricsKey = "enableSpanMetrics"
	// EnableSpanStatementRecordKey
	EnableSpanStatementRecordKey = "enableSpanStatementRecord"
	// SlowRequestThresholdKey .
	SlowRequestThresholdKey = "slowRequestThreshold"
	// MeshMetaInfo
	// 在 DSN 可以通过 bytedmysql?meshMetaInfo=my_test,my_key2 设置白名单，指定透传 my_test,my_key2 两个 meta info 的 key；
	// 如果没有设置，默认将全部透传到 mesh
	MeshMetaInfo = "meshMetaInfo"
)

var (
	consulLookup        = consul.LookupName // 为了单测
	enableOpenConnTrace = true              // 为了单测

	envMeshMode bool
	egressSock  = os.Getenv("SERVICE_MESH_MYSQL_ADDR")
	withMeshUds = isUnixDomainSocket(egressSock)
)

// Driver .
type Driver struct{}

func init() {
	if os.Getenv("TCE_ENABLE_MYSQL_SIDECAR_EGRESS") == "True" && egressSock != "" {
		envMeshMode = true
	}
}

// ErrNoEndpoints .
var ErrNoEndpoints = errors.New("bytedmysql: consul returned empty endpoint")

var makeConn = func(conn driver.Conn, cfg *Config) (driver.Conn, error) {
	mc, ok := conn.(mysqlConnection)
	if !ok {
		return nil, errNotMysqlConnection
	}

	wrappedConn := &Connection{
		WrappedConn: mc,
		Cfg:         cfg.Config,
		DBPSM:       cfg.To,
		// default emit span metrics
		enableSpanMetrics: true,
	}
	if meshMode(cfg.Config) {
		wrappedConn.meshConn, ok = cfg.ConnCtx.Value(meshConnCtxKey).(*meshConnection)
		if !ok {
			return nil, ErrGetMeshConn
		}
	}
	parseBytedTraceParams(wrappedConn, cfg.Params)

	return wrappedConn, nil
}

// parseBytedTraceParams...
func parseBytedTraceParams(conn *Connection, mp map[string]string) {
	if mp == nil {
		return
	}

	wm := mapwrap(mp)

	if v, exist := wm.getBool(EnableSpanLogKey); exist {
		conn.enableSpanLog = v
	}

	if v, exist := wm.getBool(EnableSpanMetricsKey); exist {
		conn.enableSpanMetrics = v
	}

	if v, exist := wm.getBool(EnableSpanStatementRecordKey); exist {
		conn.enableSpanStatementRecord = v
	}

	if v, exist := wm.getDuration(SlowRequestThresholdKey); exist {
		conn.slowRequestThreshold = v
	}

	if meshMode(conn.Cfg) {
		if v, exist := wm.getSplice(MeshMetaInfo, ","); exist {
			whiteList := make(map[string]bool)
			for _, s := range v {
				whiteList[s] = true
			}
			conn.meshConn.sessionCtx.metaInfoWhiteList = whiteList
		}
	}
}

// extractParams pick needed params.
// expose this method if needed.
func extractParams(cfg *mysql.Config) map[string]string {
	// extract bytedtrace relative
	params := mapwrap(cfg.Params)
	if params == nil || len(params) == 0 {
		return nil
	}

	ret := make(map[string]string, 5)

	// span log
	v := params.get(EnableSpanLogKey)
	if len(v) != 0 {
		ret[EnableSpanLogKey] = v
	}

	// span metrics
	v = params.get(EnableSpanMetricsKey)
	if len(v) != 0 {
		ret[EnableSpanMetricsKey] = v
	}

	// span statement tag
	v = params.get(EnableSpanStatementRecordKey)
	if len(v) != 0 {
		ret[EnableSpanStatementRecordKey] = v
	}

	// slow request threshold
	v = params.get(SlowRequestThresholdKey)
	if len(v) != 0 {
		ret[SlowRequestThresholdKey] = v
	}

	// mesh meta info
	v = params.get(MeshMetaInfo)
	if len(v) != 0 {
		ret[MeshMetaInfo] = v
	}

	return ret
}

// 用于业务封装 conn
func SetMakeConn(f func(conn driver.Conn, cfg *Config) (driver.Conn, error)) {
	makeConn = f
}

// 对 Driver 接口的实现
func (d Driver) Open(dsn string) (driver.Conn, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, err
	}

	to := cfg.Addr
	ctx := context.Background()

	tryToUpdateMeshCfg(cfg)
	// 如果开启 Mesh，走 Mesh
	if meshMode(cfg) {
		ctx = context.WithValue(ctx, meshConnCtxKey, &meshConnection{
			metaInfoStatus: metaInfoInit,
		})
		mysql.RegisterDialContext(meshNetMode, func(dctx context.Context, addr string) (net.Conn, error) {
			var net string
			if withMeshUds {
				net = "unix"
			} else {
				net = "tcp"
			}
			return initMeshConn(dctx, net, egressSock)
		})
		delete(cfg.Params, "use_gdpr_auth")
		return d.open(ctx, cfg, to)
	}

	// 服务发现
	if cfg.Net == "sd" {
		cfg.Net = "tcp"
		dbPsm := cfg.Addr

		tmpEndpoints, err := consulLookup(dbPsm)
		if err != nil {
			return nil, err
		}

		if len(tmpEndpoints) == 0 {
			return nil, ErrNoEndpoints
		}

		endpoints := make(consul.Endpoints, len(tmpEndpoints))

		copy(endpoints, tmpEndpoints)

		rand.Shuffle(len(endpoints), func(i, j int) {
			endpoints[i], endpoints[j] = endpoints[j], endpoints[i]
		})

		var conn driver.Conn

		// retry all endpoints
		for _, ed := range endpoints {
			cfg.Addr = ed.Addr
			conn, err = d.openByToken(ctx, cfg, dbPsm)
			if err == nil {
				return conn, nil
			}
			log.Printf("bytedmysql: connect %s failed with %s", ed.Addr, err.Error())
		}

		return nil, err
	}

	return d.open(ctx, cfg, to)
}

// 通过 Dps Token 获取认证信息并进行连接
func (d Driver) openByToken(ctx context.Context, cfg *mysql.Config, dbPsm string) (driver.Conn, error) {
	// 授权之后，如果是部署在 TCE 的服务，Token 将由 TCE 注入，如果是部署在物理机的服务，Token 从 Dps Agent 获取。
	// 通过 Token，到 DBAuth 服务获取认证的元信息并注入 Cfg 中
	if err := interpolateAuthMeta(cfg, dbPsm); err != nil {
		return nil, err
	}

	return d.open(ctx, cfg, dbPsm)
}

// open a new wrapped Connection
func (d Driver) open(ctx context.Context, cfg *mysql.Config, to string) (conn driver.Conn, err error) {
	if enableOpenConnTrace {
		fakeConn := &Connection{
			Cfg:               cfg,
			enableSpanMetrics: true,
			DBPSM:             to,
			meshConn:          &meshConnection{},
		}
		parseBytedTraceParams(fakeConn, cfg.Params)
		defer func() {
			fakeConn.recordErr(err)
			fakeConn.sqlAfter()
		}()
		fakeConn.sqlBefore(ctx, "conn", false)
	}

	// extract bytedmysql Params and remove from Cfg.Params
	extraParams := extractParams(cfg)

	c, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, err
	}

	wrappedConn, err := c.Connect(ctx)
	if err != nil {
		return nil, err
	}

	bCfg := &Config{
		Config:  cfg,
		ConnCtx: ctx,
		To:      to,
		Params:  extraParams,
	}

	return makeConn(wrappedConn, bCfg)
}

func init() {
	// register bytedmysql driver
	sql.Register("bytedmysql", Driver{})
}
