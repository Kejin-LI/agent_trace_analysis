package bytedmysql

import (
	"bufio"
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/cloudwego/gopkg/bufiox"
	"github.com/cloudwego/gopkg/protocol/ttheader"
	"github.com/cloudwego/localsession/backup"
	"github.com/go-sql-driver/mysql"

	"code.byted.org/gopkg/ctxvalues"
	"code.byted.org/gopkg/metainfo"
)

const (
	// Initial state, no handshake performed
	metaInfoInit = iota
	// Handshake completed, but disabled by remote
	metaInfoDisabled
	// Handshake completed, using native protocol
	metaInfoByNative
	// Handshake completed, using TTheader protocol
	metaInfoByTTheader
)

var (
	cmdPrefix = "SELECT VERSION() /* MESH_SESSION_CTX "
	cmdSuffix = " */"
	padding   = []byte{0x01, 0x20} // version 1 and space

	meshNetMode    = "mesh"
	meshConnCtxKey = "MESH_CONN_CONTEXT_KEY"
	// offset of frame length
	offsetFrameLength = 4
	// for handshake
	meshTTHeaderText      = "bytemesh-ttheader"
	meshTTHeaderHandshake = []byte("{\"with_ttheader\":true}")
	// for specific meta info
	metaInfoTableKey     = "mit"
	metaInfoStatementKey = "mist"

	ErrGetMeshConn = fmt.Errorf("bytedmysql: get mesh connection failed")
)

// tryToUpdateMeshCfg will be executed once for each connection,
// and cfg.Net will be marked as supporting/unsupporting mesh.
func tryToUpdateMeshCfg(cfg *mysql.Config) {
	if cfg == nil || !envMeshMode {
		return
	}

	// TODO: tcp(ip:port) or tcp(domain:port) is not support in mesh so far
	//       keep it until next version
	if cfg.Net == "tcp" {
		return
	}

	cfg.Net = meshNetMode
	cfg.User = cfg.Addr
	cfg.Addr = egressSock
	// disable tls between sdk and mesh
	cfg.TLSConfig = "false"
	cfg.TLS = nil
}

// cfg.Net will be updated in the tryToUpdateMeshCfg, and then we only need to
// judge the value of cfg.Net in meshMode to know whether to use mesh
func meshMode(cfg *mysql.Config) bool {
	return cfg.Net == meshNetMode
}

type sessionCtx struct {
	LogID             string            `json:"log_id"`
	StressTag         string            `json:"stress_tag"`
	IngressMethod     string            `json:"ingress_method"`
	ClientEnv         string            `json:"client_env"`
	Table             string            `json:"table"`
	Statement         string            `json:"statement"`
	MetaInfo          map[string]string `json:"metainfo"`
	cached            []byte            `json:"-"`
	metaInfoWhiteList map[string]bool   `json:"-"`
}

type meshMiddleWare struct{}

func sendContextWithSQL(conn *Connection, content []byte) (driver.Rows, error) {
	var sql strings.Builder
	sql.Grow(100)
	sql.WriteString(cmdPrefix)
	sql.Write(padding)
	sql.Write(content)
	sql.WriteString(cmdSuffix)
	return conn.WrappedConn.Query(sql.String(), nil)
}

func parseHandshake(rows driver.Rows, conn *Connection) {
	defer rows.Close()
	if len(rows.Columns()) == 0 {
		conn.meshConn.metaInfoStatus = metaInfoByNative
		return
	}
	dest := make([]driver.Value, 1)
	for {
		if rows.Next(dest) != nil { // read all data
			break
		}
		b, ok := dest[0].([]byte)
		if ok {
			if string(b) == meshTTHeaderText { // bytemesh, use ttheader
				conn.meshConn.metaInfoStatus = metaInfoByTTheader
				return
			}
		}
	}
	// default, disabled
	conn.meshConn.metaInfoStatus = metaInfoDisabled
}

// update session ctx
// select version() /* mesh_session_ctx 0x01    {"a":"b"} */
func (mesh *meshMiddleWare) doBefore(ctx context.Context, conn *Connection) {
	if conn.reqTrace.sql == "conn" || conn.meshConn == nil || !meshMode(conn.Cfg) {
		return
	}

	// merge backup context if no log_id found in ctx
	ctx = backup.RecoverCtxOnDemands(ctx, backupMeshMetaInfoCtx)
	conn.meshConn.ctx = ctx
	conn.meshConn.reqTrace = conn.reqTrace

	if conn.meshConn.metaInfoStatus == metaInfoInit { // only do in handshake
		rows, err := sendContextWithSQL(conn, meshTTHeaderHandshake)
		if err != nil { // connection has marked as bad
			return
		}
		parseHandshake(rows, conn)
	}

	if conn.meshConn.metaInfoStatus != metaInfoByNative {
		return
	}

	// send json session ctx
	m := &conn.meshConn.sessionCtx
	if val, ok := ctxvalues.LogID(ctx); ok {
		m.LogID = val
	}
	if val, ok := ctxvalues.StressTag(ctx); ok {
		m.StressTag = val
	}
	if val, ok := ctxvalues.Method(ctx); ok {
		m.IngressMethod = val
	}
	if val, ok := ctxvalues.Env(ctx); ok {
		m.ClientEnv = val
	}
	m.Table = conn.reqTrace.tableName
	m.Statement, _ = getOperation(conn.reqTrace.sql)
	m.Statement = strings.ToUpper(m.Statement)
	info := make(map[string]string)
	// follow Thrift rules
	for k, v := range metainfo.GetAllValues(ctx) {
		if m.metaInfoWhiteList != nil {
			if _, exists := m.metaInfoWhiteList[k]; !exists {
				continue
			}
		}
		info[metainfo.PrefixTransient+strings.Replace(k, "-", "_", -1)] = v
	}
	for k, v := range metainfo.GetAllPersistentValues(ctx) {
		if m.metaInfoWhiteList != nil {
			if _, exists := m.metaInfoWhiteList[k]; !exists {
				continue
			}
		}
		info[metainfo.PrefixPersistent+strings.Replace(k, "-", "_", -1)] = v
	}
	m.MetaInfo = info

	content, _ := json.Marshal(m)
	if bytes.Compare(m.cached, content) == 0 {
		return
	}
	m.cached = content
	sendContextWithSQL(conn, content)
}

func (mesh *meshMiddleWare) doAfter(conn *Connection) {
}

type meshConnection struct {
	metaInfoStatus uint8
	ctx            context.Context
	conn           net.Conn
	reqTrace       *reqTrace
	reader         *bufio.Reader
	sessionCtx     sessionCtx
	seqId          int32
	param          ttheader.DecodeParam

	ttheaderRemainPayload int
}

var _ net.Conn = (*meshConnection)(nil)

func initMeshConn(ctx context.Context, netMode, addr string) (net.Conn, error) {
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, netMode, addr)
	if err != nil {
		return nil, err
	}
	// must use mesh
	m, ok := ctx.Value(meshConnCtxKey).(*meshConnection)
	if !ok {
		return nil, ErrGetMeshConn
	}
	m.conn = conn
	m.reader = bufio.NewReader(conn)
	return m, nil
}

func updateFrameLength(b []byte) {
	binary.BigEndian.PutUint32(b, uint32(len(b)-offsetFrameLength))
}

func (m *meshConnection) Read(b []byte) (n int, err error) {
	if m.metaInfoStatus != metaInfoByTTheader {
		return m.conn.Read(b)
	}
	if m.ttheaderRemainPayload == 0 {
		peeked, err := m.reader.Peek(ttheader.TTHeaderMetaSize)
		if err != nil {
			return 0, err
		}
		headerInfoSize := ttheader.Bytes2Uint16NoCheck(peeked[ttheader.Size32*3:ttheader.TTHeaderMetaSize]) * 4
		if uint32(headerInfoSize) > ttheader.MaxHeaderSize {
			return 0, fmt.Errorf("invalid ttheader header length[%d]", headerInfoSize)
		}
		start := 0
		headerSize := ttheader.TTHeaderMetaSize + int(headerInfoSize)
		buf := make([]byte, headerSize)
		for start < headerSize {
			l, err := m.reader.Read(buf[start:])
			if err != nil {
				return 0, err
			}
			start = start + l
		}
		in := bufiox.NewBytesReader(buf)
		m.param, err = ttheader.Decode(m.ctx, in)
		if err != nil {
			return 0, fmt.Errorf("decode meta info failed: %s", err.Error())
		}
		_ = in.Release(nil)
		m.ttheaderRemainPayload = m.param.PayloadLen
	}
	n = len(b)
	if m.ttheaderRemainPayload < n {
		n = m.ttheaderRemainPayload
	}
	n, err = m.reader.Read(b[:n])
	m.ttheaderRemainPayload = m.ttheaderRemainPayload - n
	return n, err
}

// must be a complete mysql package, and less than maxPacketSize(1<<24 - 1)
func (m *meshConnection) Write(b []byte) (n int, err error) {
	if m.metaInfoStatus != metaInfoByTTheader {
		return m.conn.Write(b)
	}
	ctx := m.ctx
	intInfo := make(map[uint16]string, 4)
	strInfo := make(map[string]string)
	if val, ok := ctxvalues.LogID(ctx); ok {
		intInfo[ttheader.LogID] = val
	}
	if val, ok := ctxvalues.StressTag(ctx); ok {
		intInfo[ttheader.StressTag] = val
	}
	if val, ok := ctxvalues.Method(ctx); ok {
		intInfo[ttheader.FromMethod] = val
	}
	if val, ok := ctxvalues.Env(ctx); ok {
		intInfo[ttheader.Env] = val
	}
	// follow Thrift rules
	for k, v := range metainfo.GetAllValues(ctx) {
		strInfo[metainfo.PrefixTransient+strings.Replace(k, "-", "_", -1)] = v
	}
	for k, v := range metainfo.GetAllPersistentValues(ctx) {
		strInfo[metainfo.PrefixPersistent+strings.Replace(k, "-", "_", -1)] = v
	}
	strInfo[metaInfoTableKey] = m.reqTrace.tableName
	statement, _ := getOperation(m.reqTrace.sql)
	strInfo[metaInfoStatementKey] = strings.ToUpper(statement)
	m.seqId++
	param := ttheader.EncodeParam{
		Flags:      0,
		SeqID:      m.seqId,
		ProtocolID: 0,
		IntInfo:    intInfo,
		StrInfo:    strInfo,
	}
	var buf []byte
	out := bufiox.NewBytesWriter(&buf)
	if _, err := ttheader.Encode(ctx, param, out); err != nil {
		return 0, fmt.Errorf("encode meta info failed: %s", err.Error())
	}
	if err := out.Flush(); err != nil {
		return 0, fmt.Errorf("encode meta info failed: %s", err.Error())
	}
	ttheaderLen := len(buf)
	buf = append(buf, b...)
	updateFrameLength(buf)
	start := 0
	for start < len(buf) {
		n, err = m.conn.Write(buf[start:])
		start = start + n
		if err != nil {
			if start <= ttheaderLen {
				return 0, err
			}
			return start - ttheaderLen, err
		}
	}
	return len(b), nil
}

func (m *meshConnection) Close() error {
	return m.conn.Close()
}

func (m *meshConnection) LocalAddr() net.Addr {
	return m.conn.LocalAddr()
}

func (m *meshConnection) RemoteAddr() net.Addr {
	return m.conn.RemoteAddr()
}

func (m *meshConnection) SetDeadline(t time.Time) error {
	return m.conn.SetDeadline(t)
}

func (m *meshConnection) SetReadDeadline(t time.Time) error {
	return m.conn.SetReadDeadline(t)
}

func (m *meshConnection) SetWriteDeadline(t time.Time) error {
	return m.conn.SetWriteDeadline(t)
}

var meshMetaInfoCtxKeys = []string{
	"K_LOGID",
	"K_STRESS",
	"K_METHOD",
	"K_ENV",
}

func backupMeshMetaInfoCtx(pre, cur context.Context) (context.Context, bool) {
	// no need backup
	if _, ok := ctxvalues.LogID(cur); ok {
		return cur, false
	}

	ctx := cur
	for _, key := range meshMetaInfoCtxKeys {
		val := pre.Value(key)
		if val == nil {
			continue
		}
		ctx = context.WithValue(ctx, key, val)
	}

	return ctx, true
}
