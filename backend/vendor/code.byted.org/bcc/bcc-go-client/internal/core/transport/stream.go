package transport

import (
	"context"
	"fmt"
	"net"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"code.byted.org/bcc/bcc-go-client/internal/core/common"
	"code.byted.org/bcc/bcc-go-client/internal/core/pb"
	"code.byted.org/bcc/bcc-go-client/internal/util"
	"code.byted.org/bcc/bcc-go-client/logger"
	"code.byted.org/bcc/tools"
	"code.byted.org/bcc/tools/utime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// 停止控制相关
type runningCtl struct {
	waitWg  sync.WaitGroup
	begin   time.Time
	isClose int32
}

// 数据收发
type StreamClient struct {
	sendCh    chan interface{}
	conn      *grpc.ClientConn
	stream    pb.BCCService_PipeClient
	transport common.MsgHandler
	ctx       context.Context
	ctl       runningCtl
	opInfo    *optionInfo
	closeCh   chan struct{}
}

func NewStreamClient(transport common.MsgHandler, opts ...StreamClientOption) *StreamClient {

	const (
		defaultTimeout = 5 * time.Second
		defaultPSM     = "bytedance.bcc.push_server"
		defaultCluster = "default"
	)

	s := &StreamClient{
		closeCh: make(chan struct{}),
		sendCh:  make(chan interface{}, 1024),
		opInfo: &optionInfo{
			psm:     defaultPSM,
			idc:     "",
			cluster: defaultCluster,
			timeout: defaultTimeout,
		},
		transport: transport,
		ctx:       util.CreateCtx(),
		ctl: runningCtl{
			isClose: 0,
		},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *StreamClient) Run() error {
	if os.Getenv("BCC_DISABLE_GRPC") == "true" {
		return fmt.Errorf("disable grpc because environment variables[BCC_DISABLE_GRPC] are set manually")
	}
	//ctx, _ := context.WithTimeout(s.ctx, s.opInfo.timeout)
	logger.CtxInfo(s.ctx, "StreamClient.grpc connection start")
	err := s.connectServer(s.ctx)
	if err != nil {
		return err
	}
	s.ctl.begin = time.Now()
	s.ctl.waitWg.Add(1)
	go s.sendWorker()
	s.ctl.waitWg.Add(1)
	go s.recvWorker()
	return nil
}

// todo error
// todo 打点
func (s *StreamClient) connectServer(ctx context.Context) error {

	conn, err := s.getGrpcConnection(ctx)
	if err != nil {
		return err
	}
	util.EmitStreamAction("connect", "success", 1)
	header := metadata.New(map[string]string{"K_LOGID": tools.GetLogID(ctx)})
	ctx = metadata.NewOutgoingContext(ctx, header)
	grpcClient := pb.NewBCCServiceClient(conn)
	stream, err := grpcClient.Pipe(ctx)
	if err != nil {
		logger.CtxWarn(ctx, "pipe connect fail err:%v", err)
		util.EmitStreamAction("pipe", "fail", 1)
		return err
	}
	util.EmitStreamAction("pipe", "success", 1)
	logger.CtxInfo(ctx, "pipe connect success, addr:%v psm:%v cluster:%v idc:%v", s.opInfo.addr, s.opInfo.psm, s.opInfo.cluster, s.opInfo.idc)
	s.conn = conn
	s.stream = stream
	return nil
}

func (s *StreamClient) getGrpcConnection(ctx context.Context) (*grpc.ClientConn, error) {

	err := s.lookup()
	if err != nil {
		return nil, err
	}

	logger.CtxInfo(ctx, "grpc start connect addr:%v psm:%v idc:%v cluster:%v timeout:%v", s.opInfo.addr, s.opInfo.psm, s.opInfo.idc, s.opInfo.cluster, s.opInfo.timeout)
	dialOption := grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	})
	ctx, cancel := context.WithTimeout(ctx, s.opInfo.timeout)
	defer cancel()
	send8MB := grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(20*1024*1024), grpc.MaxCallSendMsgSize(20*1024*1024))
	conn, err := grpc.DialContext(ctx, s.opInfo.addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock(), dialOption, send8MB)
	if err != nil {
		if conn != nil {
			//连接超时
			conn.Close()
		}
		err := fmt.Errorf("grpc dial error:%v", err)
		logger.CtxError(ctx, "getGrpcConnection err:%v", err)
		util.EmitStreamAction("connect", "fail", 1)
		return nil, err
	}
	return conn, nil
}

func (s *StreamClient) lookup() error {
	if s.opInfo.addr == "" {
		addr, err := ServerSelector{}.FindBestServer(s.opInfo)
		if err != nil {
			return err
		}
		s.opInfo.addr = addr
	}
	return nil
}

func (s *StreamClient) AddTask(task interface{}) {
	s.sendCh <- task
}

func (s *StreamClient) sendWorker() {
	defer s.ctl.waitWg.Done()
	for {
		select {
		case task := <-s.sendCh:
			logger.Debug("sendWorker  task type:%v task:%v", reflect.TypeOf(task), task)
			if task == nil {
				return
			}
			reqMsg := s.getStreamRequest(task)
			if reqMsg == nil {
				continue
			}
			if err := s.stream.Send(reqMsg); err != nil {
				if atomic.LoadInt32(&s.ctl.isClose) == 1 { //由于其他原因关闭导致的Send失败
					logger.Debug("stream close sendWorker exit")
					return
				}
				util.EmitStreamAction("send", "fail", 1)
				logger.CtxError(s.ctx, "sendWorker err:%v", err)
				s.transport.AddMsg(&StreamCloseMsg{Ctx: s.ctx, Err: err, Dur: 0, stream: s})
				return
			}
			util.EmitStreamAction("send", "success", 1)
		case <-s.closeCh:
			logger.Debug("stream close sendWorker exit")
			return

		}
	}
}

func (s *StreamClient) recvWorker() {
	defer s.ctl.waitWg.Done()
	for {
		rsp, err := s.stream.Recv()
		if err != nil {
			if atomic.LoadInt32(&s.ctl.isClose) == 1 { //由于其他原因关闭导致的Recv失败
				logger.Debug("stream close recvWorker exit")
				return
			}
			util.EmitStreamAction("recv", "fail", 1)
			logger.CtxWarn(s.ctx, "recvWorker error:%v", err)
			s.transport.AddMsg(&StreamCloseMsg{Ctx: s.ctx, Err: err, Dur: 0, stream: s})
			return
		}

		s.dispatchRespMsg(rsp)
	}
}

func (s *StreamClient) Close(err error) {
	if atomic.LoadInt32(&s.ctl.isClose) == 1 {
		return
	}

	atomic.StoreInt32(&s.ctl.isClose, 1)
	s.conn.Close() //关闭grpc底层连接
	//close(s.sendCh) 这个不能被关闭。因为还有一些协程在跑s.SendMsg(req)。 通过关闭s.recvTaskCh就已经能关闭那个sendWorker协程了
	close(s.closeCh)
	dur := time.Since(s.ctl.begin)
	logger.CtxWarn(s.ctx, "grpc conn closeStream err:%v, dur:%v", err, dur)
	logger.Debug("wait send,recv worker close")
	s.ctl.waitWg.Wait() //需要等待两个协程都退出，才返回。 不然当conn快速建立一个新的StreamClient时，会同时有两个StreamClient往conn发消息。
}

func (s *StreamClient) dispatchRespMsg(respMsg *pb.PipeRsp) {
	if respMsg.Ping == nil {
		logger.Debug("stream handle msg:%v", respMsg.Type)
	}

	if respMsg.Ping != nil {
		s.handlePingMsg(respMsg.Ping)
	} else if respMsg.Register != nil {
		s.handleRegisterMsg(respMsg.Register)
	} else if respMsg.UpdateKey != nil {
		s.handleUpdateKeyMsg(respMsg.UpdateKey)
	} else if respMsg.UpdatePath != nil {
		s.handleUpdatePathMsg(respMsg.UpdatePath)
	} else if respMsg.FinishPath != nil {
		s.handleFinishPathMsg(respMsg.FinishPath)
	}
}
func (s *StreamClient) handlePingMsg(pingInfo *pb.PingNotify) {
	msg := &common.PingMsg{}
	s.transport.AddMsg(msg)
}

func (s *StreamClient) handleRegisterMsg(regInfo *pb.RegisterNotify) {
	msg := &common.RegisterMsg{
		Code:         common.RegisterRspCode(regInfo.Code),
		CodeMsg:      regInfo.CodeMsg,
		WarnMsg:      regInfo.WarnMsg,
		ErrorMsg:     regInfo.ErrorMsg,
		PingInterval: regInfo.PingInterval,
	}

	s.transport.AddMsg(msg)
}

func (s *StreamClient) handleUpdateKeyMsg(keyInfo *pb.UpdateKeyNotify) {
	msg := &common.UpdateKeyMsg{KeyItem: common.PBItem2CommmonKeyItem(keyInfo.Item)}
	s.transport.AddMsg(msg)
}

func (s *StreamClient) handleUpdatePathMsg(keyInfo *pb.UpdatePathNotify) {
	logger.Debug("handleUpdatePathMsg: path:%v", keyInfo.Path)
	msg := &common.UpdatePathMsg{Path: keyInfo.Path, KeyItem: common.PBItem2CommmonKeyItem(keyInfo.Item)}
	s.transport.AddMsg(msg)
}

func (s *StreamClient) handleFinishPathMsg(finishInfo *pb.FinishPathNotify) {
	msg := common.NewFinishPathMsg(finishInfo.Path, finishInfo.Total, finishInfo.FailMsg, common.PathFinishCode(finishInfo.FinishCode))
	s.transport.AddMsg(msg)
}

//==============================================================================================================================

func (s *StreamClient) getStreamRequest(task interface{}) (req *pb.PipeReq) {
	logger.Debug("stream dispatch task:%v", tools.GetStructName(task))

	switch realTask := task.(type) {
	case *common.StreamPingTask:
		req = s.onPingTask()
	case *common.RegisterTask:
		req = s.onRegisterTask(realTask)

	case *common.StreamWatchKeyTask:
		req = s.onSendWatchKeyTask(realTask)

	case *common.StreamWatchPathTask:
		req = s.onSendWatchPathTask(realTask)

	case *common.StreamReportKeyTask:
		req = s.onReportKeyTask(realTask)

	case *common.StreamReportPathTask:
		req = s.onReportPathTask(realTask)

	case *common.StreamCancelKeyTask:
		req = s.onCancelKeyTask(realTask)

	case *common.StreamCancelPathTask:
		req = s.onCancelPathTask(realTask)

	default:
		logger.Info("unknow task[%s]", tools.GetStructName(task))
	}
	return req
}
func (s *StreamClient) onPingTask() *pb.PipeReq {
	req := &pb.PipeReq{Ping: &pb.PingRequest{Msg: utime.NowYmdhms()}}
	return req
}

func (s *StreamClient) onRegisterTask(task *common.RegisterTask) *pb.PipeReq {
	req := &pb.RegisterRequest{
		EnvParam:     task.Info.BuildEnvParam(),
		ClientName:   task.Info.ClientName,
		ConnectCount: task.Info.ConnectCount,
	}

	return &pb.PipeReq{Register: req}
}

func (s *StreamClient) onSendWatchKeyTask(task *common.StreamWatchKeyTask) *pb.PipeReq {
	pbItems := make(map[string]*pb.CltItem, len(task.Items))
	for key, item := range task.Items {
		pbItems[key] = common.CommonCtlItem2PbItem(item)
	}
	req := &pb.PipeReq{WatchKey: &pb.WatchKeyRequest{Items: pbItems}}

	return req
}

func (s *StreamClient) onSendWatchPathTask(task *common.StreamWatchPathTask) *pb.PipeReq {
	pbDir := common.CommonDirItem2PbItem(task.CltDir)
	req := &pb.PipeReq{WatchPath: &pb.WatchPathRequest{Dir: pbDir}}

	return req
}

func (s *StreamClient) onReportKeyTask(task *common.StreamReportKeyTask) *pb.PipeReq {
	req := &pb.PipeReq{ReportKey: &pb.ReportKeyRequest{Item: common.CommonCtlItem2PbItem(task.Item)}}
	return req
}

func (s *StreamClient) onReportPathTask(task *common.StreamReportPathTask) *pb.PipeReq {
	req := &pb.PipeReq{ReportPath: &pb.ReportPathRequest{Path: task.Path, Item: common.CommonCtlItem2PbItem(task.Item)}}
	return req
}

func (s *StreamClient) onCancelKeyTask(task *common.StreamCancelKeyTask) *pb.PipeReq {
	req := &pb.PipeReq{CancelKey: &pb.CancelKeyRequest{Keys: task.Keys}}
	return req

}

func (s *StreamClient) onCancelPathTask(task *common.StreamCancelPathTask) *pb.PipeReq {
	req := &pb.PipeReq{CancelPath: &pb.CancelPathRequest{Path: task.Path}}
	return req
}
