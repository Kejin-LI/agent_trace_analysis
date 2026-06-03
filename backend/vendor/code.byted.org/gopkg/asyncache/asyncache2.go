package asyncache

import (
	"sync"
	"time"

	"code.byted.org/gopkg/logs"
)

// Options .
type Options struct {
	BlockIfFirst    bool
	RefreshDuration time.Duration
	Fetcher         func(key string) (interface{}, error)
	ErrHandler      func(key string, err error)
	ChangeHandler   func(key string, oldData, newData interface{})
	IsSame          func(key string, oldData, newData interface{}) bool
}

// Asyncache .
type Asyncache struct {
	sfg  *Group
	opt  Options
	data map[string]interface{}
	lock sync.RWMutex
	exit chan struct{}
}

// NewAsyncache .
func NewAsyncache(opt Options) *Asyncache {
	c := &Asyncache{
		sfg:  &Group{},
		opt:  opt,
		data: make(map[string]interface{}),
		exit: make(chan struct{}),
	}
	go c.refresher()
	return c
}

// Get .
func (c *Asyncache) Get(key string, defaultVal interface{}) interface{} {
	c.lock.RLock()
	val, ok := c.data[key]
	c.lock.RUnlock()
	if ok {
		return val
	}

	if !c.opt.BlockIfFirst {
		c.lock.Lock()
		c.data[key] = defaultVal
		c.lock.Unlock()
		return defaultVal
	}

	// 避免启动时, 并发的对同一个key产生大量请求
	val, err := c.sfg.Do(key, func() (interface{}, error) {
		return c.opt.Fetcher(key)
	})
	if err != nil {
		if c.opt.ErrHandler != nil {
			c.opt.ErrHandler(key, err)
		} else {
			logs.Errorf("first fetch %s err: %s, default value: %v", key, err.Error(), val)
		}
		val = defaultVal
	}

	c.lock.Lock()
	c.data[key] = val
	c.lock.Unlock()
	return val
}

// Dump .
func (c *Asyncache) Dump() map[string]interface{} {
	data := make(map[string]interface{})
	c.lock.RLock()
	for k, v := range c.data {
		data[k] = v
	}
	c.lock.RUnlock()
	return data
}

// Close .
func (c *Asyncache) Close() {
	close(c.exit)
}

// Clear .
func (c *Asyncache) Clear() {
	c.lock.Lock()
	c.data = make(map[string]interface{})
	c.lock.Unlock()
}

func (c *Asyncache) refresher() {
	ch := time.Tick(c.opt.RefreshDuration)
	for {
		select {
		case <-c.exit:
			return
		case <-ch:
			c.refresh()
		}
	}
}

func (c *Asyncache) refresh() {
	oldData := c.Dump()
	for key, oldVal := range oldData {
		newVal, err := c.opt.Fetcher(key)
		if err != nil {
			go c.opt.ErrHandler(key, err)
			continue
		}
		if c.opt.IsSame != nil && !c.opt.IsSame(key, oldVal, newVal) {
			if c.opt.ChangeHandler != nil {
				go c.opt.ChangeHandler(key, oldVal, newVal)
			}
		}

		c.lock.Lock()
		c.data[key] = newVal
		c.lock.Unlock()
	}
}
