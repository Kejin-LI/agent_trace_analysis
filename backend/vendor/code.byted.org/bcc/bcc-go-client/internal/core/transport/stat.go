package transport

import (
	"errors"
	"sync/atomic"
	"time"

	"code.byted.org/bcc/bcc-go-client/internal/core/common"
	"code.byted.org/bcc/bcc-go-client/internal/util"
	"code.byted.org/bcc/bcc-go-client/logger"
)

type ConnectionStatus int32

const (
	ConnInit           ConnectionStatus = 1 //连接断开时的状态
	ConnEstablished    ConnectionStatus = 2 //成功建立连接时的状态
	ConnFinishRegister ConnectionStatus = 3 //连接完成注册的状态
)

// 负责注册包与心跳包的状态记录，用于判断连接是否正常，是否需要重连
type connState struct {
	status              ConnectionStatus
	retryCount          int //stream client错误重试次数
	connectCount        int //连接次数（1~n）
	registerFinishCount int
	registerError       common.RegisterRspCode
	lastConnectTime     time.Time
	lastRegisterTime    time.Time
	lastPingTime        time.Time //用于计算stream活跃时间
	pingInterval        int64
}

func newStreamStat() *connState {
	s := &connState{status: ConnInit}
	s.OnPing()
	return s
}

func (s *connState) ResetStreamConn() {
	s.lastRegisterTime = time.Time{}
	atomic.StoreInt32((*int32)(&s.status), int32(ConnInit))
}

func (s *connState) IsInitStatus() bool {
	return atomic.LoadInt32((*int32)(&s.status)) == int32(ConnInit)
}

func (s *connState) IsFinishRegistedStatus() bool {
	return atomic.LoadInt32((*int32)(&s.status)) == int32(ConnFinishRegister)
}

func (s *connState) OnPing() {
	s.lastPingTime = time.Now()
}
func (s *connState) OnSendRegister() {
	s.lastRegisterTime = time.Now()
}
func (s *connState) PingTimeout() bool {
	return time.Since(s.lastPingTime) > 10*time.Duration(s.pingInterval)*time.Second
}
func (s *connState) NeedPing(idx int64) bool {
	logger.Trace("idx:%v pingInterval:%v", idx, s.pingInterval)
	return idx%(s.pingInterval) == 0
}
func (s *connState) RegisSuccess(msg *common.RegisterMsg) {
	atomic.StoreInt32((*int32)(&s.status), int32(ConnFinishRegister))
	s.registerFinishCount += 1
	s.registerError = common.REGISTERCODE_OK
	s.lastRegisterTime = time.Now()
	s.lastPingTime = time.Now()
	s.lastConnectTime = time.Now()
	s.retryCount = 0
	if msg.PingInterval > 0 {
		s.pingInterval = msg.PingInterval
	}
}
func (s *connState) ConnectSuccess() {
	util.EmitConnectSLA("success")
	s.lastConnectTime = time.Now()
	atomic.StoreInt32((*int32)(&s.status), int32(ConnEstablished))
	s.retryCount = 0
	s.connectCount += 1
}
func (s *connState) ConnectFail() {
	util.EmitConnectSLA("fail")
	s.lastConnectTime = time.Now()
	s.retryCount += 1
}
func (s *connState) RegisterTimeout() bool {
	return !s.lastRegisterTime.IsZero() && time.Since(s.lastRegisterTime) > time.Second*20
}

// 刚启动，从没成功过，直接退出
func (s *connState) RegisterFatal() error {
	if s.registerError != common.REGISTERCODE_OK && s.registerFinishCount == 0 {
		return errors.New("register fail:" + s.registerError.String())
	}
	return nil
}

func (s *connState) CreateStreamMustSlow() bool {
	// 曾经重连成功，突然断开并出现错误，等待30秒
	if s.registerError != common.REGISTERCODE_OK && s.registerFinishCount > 0 && (time.Since(s.lastRegisterTime) < 30*time.Second) {
		return true
	}

	retryError := map[common.RegisterRspCode]bool{
		common.REGISTERCODE_STRESS: true,
		common.REGISTERCODE_RETRY:  true,
		common.REGISTERCODE_AUTH:   true,
		common.REGISTERCODE_FORBID: true,
	}
	//连接成功，但是出现注册错误，每五秒重连
	if retryError[s.registerError] && (time.Since(s.lastRegisterTime) < 5*time.Second) {
		return true
	}

	// 连接成功过，断开了，等待两秒进行重连
	if s.connectCount > 0 && (time.Since(s.lastConnectTime) < 2*time.Second) {
		return true
	}

	// 一直连接失败，根据retry count进行退避，避免高频重连
	retryTime := time.Duration(s.retryCount) * 200 * time.Millisecond
	if retryTime > 1*time.Minute {
		retryTime = 1 * time.Minute
	}
	if s.retryCount > 0 && time.Since(s.lastConnectTime) < retryTime {
		return true
	}
	return false
}
func (s connState) RegisError(err common.RegisterRspCode) {
	s.registerError = err
}
