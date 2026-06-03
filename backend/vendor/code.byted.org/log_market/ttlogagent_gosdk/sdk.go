package ttlogagent_gosdk

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gogo/protobuf/proto"
	"golang.org/x/time/rate"

	"code.byted.org/gopkg/env"
	"code.byted.org/gopkg/metrics"
)

var (
	debugMode          bool
	raddr              *net.UnixAddr
	metricsClient      *metrics.MetricsClient
	errCount           = int64(0)
	network            = "unixpacket"
	socketPath         = "/opt/tmp/ttlogagent/unixpacket_v3.sock"
	sendSuccessTag     = map[string]string{"status": "success"}
	connRateLimitTag   = map[string]string{"status": "error", "reason": "connRateLimit"}
	connWriteErrorTag  = map[string]string{"status": "error", "reason": "connWriteError"}
	asyncWriteErrorTag = map[string]string{"status": "error", "reason": "asyncWrite"}
	connErrorTag       = map[string]string{"status": "error", "reason": "connError"}
	marshalErrorTag    = map[string]string{"status": "error", "reason": "marshalError"}
	msgTypeErrorTag    = map[string]string{"status": "error", "reason": "msgTypeError"}
	msgCutWarn         = map[string]string{"status": "warn", "reason": "msgCut"}
	ErrChannelFull     = errors.New("[logagent-gosdk] channel full")
	ErrMsgNil          = errors.New("msg cannot be nil")
	ErrStop            = errors.New("gosdk had exited gracefully ")
	host               []byte
)

const (
	statusStop = iota
	statusRunning
)

const (
	// [ttlogagent_gosdk] send message to [log agent ] by unix packet protocol on unix domain socket.
	// the protocol limit the socket buffer size.  160K on 32-bit linux and  208K on 64-bit linux.
	// so sdk will limit the message size.
	// if message greater than oneMessageLimitByte, the message will be truncated or loss.
	oneMessageLimitByte = 120 << 10
	traceLogTaskName    = "_rpc"
)

func init() {
	var err error
	if os.Getenv("STREAMLOG2_SIDECAR") == "1" {
		// for sandbox sidecar
		socketPath = "/ttlogagent_sidecar/sock/unixpacket_v3.sock"
	}
	raddr, err = net.ResolveUnixAddr("unixpacket", socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[logagent-gosdk] init error:%s", err.Error())
		return
	}
	mode := os.Getenv("ttlogagent_sdk_mode")
	if mode == "debug" {
		fmt.Println("[ttlogagent-gosdk] use debug mode")
		debugMode = true
	}
	metricsClient = metrics.NewDefaultMetricsClient("toutiao.infra.ttlogagent.gosdk_v3", true)
	host = net.ParseIP(env.HostIP())
}

type Config struct {
	// when the size of buffer greater than PacketSizeLimit, sdk will send the batch data to the [log agent].
	PacketSizeLimit int
	// if hadn't send any data to [log agent] in MaxBatchDelayMs yet, sdk will send the batch data to the [log agent].
	MaxBatchDelayMs int
	// because sdk is asynchronous, the ChannelSize specify the max size of message stored in channel.
	ChannelSize int
	// the timeout of write message to domain socket.
	WriteTimeoutMs int
}

type LogSender struct {
	Config
	header         *BatchHeader
	conn           net.Conn
	connRetryLimit *rate.Limiter
	batchSizeByte  int
	lastSendTime   time.Time
	buffer         *bytes.Buffer
	ch             chan Msg
	exitWg         *sync.WaitGroup
	batch          msgBatch
	versionByte    byte
	status         int32
	exitCh         chan struct{}
}

func NewLogSender(header *BatchHeader, config Config) *LogSender {
	// align https://code.byted.org/log_market/ttlogagent/blob/master/agent/utils/msg_version.go
	versionByte := byte(3)
	return newLogSender(
		header,
		config,
		&MsgV3Batch{
			BatchHeader: header,
			Msgs:        make([]*MsgV3, 0, 1024),
		},
		versionByte,
	)
}

// In log scenario , name generally is PSM.
func NewLogSenderByName(name string) *LogSender {
	return NewLogSender(NewDefaultHeader(name), NewDefaultConfig())
}

func NewTraceLogSender(psm string) *LogSender {
	header := NewDefaultHeader(traceLogTaskName)
	header.Psm = psm
	// align https://code.byted.org/log_market/ttlogagent/blob/master/agent/utils/msg_version.go
	versionByte := byte(5)
	return newLogSender(
		header,
		NewDefaultConfig(),
		&TraceLogBatch{
			BatchHeader: header,
			TraceLogs:   make([]*TraceLog, 0, 1024),
		},
		versionByte,
	)
}

func NewDefaultConfig() Config {
	return Config{
		// unix socket buffer size is :  160K on 32-bit linux, 208K on 64-bit linux. packetSizeLimit must be less than above.
		// and < 64KB packet will be more friendly for memory allocate.
		PacketSizeLimit: 60 * 1024,
		MaxBatchDelayMs: 200,
		ChannelSize:     4096,
		WriteTimeoutMs:  200,
	}
}

func NewDefaultHeader(name string) *BatchHeader {
	h := &BatchHeader{
		TaskName: name,
		Language: "go",
		Cluster:  env.Cluster(),
		Host:     host,
		Psm:      name,
		PodName:  env.PodName(),
		Stage:    env.Stage(),
		Idc:      env.IDC(),
	}
	return h
}

func newLogSender(header *BatchHeader, config Config, batch msgBatch, versionByte byte) *LogSender {
	return &LogSender{
		Config:         config,
		header:         header,
		connRetryLimit: rate.NewLimiter(rate.Limit(1), 1),
		batchSizeByte:  0,
		lastSendTime:   time.Now(),
		buffer:         new(bytes.Buffer),
		ch:             make(chan Msg, config.ChannelSize),
		exitWg:         &sync.WaitGroup{},
		exitCh:         make(chan struct{}),
		batch:          batch,
		versionByte:    versionByte,
	}
}

func (s *LogSender) Start() {
	if !atomic.CompareAndSwapInt32(&s.status, statusStop, statusRunning) {
		return
	}
	s.exitWg.Add(1)
	go s.run()
}

// asynchronously send message to [log agent].
// if the size of  message > 128KB, it will be truncated.
func (s *LogSender) Send(m Msg) error {
	if atomic.LoadInt32(&s.status) != statusRunning {
		return ErrStop
	}

	if m == nil {
		return ErrMsgNil
	}

	select {
	case s.ch <- m:
		return nil
	default:
		atomic.AddInt64(&errCount, 1)
		printToStderrIfDebug("[logagent-gosdk] discard message " + strconv.Itoa(int(atomic.LoadInt64(&errCount))))
		metricsClient.EmitCounter(s.header.TaskName+".send", 1, "", asyncWriteErrorTag)
		return ErrChannelFull
	}
}

func (s *LogSender) run() {
	if c, err := net.DialUnix(network, nil, raddr); err == nil {
		s.conn = c
	}
	ticker := time.NewTicker(time.Millisecond * time.Duration(s.MaxBatchDelayMs))
	defer ticker.Stop()
	for {
		select {
		case msg := <-s.ch:
			msgSize := msg.Size()
			// If this message is too long, then flush the batch and truncate the message
			if msgSize > oneMessageLimitByte {
				s.flush()
				msg.Truncate()
				msgSize = msg.Size()
				metricsClient.EmitCounter(s.header.TaskName+".send", 1, "", msgCutWarn)
			}

			if s.batchSizeByte+msgSize >= s.PacketSizeLimit {
				s.flush()
			}

			err := s.batch.appendMsg(msg)
			if err != nil {
				atomic.AddInt64(&errCount, 1)
				printToStderrIfDebug("[logagent-gosdk] appendMsgFail :" + err.Error())
				metricsClient.EmitCounter(s.header.TaskName+".send", 1, "", msgTypeErrorTag)
				continue
			}
			s.batchSizeByte += msgSize
		case <-ticker.C:
			if int(time.Now().Sub(s.lastSendTime))/int(time.Millisecond) > s.MaxBatchDelayMs {
				s.flush()
			}
		case <-s.exitCh:
			chanLen := len(s.ch)
			for i := 0; i < chanLen; i++ {
				select {
				case msg1 := <-s.ch:
					msgSize := msg1.Size()
					if msgSize > oneMessageLimitByte {
						s.flush()
						msg1.Truncate()
						msgSize = msg1.Size()
						metricsClient.EmitCounter(s.header.TaskName+".send", 1, "", msgCutWarn)
					}

					if s.batchSizeByte+msgSize >= s.PacketSizeLimit {
						s.flush()
					}

					err := s.batch.appendMsg(msg1)

					if err != nil {
						atomic.AddInt64(&errCount, 1)
						printToStderrIfDebug("[logagent-gosdk] appendMsgFail :" + err.Error())
						metricsClient.EmitCounter(s.header.TaskName+".send", 1, "", msgTypeErrorTag)
					}
					s.batchSizeByte += msgSize
				default:
				}
			}
			s.flush()
			s.exitWg.Done()
			return
		}
	}
}

func (s *LogSender) send(buf []byte) (error, map[string]string) {
	if s.conn == nil {
		if !s.connRetryLimit.Allow() {
			return fmt.Errorf("[logagent-gosdk] build connection error=rate limit"), connRateLimitTag
		}
		c, err := net.DialUnix(network, nil, raddr)
		if err != nil {
			return fmt.Errorf("[logagent-gosdk] build connection error=%s", err.Error()), connErrorTag
		}
		s.conn = c
	}
	s.conn.SetWriteDeadline(time.Now().Add(time.Millisecond * time.Duration(s.WriteTimeoutMs)))
	if _, err := s.conn.Write(buf); err != nil {
		s.conn.Close()
		s.conn = nil
		return fmt.Errorf("[logagent-gosdk] send batch error=%s", err.Error()), connWriteErrorTag
	}
	return nil, nil
}

func (s *LogSender) flush() {
	if s.batch.msgNumber() == 0 {
		return
	}
	defer func() {
		s.batchSizeByte = 0
		s.batch.cleanMsgs()
		s.lastSendTime = time.Now()
		s.buffer.Reset()
	}()

	data, err := proto.Marshal(s.batch)
	// [log agent ] v3 protocol : version (2 byte)| payload
	s.buffer.WriteByte(s.versionByte)
	s.buffer.WriteByte(0)
	s.buffer.Write(data)

	if err != nil {
		printToStderrIfDebug(fmt.Sprintf("[logagent-gosdk] proto marashal batch failed,error=%s", err.Error()))
		atomic.AddInt64(&errCount, int64(s.batch.msgNumber()))
		metricsClient.EmitCounter(s.header.TaskName+".send", s.batch.msgNumber(), "", marshalErrorTag)
		return
	}
	if err, errorTag := s.send(s.buffer.Bytes()); err != nil {
		printToStderrIfDebug("first send failed :" + err.Error())
		if err, errorTag = s.send(s.buffer.Bytes()); err != nil {
			atomic.AddInt64(&errCount, int64(s.batch.msgNumber()))

			metricsClient.EmitCounter(s.header.TaskName+".send", s.batch.msgNumber(), "", errorTag)
			if errorTag["reason"] == connWriteErrorTag["reason"] {
				fmt.Fprintf(os.Stderr, err.Error())
			} else {
				printToStderrIfDebug(err.Error())
			}
			return
		}
	}
	metricsClient.EmitCounter(s.header.TaskName+".send", s.batch.msgNumber(), "", sendSuccessTag)
}

func (s *LogSender) Exit() {
	if !atomic.CompareAndSwapInt32(&s.status, statusRunning, statusStop) {
		return
	}
	close(s.exitCh)
	s.exitWg.Wait()
}

func printToStderrIfDebug(msg string) {
	if !debugMode {
		return
	}
	fmt.Fprintln(os.Stderr, fmt.Sprintf("error count:%d ,%s ", atomic.LoadInt64(&errCount), msg))
}
