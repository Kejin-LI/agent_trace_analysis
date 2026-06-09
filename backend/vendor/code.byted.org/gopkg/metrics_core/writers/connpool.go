package writers

import (
	"fmt"
	"math"
	"net"
	"strings"
	"sync"
	"time"

	"code.byted.org/aiops/monitoring-common-go/connpool"
	"code.byted.org/gopkg/metrics_core/logs"
)

type ConnPoolManager interface {
	GetPool(sc *socketConfig) (connpool.Pool, error)
	ReleasePool(sc *socketConfig)
	IncreasePoolConns(sc *socketConfig) bool
}

type socketConfig struct {
	Addrs   []string
	Network string

	id string
}

func (c *socketConfig) Id() string {
	if c.id == "" {
		c.id = fmt.Sprintf("%s_%s", c.Network, strings.Join(c.Addrs, "_"))
	}
	return c.id
}

func NewConnPoolManager(maxCap, maxIdle, initialCap int,
	adjustInterval time.Duration, adjustThreshold int, adjustStep float64) ConnPoolManager {
	manager := &connPoolManager{
		maxCap:          maxCap,
		maxIdle:         maxIdle,
		initialCap:      initialCap,
		adjustInterval:  adjustInterval,
		adjustThreshold: adjustThreshold,
		adjustStep:      adjustStep,

		connPools:   map[string]connpool.Pool{},
		poolRefs:    map[string]int{},
		adjustTimes: map[string]time.Time{},
	}
	go manager.monitorAndAdjustConns()
	return manager
}

type connPoolManager struct {
	maxCap     int
	maxIdle    int
	initialCap int

	adjustInterval  time.Duration
	adjustThreshold int
	adjustStep      float64

	connPools   map[string]connpool.Pool
	poolRefs    map[string]int
	adjustTimes map[string]time.Time
	poolLock    sync.RWMutex
}

func (c *connPoolManager) GetPool(sc *socketConfig) (connpool.Pool, error) {
	c.poolLock.Lock()
	defer c.poolLock.Unlock()
	exists, ok := c.connPools[sc.Id()]
	if ok {
		c.poolRefs[sc.Id()]++
		return exists, nil
	}
	p, err := connpool.NewChannelPool(&connpool.Config{
		Factory:     func() (net.Conn, error) { return c.connectAgentSocket(sc) },
		Close:       func(i net.Conn) error { return i.Close() },
		MaxIdle:     c.maxIdle,
		MaxCap:      c.initialCap, // init as the initialCap, will adjust automatically
		InitialCap:  c.initialCap,
		IdleTimeout: 10 * time.Minute,
	})
	if err != nil {
		return nil, err
	}
	c.connPools[sc.Id()] = p
	c.poolRefs[sc.Id()]++
	return p, nil
}

func (c *connPoolManager) ReleasePool(sc *socketConfig) {
	c.poolLock.Lock()
	defer c.poolLock.Unlock()
	refs := c.poolRefs[sc.Id()]
	if refs <= 0 {
		return
	}
	refs -= 1
	c.poolRefs[sc.Id()] = refs
	if refs == 0 {
		c.connPools[sc.Id()].Release()
		delete(c.poolRefs, sc.Id())
		delete(c.connPools, sc.Id())
	}
}

func (c *connPoolManager) IncreasePoolConns(sc *socketConfig) bool {
	return c.increaseConns(sc.Id())
}

func (c *connPoolManager) connectAgentSocket(sc *socketConfig) (net.Conn, error) {
	var err error
	var conn net.Conn
	for _, path := range sc.Addrs {
		for i := 0; i < 3; i++ {
			conn, err = net.Dial(sc.Network, path)
			if err == nil {
				logs.Debug("using this socket: %s", path)
				return conn, nil
			}

			if conn != nil {
				_ = conn.Close()
			}

			if strings.Contains(err.Error(), "no such file") {
				logs.Debug("didn't found the socket file %s", path)
				break
			}
		}
	}
	return nil, fmt.Errorf("metrics sdk can not get agent socket path: %s", err)
}

func (c *connPoolManager) increaseConns(socketId string) bool {
	c.poolLock.Lock()
	lastTime, ok := c.adjustTimes[socketId]
	if ok && time.Since(lastTime) < c.adjustInterval {
		c.poolLock.Unlock()
		return false
	}
	refTotal := c.poolRefs[socketId]
	p := c.connPools[socketId]
	c.poolLock.Unlock()
	if p == nil || p.Opening() == 0 { // if no opening, means addrs broken, no need adjust
		return false
	}
	maxCap := int(math.Max(float64(refTotal), float64(c.maxCap)))
	currMaxCap := p.GetMaxCap()
	if currMaxCap >= maxCap {
		return false
	}
	step := c.adjustStep
	if step < 1 {
		// if step < 1, means using as the percentage of total refs
		step = math.Max(c.adjustStep*float64(maxCap), 1)
	}
	newMaxCap := int(math.Min(float64(currMaxCap)+step, float64(maxCap)))
	if newMaxCap == 0 {
		return false
	}

	// do connections adjust
	if p != nil {
		logs.Debug("adjust MaxCap of connection pool(%s) to: %d", socketId, newMaxCap)
		p.SetMaxCap(newMaxCap)
	}
	c.poolLock.Lock()
	c.adjustTimes[socketId] = time.Now()
	c.poolLock.Unlock()
	return true
}

// monitorAndAdjustConns will check the conn-pools periodically
// if one pool's idle conns keep at zero continuously for connPoolManager.adjustThreshold periods,
// then connPoolManager.adjustStep new conns will be injected into the pool
func (c *connPoolManager) monitorAndAdjustConns() {
	defer func() {
		if err := recover(); err != nil {
			logs.Error("panic on monitorAndAdjustConns, err: %s", err)
		}
	}()
	if c.adjustThreshold < 0 {
		logs.Debug("adjust pool connections by interval disabled")
		return
	}
	tick := time.NewTicker(time.Duration(float64(c.adjustInterval) * 0.99))
	watermarks := map[string]int{}
	for range tick.C {
		c.poolLock.Lock()
		for id, p := range c.connPools {
			if p.Len() > 0 {
				watermarks[id] = 0 // reset watermark
				continue
			}
			watermarks[id]++
		}
		c.poolLock.Unlock()

		for id, i := range watermarks {
			if i >= c.adjustThreshold && c.adjustThreshold > 0 {
				c.increaseConns(id)
			}
		}
	}
}
