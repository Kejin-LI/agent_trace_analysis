package transport

import (
	"errors"
	"time"

	"code.byted.org/bcc/bcc-go-client/coreclient/model"
	"code.byted.org/bcc/bcc-go-client/internal/core/common"
	"code.byted.org/bcc/bcc-go-client/internal/util"
	"code.byted.org/bcc/bcc-go-client/logger"
	"code.byted.org/bcc/tools"
)

var _ common.Connection = (*Client)(nil)

// 负责创建grpc 连接，与eventMgr进行交互，
type Client struct {
	msgChan   chan interface{}
	taskChan  chan interface{}
	closeChan chan int
	stream    *StreamClient
	// 实为 "code.byted.org/bcc/bcc-go-client/internal/core/eventmgr".EventMgr
	msgHandler common.MsgHandler
	state      *connState
	regisInfo  common.RegisterInfo
	streamOpts []StreamClientOption
}

func newTransportClient(msgHandler common.MsgHandler, clientName string, opt *model.SdkOptions) *Client {
	regisInfo := common.RegisterInfo{
		SdkPath:     opt.SDKPath,
		SdkLang:     opt.SDKLang,
		ClientName:  clientName,
		Tags:        opt.Tags,
		DisableAuth: opt.DisableAuth,
	}
	opts := getStreamOpts(opt)
	c := &Client{
		msgChan:    make(chan interface{}, 500), // 监听目录的情况下，server会发送比较多消息
		taskChan:   make(chan interface{}, 200),
		msgHandler: msgHandler,
		state:      newStreamStat(),
		closeChan:  make(chan int),
		regisInfo:  regisInfo,
		streamOpts: opts,
	}
	c.createStream()
	go c.eventLoop()
	return c
}

func getStreamOpts(opt *model.SdkOptions) []StreamClientOption {
	opts := make([]StreamClientOption, 0, 0)
	if opt.RemoteAddr != "" {
		opts = append(opts, StreamWithAddr(opt.RemoteAddr))
	}
	if opt.RemotePSM != "" {
		opts = append(opts, StreamWithPSM(opt.RemotePSM))
	}
	if opt.RemoteCluster != "" {
		opts = append(opts, StreamWithCluster(opt.RemoteCluster))
	}
	if opt.RemoteIDC != "" {
		opts = append(opts, StreamWithIDC(opt.RemoteIDC))
	}
	if opt.StreamTimeout.Nanoseconds() > 0 {
		opts = append(opts, StreamWithTimeout(opt.StreamTimeout))
	}
	return opts
}

func (c *Client) NeedRegister() bool {
	return !c.state.IsFinishRegistedStatus()
}

func (c *Client) HasHandshake() bool {
	return false
}

func (c *Client) needCreateStream() bool {
	return c.stream == nil || c.state.IsInitStatus()
}

func (c *Client) createStream() {
	logger.Debug("create stream")

	//调整重连时间间隔
	if c.state.CreateStreamMustSlow() {
		return
	}

	//曾经收到注册回包，但是存在严重错误
	//todo 直接退出，抛出err给业务侧
	if err := c.state.RegisterFatal(); err != nil {
		logger.Error("client init exit registerLastError=%v", err)
		c.Close()
		return
	}

	stream := NewStreamClient(c, c.streamOpts...)
	err := stream.Run()
	if err != nil {
		logger.Error("create stream failed, err=%v", err)
		c.state.ConnectFail()
		return
	}
	c.state.ConnectSuccess()
	logger.Debug("connect success")
	c.stream = stream
	c.sendRegister()
}

// 发送eventmgr的消息到server，处理server的消息发送到eventmgr
// 定时处理心跳包，重连
func (c *Client) eventLoop() {
	//ticker兼容1.13, 不使用reset
	ticker := time.NewTicker(time.Millisecond * 50)
	tickerInterval := 50 * time.Millisecond
	resetTicker := func(interval time.Duration) {
		if interval == tickerInterval {
			return
		}
		ticker.Stop()
		ticker = time.NewTicker(interval)
		tickerInterval = interval
	}
	secTicker := time.NewTicker(1 * time.Second)
	running := true
	idx := int64(0)
	pingIdx := int64(0)
	for running {
		select {
		case msg := <-c.msgChan:
			c.dispatchMsg(msg)

		case task := <-c.taskChan:
			c.dispatchTask(task)
		case <-ticker.C:
			if c.needCreateStream() {
				c.createStream()
				resetTicker(50 * time.Millisecond)
			} else {
				c.onTimer(TimerMsg{idx: idx})
				idx += 1
				resetTicker(1 * time.Second)
			}
		case <-secTicker.C:
			pingIdx++
			c.dispatchTask(&common.StreamPingTask{Idx: pingIdx})

		case <-c.closeChan:
			running = false
		}
	}
	logger.Notice("transport eventLoop exit")
}

// 定时处理心跳包,检测连接状态
func (c *Client) onTimer(msg TimerMsg) {
	c.checkRegister()
}

// 处理来自Stream的消息。
func (c *Client) dispatchMsg(m interface{}) {
	//会出现在mgr或者Client已经下达了CloseStreamTask，此时stream还往Client推送消息
	//task的处理和dispatchMsg是跑在一个协程的，所以不会出现c.stream判断不为nil之后，马上被设置在handleCloseStreamTask()设置为nil.
	if c.stream == nil {
		return
	}
	logger.Debug("client handle msg %v", tools.GetStructName(m))

	switch msg := m.(type) {
	case *common.PingMsg:
		c.onPing()
		//c.msgHandler.AddMsg(msg) 不需要回调给上层

	case *common.RegisterMsg:
		c.onRegister(msg)

	case *common.UpdateKeyMsg, *common.UpdatePathMsg, *common.FinishPathMsg:
		c.msgHandler.AddMsg(msg)

	case *StreamCloseMsg:
		logger.Debug("streamClose :%v", msg.stream == nil)
		c.closeStream(msg.Err, msg.Normal)

	default:
		logger.Warn("unknown msg:%s", tools.GetStructName(m))
	}
}

func (c *Client) dispatchTask(t interface{}) {
	if t == nil || c.NeedRegister() {
		return
	}

	if _, ok := t.(*common.StreamPingTask); !ok {
		logger.Debug("client handle task %v", tools.GetStructName(t))

		defer logger.Debug("client handle task %v end", tools.GetStructName(t))
	}

	switch task := t.(type) {
	case *common.StreamPingTask:
		{
			if c.state.NeedPing(task.Idx) {
				logger.Trace("send ping")
				c.stream.AddTask(task)
			}
			if c.state.PingTimeout() {
				logger.Debug("ping timeout")
				util.EmitPingSLA("fail")
				c.closeStream(errors.New("ping_timeout"), false)
			}
		}

	case *common.StreamReportKeyTask, *common.StreamReportPathTask:
		c.stream.AddTask(task)

	case *common.StreamWatchKeyTask, *common.StreamWatchPathTask:
		c.stream.AddTask(task)

	case *common.StreamCancelKeyTask, *common.StreamCancelPathTask:
		c.stream.AddTask(task)

	default:
		logger.Warn("unknown task:%s", tools.GetStructName(t))
	}
}

func (c *Client) closeStream(err error, normal bool) {
	logger.Info("grpc conn closeStream err. normal[%v] err:%v", normal, err)
	logger.Debug("handle close stream task")
	if c.stream != nil {
		//确保在stream.close()之前，c.stream就已经置为nil了。避免了循环close
		stream := c.stream
		c.stream = nil
		stream.Close(err)

		dur := time.Since(c.state.lastConnectTime)
		if dur <= 3*time.Second {
			//todo 打点断连
		} else if dur <= 30*time.Second {
		} else {
		}
	}
	if err != nil {
		util.EmitClose(c.state.connectCount, normal, err.Error())
	}

	c.state.ResetStreamConn()
}

// 不能阻塞
func (c *Client) AddMsg(msg interface{}) {
	c.msgChan <- msg
}

func (c *Client) AddTask(task interface{}) {
	c.taskChan <- task
}

func (c *Client) sendRegister() {
	//等待transport建立stream
	if c.needCreateStream() {
		return
	}

	//todo 优化regis 结构体
	r := c.regisInfo
	r.ConnectCount = int64(c.state.connectCount)
	registerTask := common.RegisterTask{Info: r}
	c.stream.AddTask(&registerTask)
	c.state.OnSendRegister()
}

// 定时执行，确保连接重建时发送注册包
func (c *Client) checkRegister() {

	if !c.NeedRegister() {
		return
	}

	//连接创建，还没回注册包
	if c.state.RegisterTimeout() {
		logger.Error("client init register timeout")
		util.EmitRegisterSLA("fail")
		c.closeStream(errors.New("register_timeout"), false)
		return
	}
}

func (c *Client) onRegister(msg *common.RegisterMsg) {
	util.EmitRegisterSLA("success")

	if msg.ErrorMsg != "" {
		logger.Error("onRegister errorMsg=%v", msg.ErrorMsg)
	}
	if msg.WarnMsg != "" {
		logger.Warn("onRegister warnMsg=%v", msg.WarnMsg)
	}
	if c.needCreateStream() {
		logger.Error("onRegister but close")
		//todo 优化路径
		c.closeStream(errors.New("register_timeout"), true)
		return
	}
	if msg.Code != common.REGISTERCODE_OK {
		logger.Warn("onRegister fail code=%v msg=%v", msg.Code, msg.ErrorMsg)
		if msg.Code != common.REGISTERCODE_RETRY {
			c.state.RegisError(msg.Code)
		}
		c.closeStream(errors.New("register_fail"), false)
		return
	}

	c.state.RegisSuccess(msg)

	//抛给eventmgr进行处理
	c.msgHandler.AddMsg(msg)
	util.EmitRegister(c.regisInfo.ClientName, int(c.regisInfo.ConnectCount), c.regisInfo.SdkLang, c.regisInfo.SdkPath)
	logger.Notice("transport receive register success")
	logger.Debug("regster info:%+v", msg)
}

func (c *Client) onPing() {
	util.EmitPingSLA("success")
	c.state.OnPing()
}

// 关闭连接模块
func (c *Client) Close() {
	if c.closeChan != nil {
		close(c.closeChan)
	}
	logger.Debug("close stream")

	//底层的stream可能已经断开了，此时不应该还close
	if c.stream != nil {
		c.stream.Close(nil)
	}
	//todo 通知mgr
}
