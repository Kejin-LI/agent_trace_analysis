package logs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"code.byted.org/aiops/apm_vendor_byted/vendor_tags"
	"code.byted.org/gopkg/ctxvalues"
	"code.byted.org/gopkg/env"
	"code.byted.org/gopkg/logs/utils"
)

/*
	bytedance log format:

	{Level} {Data} {Time} {Version}({NumHeaders}) {Location} {HostIP} {PSM} {LogID} {Cluster} {Stage} {RawLog} ...
	Warn 2017-11-28 14:55:22,562 v1(6) kite.go:94 10.8.44.18 byted.user.info - default canary message(97)=KITE: processing request error=RPC timeout: context deadline exceeded, remoteIP=10.8.59.153:58834
*/

var (
	spaceBytes   = []byte(" ")
	versionBytes = []byte("v1(7)")
	hostIPBytes  = []byte(env.HostIP())
	clusterBytes = []byte(env.Cluster())
	stageBytes   = []byte(env.Stage())
	unknownBytes = []byte("-")
)

// LocationCtxKey : if LocationCtxKey in Ctx, use it as location
type LocationCtxKey struct{}

// input the raw log to print and the kvs to setting
/**
Input:
	rawLog: 已经format过的log，即最终需要emit出来的函数
	kvs：contex中生成的和以KVs结尾的函数中的kvs列表
Output:
	string: 处理过的log
	[]interface{}：处理过的kvs返回
	bool: 是否有效，如果为false，默认该条日志不处理了
*/
type Processor func(rawLog string, kvs ...interface{}) (string, []interface{}, bool)

type LogMsgProcessor func(msg *LogMsg) (*LogMsg, bool)

func init() {
	if len(hostIPBytes) == 0 {
		hostIPBytes = unknownBytes
	}
	if len(clusterBytes) == 0 {
		clusterBytes = unknownBytes
	}
	if len(stageBytes) == 0 {
		stageBytes = unknownBytes
	}
}

const (
	LevelTrace = iota
	LevelDebug
	LevelInfo
	LevelNotice
	LevelWarn
	LevelError
	LevelFatal
)

const (
	_TracebackMaxFrames = 20
)

var (
	levelMap = []string{
		"Trace",
		"Debug",
		"Info",
		"Notice",
		"Warn",
		"Error",
		"Fatal",
	}

	levelBytes = [][]byte{
		[]byte("Trace"),
		[]byte("Debug"),
		[]byte("Info"),
		[]byte("Notice"),
		[]byte("Warn"),
		[]byte("Error"),
		[]byte("Fatal"),
	}
)

// LogMsg .
type LogMsg struct {
	// Msg is raw log. ex: logs.Info("Hello"), the Msg is "Hello".
	Msg   string
	Level int
	// the code location of the log, such as user.go:123.
	Location string
	Time     time.Time
	Ctx      context.Context
	Kvs      []interface{}
}

type KVLogMsg struct {
	msg     string
	level   int
	headers map[string]string
	kvs     map[string]string
}

// Logger .
type Logger struct {
	callDepth int // callDepth <= 0 will not print file number info.

	isRunning        int32
	level            int32
	buf              chan *LogMsg
	flush            chan *sync.WaitGroup
	providers        []LogProvider
	processors       []Processor
	logMsgProcessors []LogMsgProcessor
	enableDynamic    bool

	kvbuf       chan *KVLogMsg
	kvproviders []KVLogProvider
	PSM         string
	psmBytes    []byte

	providerPluses []LogProviderPlus

	wg   sync.WaitGroup
	stop chan struct{}

	useFullPath bool
	includeZone bool
	addEnv      bool

	disableAgentProvider bool
}

// NewLogger make default level is debug, default callDepth is 2, default provider is console.
func NewLogger(bufLen int) *Logger {
	defaultPSM := env.PSM()
	defaultPSMBytes := []byte(defaultPSM)

	if len(defaultPSMBytes) == 0 {
		defaultPSMBytes = unknownBytes
	}

	l := &Logger{
		level:         LevelDebug,
		buf:           make(chan *LogMsg, bufLen),
		kvbuf:         make(chan *KVLogMsg, bufLen),
		stop:          make(chan struct{}),
		flush:         make(chan *sync.WaitGroup),
		callDepth:     2,
		providers:     nil,
		PSM:           defaultPSM,
		psmBytes:      defaultPSMBytes,
		enableDynamic: false,
	}

	return l
}

// NewConsoleLogger 日志输出到屏幕，通常用于Debug模式
func NewConsoleLogger() *Logger {
	logger := NewLogger(1024)
	consoleProvider := NewConsoleProvider()
	consoleProvider.Init()
	logger.AddProvider(consoleProvider)
	return logger
}

// AddProvider .
func (l *Logger) AddProvider(p LogProvider) error {
	if kv, ok := p.(KVLogProvider); ok {
		l.kvproviders = append(l.kvproviders, kv)
	} else if tt, ok := p.(LogProviderPlus); ok {
		l.providerPluses = append(l.providerPluses, tt)
	} else {
		l.providers = append(l.providers, p)
	}
	return nil
}

func (l *Logger) AddProcessor(p Processor) error {
	l.processors = append(l.processors, p)
	return nil
}

func (l *Logger) AddLogMsgProcessor(p LogMsgProcessor) error {
	l.logMsgProcessors = append(l.logMsgProcessors, p)
	return nil
}

// SetLevel .
func (l *Logger) SetLevel(level int) {
	atomic.StoreInt32(&l.level, int32(level))
}

// GetLevel .
func (l *Logger) GetLevel() int {
	return int(atomic.LoadInt32(&l.level))
}

// EnableDynamicLogLevel.
func (l *Logger) EnableDynamicLogLevel() {
	l.enableDynamic = true
}

// SetPSM .
func (l *Logger) SetPSM(psm string) {
	l.PSM = psm
	l.psmBytes = []byte(psm)
}

// DisableCallDepth will not print file numbers.
func (l *Logger) DisableCallDepth() {
	l.callDepth = 0
}

// SetCallDepth .
func (l *Logger) SetCallDepth(depth int) {
	l.callDepth = depth
}

// SetZoneInfo set if include zone info in time string
func (l *Logger) SetZoneInfo(include bool) {
	l.includeZone = include
}

// DisableAgentProvider disables the agent provider
func (l *Logger) DisableAgentProvider() {
	l.disableAgentProvider = true
}

// SetDisplayEnvInfo sets if the logger will add a KV pair: ("_env", {env}) to all logs.
func (l *Logger) SetDisplayEnvInfo(add bool) {
	l.addEnv = add
}

// Deprecated:
// This option has issues and is under debugging now.
// EnableFastRuntimeCaller enables whether to use a faster runtime caller implementation to speed up log generation.
func (l *Logger) EnableFastRuntimeCaller(isEnabled bool) {
	//l.enableFastCaller = isEnabled
}

func (l *Logger) initProviders() {
	hasAgentP := false
	for _, p := range l.providers {
		if err := p.Init(); err != nil {
			fmt.Fprintln(os.Stderr, "provider Init() error:"+err.Error())
		}
	}

	for _, p := range l.providerPluses {
		if err := p.Init(); err != nil {
			fmt.Fprintln(os.Stderr, "providerPlus Init() error:"+err.Error())
		}
		// avoid recursive import
		if reflect.TypeOf(p).String() == "*provider.LogAgentProvider" {
			hasAgentP = true
		}
	}

	for _, p := range l.kvproviders {
		if err := p.Init(); err != nil {
			fmt.Fprintln(os.Stderr, "kvProvider Init() error:"+err.Error())
		}
		if _, ok := p.(*AgentProvider); ok {
			hasAgentP = true
		}
	}
	if env.InTCE() && !hasAgentP && !l.disableAgentProvider {
		p := NewAgentProvider()
		l.kvproviders = append(l.kvproviders, p)
		p.Init()
	}
}

func (l *Logger) encode2Text(log *LogMsg) string {
	enc := logEncoderPool.Get().(KVEncoder)
	defer func() {
		enc.Reset()
		logEncoderPool.Put(enc)
	}()

	l.prefixV1(log.Ctx, log.Level, log.Time, log.Location, enc)

	// KVs with security marks are handled before this function,
	// Here are all unmarkable KVs.
	enc.AppendKVs(log.Kvs...)

	if len(log.Msg) > 0 {
		rawBytes := []byte(log.Msg)
		if rawBytes[len(rawBytes)-1] == '\n' {
			rawBytes = rawBytes[:len(rawBytes)-1]
		}
		enc.Write(rawBytes)
	}
	enc.EndRecord()
	return enc.String()
}

// StartLogger .
func (l *Logger) StartLogger() {
	if !atomic.CompareAndSwapInt32(&l.isRunning, 0, 1) {
		return
	}
	if len(l.providers) == 0 && len(l.kvproviders) == 0 && len(l.providerPluses) == 0 {
		fmt.Fprintln(os.Stderr, "logger's providers is nil.")
		return
	}
	l.initProviders()

	l.wg.Add(1)
	var worker func()
	worker = func() {
		defer func() {
			if x := recover(); x != nil {
				_, _ = fmt.Fprintf(os.Stderr, "log SDK worker panic: %s\n", x)
				go worker()
				return
			}

			atomic.StoreInt32(&l.isRunning, 0)
			l.cleanBuf()
			for _, provider := range l.providers {
				provider.Destroy()
			}
			for _, kvprovider := range l.kvproviders {
				kvprovider.Destroy()
			}

			for _, p := range l.providerPluses {
				p.Destroy()
			}

			l.wg.Done()
		}()
		for {
			select {
			case log, ok := <-l.buf:

				if !ok {
					fmt.Fprintln(os.Stderr, "buf channel has been closed.")
					return
				}
				msg := l.encode2Text(log)
				for _, provider := range l.providers {
					provider.WriteMsg(msg, log.Level)
				}

				for _, p := range l.providerPluses {
					p.Write(log)
				}
			case msg, ok := <-l.kvbuf:
				if !ok {
					fmt.Fprintln(os.Stderr, "kvbuf channel has been closed.")
					return
				}
				for _, provider := range l.kvproviders {
					provider.WriteMsgKVs(msg.level, msg.msg, msg.headers, msg.kvs)
				}
			case wg := <-l.flush:
				l.cleanBuf()
				wg.Done()
			case <-l.stop:
				return
			}
		}
	}
	go worker()
}

func (l *Logger) cleanBuf() {
	bufEmpty := false
	for {
		if bufEmpty {
			break
		}
		select {
		case log := <-l.buf:
			msg := l.encode2Text(log)
			for _, provider := range l.providers {
				provider.WriteMsg(msg, log.Level)
			}
			for _, p := range l.providerPluses {
				p.Write(log)
			}
		case msg := <-l.kvbuf:
			for _, provider := range l.kvproviders {
				provider.WriteMsgKVs(msg.level, msg.msg, msg.headers, msg.kvs)
			}
		default:
			bufEmpty = true
		}
	}
	for _, provider := range l.providers {
		provider.Flush()
	}
	for _, kvprovider := range l.kvproviders {
		kvprovider.Flush()
	}
	for _, p := range l.providerPluses {
		p.Flush()
	}
}

// Stop .
func (l *Logger) Stop() {
	if !atomic.CompareAndSwapInt32(&l.isRunning, 1, 0) {
		return
	}
	close(l.stop)
	l.wg.Wait()
}

// CtxLevel .
func (l *Logger) CtxLevel(ctx context.Context) int {
	if !l.enableDynamic || ctx == nil {
		return int(atomic.LoadInt32(&l.level))
	}

	val := ctx.Value(DynamicLogLevelKey)
	if dynamicLevel, ok := val.(int); ok {
		return dynamicLevel
	}
	return int(atomic.LoadInt32(&l.level))
}

// Fatal .
func (l *Logger) Fatal(format string, v ...interface{}) {
	if LevelFatal < atomic.LoadInt32(&l.level) {
		return
	}
	if len(v) == 0 {
		l.fmtLog(context.Background(), LevelFatal, "", format)
		return
	}
	l.fmtLog(context.Background(), LevelFatal, "", utils.FmtSprintf(format, v...))
}

// CtxFatal .
func (l *Logger) CtxFatal(ctx context.Context, format string, v ...interface{}) {
	if LevelFatal < l.CtxLevel(ctx) {
		return
	}
	if len(v) == 0 {
		l.fmtLog(ctx, LevelFatal, "", format)
		return
	}
	l.fmtLog(ctx, LevelFatal, "", utils.FmtSprintf(format, v...))
}

// CtxFatalKVs .
func (l *Logger) CtxFatalKVs(ctx context.Context, kvs ...interface{}) {
	if LevelFatal < l.CtxLevel(ctx) {
		return
	}
	l.fmtLog(ctx, LevelFatal, "", "", kvs...)
}

// Error .
func (l *Logger) Error(format string, v ...interface{}) {
	if LevelError < atomic.LoadInt32(&l.level) {
		return
	}
	if len(v) == 0 {
		l.fmtLog(context.Background(), LevelError, "", format)
		return
	}
	l.fmtLog(context.Background(), LevelError, "", utils.FmtSprintf(format, v...))
}

// CtxError .
func (l *Logger) CtxError(ctx context.Context, format string, v ...interface{}) {
	if LevelError < l.CtxLevel(ctx) {
		return
	}
	if len(v) == 0 {
		l.fmtLog(ctx, LevelError, "", format)
		return
	}
	l.fmtLog(ctx, LevelError, "", utils.FmtSprintf(format, v...))
}

// CtxErrorKVs .
func (l *Logger) CtxErrorKVs(ctx context.Context, kvs ...interface{}) {
	if LevelError < l.CtxLevel(ctx) {
		return
	}
	l.fmtLog(ctx, LevelError, "", "", kvs...)
}

// Warn .
func (l *Logger) Warn(format string, v ...interface{}) {
	if LevelWarn < atomic.LoadInt32(&l.level) {
		return
	}
	if len(v) == 0 {
		l.fmtLog(context.Background(), LevelWarn, "", format)
		return
	}
	l.fmtLog(context.Background(), LevelWarn, "", utils.FmtSprintf(format, v...))
}

// CtxWarn .
func (l *Logger) CtxWarn(ctx context.Context, format string, v ...interface{}) {
	if LevelWarn < l.CtxLevel(ctx) {
		return
	}
	if len(v) == 0 {
		l.fmtLog(ctx, LevelWarn, "", format)
		return
	}
	l.fmtLog(ctx, LevelWarn, "", utils.FmtSprintf(format, v...))
}

// CtxWarnKVs .
func (l *Logger) CtxWarnKVs(ctx context.Context, kvs ...interface{}) {
	if LevelWarn < l.CtxLevel(ctx) {
		return
	}
	l.fmtLog(ctx, LevelWarn, "", "", kvs...)
}

// Notice .
func (l *Logger) Notice(format string, v ...interface{}) {
	if LevelNotice < atomic.LoadInt32(&l.level) {
		return
	}
	if len(v) == 0 {
		l.fmtLog(context.Background(), LevelNotice, "", format)
		return
	}
	l.fmtLog(context.Background(), LevelNotice, "", utils.FmtSprintf(format, v...))
}

// CtxNotice .
func (l *Logger) CtxNotice(ctx context.Context, format string, v ...interface{}) {
	if LevelNotice < l.CtxLevel(ctx) {
		return
	}
	if len(v) == 0 {
		l.fmtLog(ctx, LevelNotice, "", format)
		return
	}
	l.fmtLog(ctx, LevelNotice, "", utils.FmtSprintf(format, v...))
}

// CtxNoticeKVs .
func (l *Logger) CtxNoticeKVs(ctx context.Context, kvs ...interface{}) {
	if LevelNotice < l.CtxLevel(ctx) {
		return
	}
	l.fmtLog(ctx, LevelNotice, "", "", kvs...)
}

// Info .
func (l *Logger) Info(format string, v ...interface{}) {
	if LevelInfo < atomic.LoadInt32(&l.level) {
		return
	}
	if len(v) == 0 {
		l.fmtLog(context.Background(), LevelInfo, "", format)
		return
	}
	l.fmtLog(context.Background(), LevelInfo, "", utils.FmtSprintf(format, v...))
}

// CtxInfo .
func (l *Logger) CtxInfo(ctx context.Context, format string, v ...interface{}) {
	if LevelInfo < l.CtxLevel(ctx) {
		return
	}
	if len(v) == 0 {
		l.fmtLog(ctx, LevelInfo, "", format)
		return
	}
	l.fmtLog(ctx, LevelInfo, "", utils.FmtSprintf(format, v...))
}

// CtxInfoKVs .
func (l *Logger) CtxInfoKVs(ctx context.Context, kvs ...interface{}) {
	if LevelInfo < l.CtxLevel(ctx) {
		return
	}
	l.fmtLog(ctx, LevelInfo, "", "", kvs...)
}

// Debug .
func (l *Logger) Debug(format string, v ...interface{}) {
	if LevelDebug < atomic.LoadInt32(&l.level) {
		return
	}
	if len(v) == 0 {
		l.fmtLog(context.Background(), LevelDebug, "", format)
		return
	}
	l.fmtLog(context.Background(), LevelDebug, "", utils.FmtSprintf(format, v...))
}

// CtxDebug .
func (l *Logger) CtxDebug(ctx context.Context, format string, v ...interface{}) {
	if LevelDebug < l.CtxLevel(ctx) {
		return
	}
	if len(v) == 0 {
		l.fmtLog(ctx, LevelDebug, "", format)
		return
	}
	l.fmtLog(ctx, LevelDebug, "", utils.FmtSprintf(format, v...))
}

// CtxDebugKVs .
func (l *Logger) CtxDebugKVs(ctx context.Context, kvs ...interface{}) {
	if LevelDebug < l.CtxLevel(ctx) {
		return
	}
	l.fmtLog(ctx, LevelDebug, "", "", kvs...)
}

// Trace .
func (l *Logger) Trace(format string, v ...interface{}) {
	if LevelTrace < atomic.LoadInt32(&l.level) {
		return
	}
	if len(v) == 0 {
		l.fmtLog(context.Background(), LevelTrace, "", format)
		return
	}
	l.fmtLog(context.Background(), LevelTrace, "", utils.FmtSprintf(format, v...))
}

// CtxTrace .
func (l *Logger) CtxTrace(ctx context.Context, format string, v ...interface{}) {
	if LevelTrace < l.CtxLevel(ctx) {
		return
	}
	if len(v) == 0 {
		l.fmtLog(ctx, LevelTrace, "", format)
		return
	}
	l.fmtLog(ctx, LevelTrace, "", utils.FmtSprintf(format, v...))
}

// CtxTraceKVs .
func (l *Logger) CtxTraceKVs(ctx context.Context, kvs ...interface{}) {
	if LevelTrace < l.CtxLevel(ctx) {
		return
	}
	l.fmtLog(ctx, LevelTrace, "", "", kvs...)
}

// Warn 2017-11-28 14:55:22,562 v1(7) kite.go:94 10.8.44.18 byted.user.info - default canary 0 message(97)=KITE: processing request error=RPC timeout: context deadline exceeded, remoteIP=10.8.59.153:58834
// {Level} {Date} {Time} {Version}({NumHeaders}) {Location} {HostIP} {PSM} {LogID} {Cluster} {Stage} {SpanID} {KV1} {KV2} ...
func (l *Logger) prefixV1(ctx context.Context, level int, time time.Time, fileLocation string, writer io.Writer) {
	writer.Write(levelBytes[level])
	writer.Write(spaceBytes)
	dt := timeDate(time, l.includeZone)
	writer.Write(dt[:])
	writer.Write(spaceBytes)
	writer.Write(versionBytes)
	writer.Write(spaceBytes)
	io.WriteString(writer, fileLocation)
	writer.Write(spaceBytes)
	writer.Write(hostIPBytes)
	writer.Write(spaceBytes)
	writer.Write(l.psmBytes)
	writer.Write(spaceBytes)
	io.WriteString(writer, ctxvalues.LogIDDefault(ctx))
	writer.Write(spaceBytes)
	writer.Write(clusterBytes)
	writer.Write(spaceBytes)
	writer.Write(stageBytes)
	writer.Write(spaceBytes)
	writer.Write(strconv.AppendUint(nil, utils.SpanIDFromContext(ctx), 16))
	writer.Write(spaceBytes)
}

func (l *Logger) fmtLog(ctx context.Context, level int, fileLocation string, rawLog string, kvs ...interface{}) {
	if level < l.CtxLevel(ctx) {
		return
	}
	if atomic.LoadInt32(&l.isRunning) == 0 {
		return
	}

	if err := doMetrics(ctx, level, l.PSM); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to do metrics: %v\n", err)
	}

	kvList := GetAllKVs(ctx)
	if kvList != nil {
		kvList = append(kvList, kvs...)
	} else {
		kvList = kvs
	}

	if l.addEnv {
		kvList = append(kvList, vendor_tags.TagEnv, env.Env(), vendor_tags.TagEnvType, getHostEnv())
	}

	normalKVsNum := 0
	allKVs, markableKVs := make([]interface{}, 0, len(kvList)), make([]interface{}, 0, len(kvList))

	if enableSecMark {
		for i := 0; i+1 < len(kvList); i += 2 {
			k := kvList[i]
			v := kvList[i+1]
			markableKVs = append(markableKVs, utils.Value2Str(k), utils.Value2Str(v))
		}
		allKVs = append(allKVs, markableKVs...)
	} else {
		for _, e := range kvList {
			allKVs = append(allKVs, utils.Value2Str(e))
		}
		normalKVsNum = len(allKVs)
	}

	for _, processor := range l.processors {
		tmpLog, tmpKvs, validated := processor(rawLog, allKVs...)
		if !validated {
			return
		}
		rawLog = tmpLog
		allKVs = tmpKvs
	}

	// If Security Mark is enabled and there are kv pairs, we directly process these pairs and append them to msg.
	// Then we clear the kv list.
	if enableSecMark {
		var sb strings.Builder
		sb.Grow(len(rawLog) + 10*(len(kvs)-normalKVsNum))
		sb.WriteString(rawLog)
		if len(rawLog) != 0 {
			sb.WriteString(" ")
		}

		if normalKVsNum < len(allKVs) {
			for i := normalKVsNum; i+1 < len(allKVs); i += 2 {
				k := allKVs[i]
				v := allKVs[i+1]
				sb.WriteString("{{")
				sb.WriteString(utils.Value2Str(k))
				sb.WriteString("=")
				sb.WriteString(utils.Value2Str(v))
				sb.WriteString("}} ")
			}
			allKVs = allKVs[:normalKVsNum]
		}
		rawLog = sb.String()

		if len(rawLog) > 0 && rawLog[len(rawLog)-1] == ' ' {
			rawLog = rawLog[:len(rawLog)-1]
		}
		allKVs = append(allKVs, "current_version", currVersion)
	}

	if ctx != nil {
		if stackInfoValue := ctx.Value(stackInfoCtxKey); stackInfoValue != nil {
			if stackInfo, ok := stackInfoValue.(StackInfo); ok {
				switch stackInfo {
				case CurrGoroutine:
					buffer := bytes.NewBuffer(make([]byte, 0, 1024))
					pcs := make([]uintptr, _TracebackMaxFrames)
					_ = runtime.Callers(1, pcs)
					frames := runtime.CallersFrames(pcs)
					for {
						f, more := frames.Next()
						_, err := fmt.Fprintf(buffer, "#\t0x%x\t%s\t%s:%d\n", f.PC, f.Function, f.File, f.Line)
						if err != nil || !more {
							break
						}
					}
					allKVs = append(allKVs, "stack", buffer.String())
				case AllGoroutines:
					buffer := bytes.NewBuffer(make([]byte, 0, 4096))
					pprof.Lookup("goroutine").WriteTo(buffer, 1)

					allKVs = append(allKVs, "stack", buffer.String())
				default:
				}
			}
		}
	}

	if len(l.providers) > 0 || len(l.providerPluses) > 0 {
		l.fmtForProvider(ctx, level, fileLocation, rawLog, allKVs...)
	}

	if len(l.kvproviders) > 0 {
		l.fmtForKVProvider(ctx, level, fileLocation, rawLog, allKVs...)
	}
}

func (l *Logger) fmtForProvider(ctx context.Context, level int, fileLocation string, rawLog string, kvs ...interface{}) {
	if ctx != nil && ctx.Value(LocationCtxKey{}) != nil {
		fileLocation = ctx.Value(LocationCtxKey{}).(string)
	}
	if len(fileLocation) == 0 && l.callDepth > 0 {
		fileLocation = l.location(l.callDepth+2, l.useFullPath || enableSecMark)
	}
	// 作用只是用context再包装一下logID
	// 考虑到一个历史问题:有挺多的gin的用户把gin.Context当成context.Context作为入参传入.
	// 一般认为context.Context是线程安全的,但gin.Context不是线程安全的,导致后续logger异步从context取logID可能会造成并发读写map的panic.
	// 基础库为保安全,故做此兼容.
	// TODO: gin context的用法规范之后删除这行代码.
	stdCtx := context.Background()
	stdCtx = ctxvalues.SetLogID(stdCtx, ctxvalues.LogIDDefault(ctx))
	stdCtx = context.WithValue(stdCtx, "K_SPANID", utils.SpanIDFromContext(ctx))
	log := &LogMsg{
		Ctx:      stdCtx,
		Level:    level,
		Location: fileLocation,
		Msg:      rawLog,
		Time:     time.Now(),
		Kvs:      kvs,
	}

	for _, processor := range l.logMsgProcessors {
		temp, validated := processor(log)
		log = temp
		if !validated {
			return
		}
	}

	select {
	case l.buf <- log:
	default:
	}
}

var logEncoderPool = sync.Pool{
	New: func() interface{} {
		return NewTTLogKVEncoder()
	},
}

// Flush 将buf中的日志数据一次性写入到各个provider中，期间新的写入到buf的日志会被丢失
func (l *Logger) Flush() {
	if atomic.LoadInt32(&l.isRunning) == 0 {
		return
	}
	wg := &sync.WaitGroup{}
	wg.Add(1)
	select {
	case l.flush <- wg:
		wg.Wait()
	case <-time.After(time.Second):
		return // busy ?
	}
}

func (l *Logger) UseFullPath(useFullPath bool) {
	l.useFullPath = useFullPath
}

func (l *Logger) fmtForKVProvider(ctx context.Context, level int, fileLocation string, rawLog string, kvs ...interface{}) {
	headers := make(map[string]string, 9)
	headers["level"] = string(levelBytes[level])
	headers["timestamp"] = strconv.Itoa(int(time.Now().UnixNano() / int64(time.Millisecond)))
	if ctx != nil && ctx.Value(LocationCtxKey{}) != nil {
		headers["location"] = ctx.Value(LocationCtxKey{}).(string)
	} else if fileLocation != "" {
		headers["location"] = fileLocation
	} else if l.callDepth > 0 {
		headers["location"] = l.location(l.callDepth+2, l.useFullPath || enableSecMark)
	}
	headers["host"] = env.HostIP()
	headers["psm"] = l.PSM
	headers["cluster"] = env.Cluster()
	headers["logid"] = string(ctxvalues.LogIDDefault(ctx))
	headers["stage"] = env.Stage()
	headers["pod_name"] = env.PodName()
	headers["span_id"] = strconv.FormatUint(utils.SpanIDFromContext(ctx), 16)

	kvMap := make(map[string]string, len(kvs))
	for i := 0; i+1 < len(kvs); i += 2 {
		k := kvs[i]
		v := kvs[i+1]
		kstr := string(utils.Value2Str(k))
		vstr := string(utils.Value2Str(v))
		kvMap[kstr] = vstr
	}

	msg := &KVLogMsg{
		msg:     rawLog,
		level:   level,
		headers: headers,
		kvs:     kvMap,
	}
	select {
	case l.kvbuf <- msg:
	default:
		// TODO(zhangyuanjia): do metrics?
	}
}

var (
	gopath = path.Join(os.Getenv("GOPATH"), "src") + "/"
)

func (l *Logger) location(deep int, fullPath bool) string {
	var file string
	var line int
	var ok bool

	_, file, line, ok = runtime.Caller(deep)

	if !ok {
		file = "???"
		line = 0
	}

	if fullPath {
		if strings.HasPrefix(file, gopath) {
			file = file[len(gopath):]
		}
	} else {
		file = filepath.Base(file)
	}
	return file + ":" + strconv.Itoa(line)
}

func getHostEnv() string {
	if hostEnv := os.Getenv("TCE_HOST_ENV"); hostEnv != "" {
		return hostEnv
	}
	return "-"
}
