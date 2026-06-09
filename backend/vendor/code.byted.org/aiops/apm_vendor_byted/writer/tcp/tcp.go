package tcp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"code.byted.org/aiops/apm_vendor_byted/config_service"
	"code.byted.org/aiops/apm_vendor_byted/vendor_tags"
	"code.byted.org/aiops/apm_vendor_byted/writer"
	"code.byted.org/aiops/apm_vendor_byted/writer/common"
	"code.byted.org/aiops/monitoring-common-go/connpool"
	"code.byted.org/gopkg/consul"
)

const (
	producerTimeOut        = time.Second * 5
	defaultConnIdleTimeout = time.Second * 30
	defaultConnLifeTime    = time.Second * 60
	defaultConnLifeCounter = 20
)

const (
	DEFAULT_TENANT                  = "default"
	METRICS_PRODUCER_FOR_TENANT     = "inf.bytetsd.producer_tenant"
	METRICS_PRODUCER_TENANT_ENV_VAR = "METRICS_PRODUCER_TENANT"
	METRICS_PRODUCER_DC_ENV_VAR     = "METRICS_PRODUCER_DC"
)

const (
	_ uint16 = iota
	Ok
	Continue
)

var (
	logfunc                 = common.LogFunc
	ErrNoAvailableEndpoints = errors.New("no available endpoints")
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

type producerTcpWriter struct {
	// resources
	packetCh   chan *common.MetricPacket
	cfgService *config_service.BytedMetricsConfigProvider
	packetPool *common.PacketPool
	connPools  map[string]connpool.Pool
	sync.WaitGroup
	done chan struct{}
	once sync.Once

	// status set after initialization
	status              int32
	destinationServices []string
	producerDC          string

	// properties can be set by options
	producerName         string
	isProducerNameSet    bool
	tenant               string // producer cluster for tenant
	debugMode            bool
	copyData             bool
	onlyUseConfigService bool
	dc                   string
	sendWorkers          int
	bufferSize           int
	retryCount           int
	connIdleTimeout      time.Duration
	connLifetime         time.Duration
	connLifeCounter      uint32
}

// NewTcpWriter returns a producer tcp writer if there is no error.
// Otherwise, it returns a noop writer and the error.
func NewTcpWriter(ops ...TcpWriterOption) (io.WriteCloser, error) {
	w := &producerTcpWriter{
		packetPool:      common.NewPacketPool(),
		done:            make(chan struct{}),
		WaitGroup:       sync.WaitGroup{},
		bufferSize:      128,
		retryCount:      3,
		sendWorkers:     int(math.Max(float64(runtime.GOMAXPROCS(0)), 1)),
		dc:              getEnvVar(METRICS_PRODUCER_DC_ENV_VAR, vendor_tags.GetDC()),
		tenant:          getEnvVar(METRICS_PRODUCER_TENANT_ENV_VAR, DEFAULT_TENANT),
		connPools:       make(map[string]connpool.Pool),
		connIdleTimeout: defaultConnIdleTimeout,
		connLifetime:    defaultConnLifeTime,
		connLifeCounter: defaultConnLifeCounter,
	}

	// TLB case: set tenant with env variable
	if w.tenant != DEFAULT_TENANT {
		w.producerName = METRICS_PRODUCER_FOR_TENANT
		w.isProducerNameSet = true
	}

	for _, op := range ops {
		op(w)
	}

	err := w.checkConfig()
	if err != nil {
		logfunc("[Error] invalid configuration: %v", err)
	}

	w.packetCh = make(chan *common.MetricPacket, int(math.Max(float64(w.sendWorkers), float64(w.bufferSize))))

	isTenantClusterReady := true
	// producer name is set in the following two cases:
	// 1. Some tenants will set producer name in the code: TLB, TCE, ByteDoc, MySQL.
	// 2. Sandbox Tests will set producer name in the code.
	if w.isProducerNameSet {
		_, err := w.consulDiscover(w.producerName, w.tenant, w.producerDC)
		if err != nil {
			logfunc("failed to get producer: %s, tenant: %s, dc: %s, err: %v, will use default producer cluster.", w.producerName, w.tenant, w.producerDC, err)
			isTenantClusterReady = false
		} else {
			w.destinationServices = []string{w.producerName}
		}
	}

	if !isTenantClusterReady || !w.isProducerNameSet {
		// GetProducer always return a producer even when error is not nil.
		producerName, err := w.getCfgService().GetProducer()
		if err != nil {
			logfunc("[Error] failed to get the producer service name from config service, will use the default producer name: %v, error: %v, ", producerName, err)
		}
		w.destinationServices = strings.Split(producerName, ",")
	}

	// if conn pools is not empty, it is injected by options
	var lastErr error
	if len(w.connPools) == 0 {
		tenant := w.tenant
		if !isTenantClusterReady {
			tenant = DEFAULT_TENANT
		}

		for _, dest := range w.destinationServices {
			pool, err := connpool.NewChannelPool(&connpool.Config{
				Factory: func() (net.Conn, error) {
					conn, err := w.createConn(dest, tenant, w.dc)
					return conn, err
				},
				Close: func(i net.Conn) error {
					return i.Close()
				},
				InitialCap:  int(math.Min(float64(runtime.GOMAXPROCS(0)), float64((w.sendWorkers)/2+1))),
				MaxIdle:     w.sendWorkers,
				MaxCap:      w.sendWorkers,
				IdleTimeout: w.connIdleTimeout,
				LifeTime:    w.connLifetime,
				LifeCounter: w.connLifeCounter,
			})
			if err != nil {
				logfunc("[Error] failed to create the conn pool for dest: %v, err: %v", dest, err)
				lastErr = err
				continue
			}
			w.connPools[dest] = pool
		}
	}

	if len(w.connPools) == 0 {
		return writer.NewNoopWriter(), fmt.Errorf("no available conn pools, will discard all the data, last error: %v", lastErr)
	}

	w.start()
	atomic.StoreInt32(&w.status, common.StatusRunning)
	return w, nil
}

func (w *producerTcpWriter) Write(p []byte) (n int, err error) {
	return common.ConvertAndSend(w, p)
}

func (w *producerTcpWriter) Close() error {
	if atomic.CompareAndSwapInt32(&w.status, common.StatusRunning, common.StatusStop) {
		close(w.done)
		w.Wait()
		for _, connPool := range w.connPools {
			connPool.Release()
		}
	}
	return nil
}

func (w *producerTcpWriter) checkConfig() error {
	var err error
	// conflicts in configuration
	if w.isProducerNameSet && w.onlyUseConfigService {
		w.onlyUseConfigService = false
		logfunc("set producer service and use config service at the same time. "+
			"use user-provided producer: %v instead of config service", w.producerName)
	}

	// get vdc from metrics configuration service
	w.producerDC, err = w.getCfgService().GetProducerDC()
	if err != nil {
		w.producerDC = w.dc
		logfunc("failed to get vdc from config service, will use the default vdc: %v, error: %v,", w.producerDC, err)
	}

	return err
}

func (w *producerTcpWriter) start() {
	for i := 0; i < w.sendWorkers; i++ {
		w.Add(1)
		id := i
		go common.RunSendTask(w, id, w.retryCount, w.Done)
	}
}

/*
	Following methods implements PacketSender
*/

func (w *producerTcpWriter) Compress(packet *common.MetricPacket, rawData []byte) error {
	err := packet.Compress(rawData)
	if err != nil {
		return err
	}
	packet.CompressHeader = consHeader(packet.CompressHeader, len(packet.CompressData), len(rawData), packet.IsCodecV4)
	packet.OriginSize = len(rawData)
	return err
}

func (w *producerTcpWriter) GetPacketChan() chan *common.MetricPacket {
	return w.packetCh
}

func (w *producerTcpWriter) GetPacketPool() *common.PacketPool {
	return w.packetPool
}

func (w *producerTcpWriter) IsDeepCopyData() bool {
	return w.copyData
}
func (w *producerTcpWriter) IsCodecV4Supported() bool {
	return true
}

func (w *producerTcpWriter) IsDebugMode() bool {
	return w.debugMode
}

func (w *producerTcpWriter) GetDone() <-chan struct{} {
	return w.done
}

// DirectSend sends the bytes to producer via tcp conn.
// It also updates the connection if failing to send the data.
func (w *producerTcpWriter) DirectSend(packet *common.MetricPacket) error {
	if len(packet.CompressData) == 0 {
		logfunc("empty compressed data")
		return nil
	}

	if w.debugMode {
		logfunc("send %d compressed data", len(packet.CompressData))
	}

	if len(w.connPools) == 1 {
		// if there is just one destination, there no need to send with goroutines.
		// and retry is controlled by common.SendWithRetry
		for dest, pool := range w.connPools {
			return w.sendData(dest, pool, packet, 1)
		}
	} else {
		// send data to multiply destinations
		return w.sendDataConcurrently(packet)
	}

	return nil
}

func (w *producerTcpWriter) sendDataConcurrently(packet *common.MetricPacket) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(w.connPools))

	for dest, pool := range w.connPools {
		wg.Add(1)

		go func(dest string, pool connpool.Pool) {
			defer wg.Done()
			err := w.sendData(dest, pool, packet, w.retryCount)
			errChan <- err
			if err != nil {
				logfunc("failed to send data to %s, err: %w", dest, err)
			}
		}(dest, pool)
	}

	wg.Wait()

	if w.debugMode {
		logfunc("successfully send data, len: %d", len(packet.CompressData))
	}

	return <-errChan
}

func (w *producerTcpWriter) sendData(dest string, pool connpool.Pool, packet *common.MetricPacket, retryTime int) error {
	for i := 0; i < retryTime; i++ {
		conn, err := pool.Get()
		if err != nil {
			logfunc("failed to get connection for destination %s: %w", dest, err)
			continue
		}

		err = conn.SetWriteDeadline(time.Now().Add(producerTimeOut))
		if err != nil {
			logfunc("failed to set write deadline for destination %s: %w", dest, err)
			_ = pool.Put(conn)
			continue
		}

		_, err = conn.Write(packet.CompressHeader)
		if err != nil {
			logfunc("phase1: write proxy error for destination %s: %w", dest, err)
			_ = pool.Put(conn)
			continue
		}

		code, err := w.recvResp(conn)
		if err != nil {
			logfunc("phase1: receive response error for destination %s: %w", dest, err)
			_ = pool.Put(conn)
			continue
		}

		if code != Continue {
			logfunc("phase1: recv from proxy not continue for destination %s: %d", dest, code)
			_ = pool.Put(conn)
			continue
		}

		err = conn.SetWriteDeadline(time.Now().Add(producerTimeOut))
		if err != nil {
			logfunc("failed to set write deadline for destination %s: %w", dest, err)
			_ = pool.Put(conn)
			continue
		}

		_, err = conn.Write(packet.CompressData)
		if err != nil {
			logfunc("write body to proxy error for destination %s: %w", dest, err)
			_ = pool.Put(conn)
			continue
		}

		code, err = w.recvResp(conn)
		if err != nil {
			logfunc("phase2: receive response error for destination %s: %w", dest, err)
			_ = pool.Put(conn)
			continue
		}

		if code != Ok {
			logfunc("phase2: recv from proxy not ok for destination %s: %d", dest, code)
			_ = pool.Put(conn)
			continue
		}

		err = pool.Put(conn)
		if err != nil {
			logfunc("failed to return connection to pool for destination %s: %w", dest, err)
			continue
		}

		if w.debugMode {
			logfunc("successfully send data to destination %s, len: %d", dest, len(packet.CompressData))
		}

		return nil // If everything is successful, return nil
	}

	return fmt.Errorf("failed to send data after %d retries", retryTime)
}

func (w *producerTcpWriter) getCfgService() *config_service.BytedMetricsConfigProvider {
	w.once.Do(func() {
		if w.cfgService == nil {
			w.cfgService = config_service.NewBytedMetricConfigProvider(config_service.SetDC(w.dc))
		}
	})
	return w.cfgService
}

func (w *producerTcpWriter) createConn(dest, tenant, dc string) (conn net.Conn, err error) {
	for i := 0; i < w.retryCount; i++ {
		var addr string
		addr, err = w.discover(dest, tenant, dc)
		if err != nil {
			time.Sleep(time.Millisecond * 100)
			continue
		}
		conn, err = net.Dial("tcp", addr)
		if err != nil {
			continue
		}

		return conn, nil
	}
	return nil, err

}

func (w *producerTcpWriter) discover(serviceName, tenant, dc string) (string, error) {
	var endpoint string
	var err error
	if w.onlyUseConfigService {
		endpoint, err = w.configServiceDiscover()
	} else {
		endpoint, err = w.consulDiscover(serviceName, tenant, dc)

		if err != nil {
			logfunc("failed to get producer: %s, tenant: %s, dc: %s, endpoints: %v", serviceName, tenant, dc, err)
			// if new producer is no available, first try default producer via consul.
			if serviceName == METRICS_PRODUCER_FOR_TENANT {
				var producerName string
				producerName, err = w.getCfgService().GetProducer()
				if err == nil {
					logfunc("try to use default producer: %s from consul", producerName)
					endpoint, err = w.consulDiscover(producerName, DEFAULT_TENANT, w.producerDC)
				}
			}
			// then try config service apis.
			if err != nil {
				logfunc("try to get producer endpoints from config service")
				endpoint, err = w.configServiceDiscover()
			}
		}
	}
	return endpoint, err
}

func (w *producerTcpWriter) configServiceDiscover() (string, error) {
	endpoints, err := w.getCfgService().GetProducerEndpoints()
	if err != nil {
		return "", err
	}

	numEndpoint := len(endpoints)
	if numEndpoint == 0 {
		return "", ErrNoAvailableEndpoints
	}
	index := rand.Intn(numEndpoint)
	return endpoints[index], nil
}

func (w *producerTcpWriter) consulDiscover(producerName, tenant string, dc string) (string, error) {
	consulOpts := make([]consul.LookupOptions, 0, 1)

	if tenant != "" && tenant != DEFAULT_TENANT {
		consulCluster := fmt.Sprintf("%s_%s", tenant, dc)
		consulOpts = append(consulOpts, consul.WithCluster(consulCluster))
	}

	endpoints, err := consul.LookupName(producerName, consulOpts...)
	if err != nil {
		return "", err
	}

	if len(endpoints) == 0 {
		return "", ErrNoAvailableEndpoints
	}
	return endpoints.GetOne().Addr, nil
}

func (w *producerTcpWriter) recvResp(conn net.Conn) (uint16, error) {
	recv := make([]byte, 10)
	err := conn.SetReadDeadline(time.Now().Add(producerTimeOut))
	if err != nil {
		return 0, fmt.Errorf("failed to set read deadline: %w", err)
	}

	n, err := conn.Read(recv)
	if err != nil {
		return 0, fmt.Errorf("recv from proxy error: %w, received bytes: %d", err, n)
	}
	return binary.LittleEndian.Uint16(recv[:2]), nil
}

func consHeader(buf []byte, compSize, origSize int, useCodecV4 bool) []byte {
	var buffer = bytes.NewBuffer(buf)

	_ = binary.Write(buffer, binary.LittleEndian, uint16(1))

	if useCodecV4 {
		_ = binary.Write(buffer, binary.LittleEndian, int16(-1)) // Message header is no compressed, read body header first to determine the Compress alg.
	} else {
		_ = binary.Write(buffer, binary.LittleEndian, uint16(0)) // all data is zlib compressed
	}
	_ = binary.Write(buffer, binary.LittleEndian, uint32(compSize))
	_ = binary.Write(buffer, binary.LittleEndian, uint32(origSize))
	_ = binary.Write(buffer, binary.LittleEndian, uint64(0))
	return buffer.Bytes()
}

func getEnvVar(envVar, defaultValue string) string {
	if value := os.Getenv(envVar); value != "" {
		return value
	}
	return defaultValue
}

type TcpWriterOption func(w *producerTcpWriter)

func WithOnlyUseConfigService() TcpWriterOption {
	return func(w *producerTcpWriter) {
		w.onlyUseConfigService = true
	}
}

func WithDC(dc string) TcpWriterOption {
	return func(w *producerTcpWriter) {
		w.dc = dc
	}
}

func WithParallelism(n int) TcpWriterOption {
	return func(w *producerTcpWriter) {
		if n > 0 {
			w.sendWorkers = n
		}
	}
}

func WithBufferSize(n int) TcpWriterOption {
	return func(w *producerTcpWriter) {
		if n > 1 {
			w.bufferSize = n
		}
	}
}

func WithCopyData() TcpWriterOption {
	return func(w *producerTcpWriter) {
		w.copyData = true
	}
}

func WithDebugMode() TcpWriterOption {
	return func(w *producerTcpWriter) {
		w.debugMode = true
	}
}

func WithRetryTimes(n int) TcpWriterOption {
	return func(w *producerTcpWriter) {
		if n > 0 {
			w.retryCount = n
		}
	}
}

// WithConnTimeout sets timeout of idle connections.
// The default timeout is 60 * second.
func WithConnTimeout(duration time.Duration) TcpWriterOption {
	return func(w *producerTcpWriter) {
		if duration > 0 {
			w.connIdleTimeout = duration
		}
	}
}

// WithConnLifetime sets the lifetime of the active connections.
// The default lifetime is 30 second.
func WithConnLifetime(duration time.Duration) TcpWriterOption {
	return func(w *producerTcpWriter) {
		if duration > 0 {
			w.connLifetime = duration
		}
	}
}

// WithConnLifeCounter sets the life counter of the active connections.
// The default life counter is 20.
func WithConnLifeCounter(counter uint32) TcpWriterOption {
	return func(w *producerTcpWriter) {
		w.connLifeCounter = counter
	}
}

// WithProducerName directly sets the producer name. It is usually for test purpose.
func WithProducerName(name string) TcpWriterOption {
	return func(w *producerTcpWriter) {
		w.producerName = name
		w.isProducerNameSet = true
	}
}

// WithTenant force tcp sender to use specific cluster of metrics producer for tenants.
// User can use their tenant name as the cluster.
func WithTenant(tenant string) TcpWriterOption {
	return func(w *producerTcpWriter) {
		w.producerName = METRICS_PRODUCER_FOR_TENANT
		w.isProducerNameSet = true
		w.tenant = tenant
	}
}

// WithTenantCluster returns a TcpWriterOption using the specified cluster.
//
// Deprecated: Use WithTenant instead.
func WithTenantCluster(cluster string) TcpWriterOption {
	return WithTenant(cluster)
}

func WithConfigService(cfgSvr *config_service.BytedMetricsConfigProvider) TcpWriterOption {
	return func(w *producerTcpWriter) {
		w.cfgService = cfgSvr
	}
}

// WithHostPorts directly sets the hostports of destination.
// This is for test purpose.
func WithHostPorts(hostports ...string) TcpWriterOption {
	logfunc("use hostports: %v", hostports)
	return withCreateConnFunc(
		func() (conn net.Conn, err error) {
			if len(hostports) <= 0 {
				return nil, ErrNoAvailableEndpoints
			}
			addr := hostports[rand.Intn(len(hostports))]
			conn, err = net.Dial("tcp", addr)
			return
		})
}

// withConnPools is a dangerous option that directly set the connpools of the tcp sender.
// it can force set multi destinations for the sender.
// it is just for test purpose at this point.
func withConnPools(pools map[string]connpool.Pool) TcpWriterOption {
	return func(w *producerTcpWriter) {
		w.connPools = pools
	}
}

// withCreateConnFunc is a dangerous option that force set the destination.
// it is just for test purpose at this point.
func withCreateConnFunc(createConnFunc func() (conn net.Conn, err error), options ...TcpWriterOption) TcpWriterOption {
	return func(w *producerTcpWriter) {
		for _, op := range options {
			op(w)
		}

		pool, err := connpool.NewChannelPool(&connpool.Config{
			Factory: func() (net.Conn, error) {
				conn, err := createConnFunc()
				return conn, err
			},
			Close: func(i net.Conn) error {
				return i.Close()
			},
			InitialCap:  int(math.Min(float64(runtime.GOMAXPROCS(0)), float64((w.sendWorkers)/2+1))),
			MaxIdle:     w.sendWorkers,
			MaxCap:      w.sendWorkers,
			IdleTimeout: w.connIdleTimeout,
			LifeTime:    w.connLifetime,
			LifeCounter: w.connLifeCounter,
		})

		if err != nil {
			return
		}

		withConnPools(map[string]connpool.Pool{
			"inject.destination": pool})(w)
	}
}
