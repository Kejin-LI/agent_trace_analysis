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

type data struct {
	val   interface{}
	error error
}

// Asyncache .
type Asyncache struct {
	sfg  *Group
	opt  Options
	data map[string]*data
	lock sync.RWMutex
	exit chan struct{}
}

// NewAsyncache .
func NewAsyncache(opt Options) *Asyncache {
	c := &Asyncache{
		sfg:  &Group{},
		opt:  opt,
		data: make(map[string]*data),
		exit: make(chan struct{}),
	}
	go c.refresher()
	return c
}

// Get 获取查询结果，如果该 key 已经缓存了一个值，那么则直接返回缓存的值。
// 但是如果是下面的情况之一，则会直接返回 defaultVal。
//
//   1. 如果该 key 未缓存，且 BlockIfFirst == false，那么该方法会直接返回 defaultVal；
//   2. 如果该 key 未缓存，BlockIfFirst == true，并且底层查询出错，也返回 defaultVal；
//   3. 如果缓存中缓存了一个 error，那么该方法也会返回 defaultVal；
//
// 如果当前 key 未缓存，该方法会将传入的 defaultVal 缓存起来，
// 该结果对于后续的 Get 和 GetOrError 方法都有效。
func (c *Asyncache) Get(key string, defaultVal interface{}) interface{} {
	d, ok := c.getCache(key)
	if ok {
		if d.error == nil {
			return d.val
		} else {
			return defaultVal
		}
	}

	if !c.opt.BlockIfFirst {
		c.setCache(key, &data{val: defaultVal, error: nil})
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

		// 即使有错误，当成正确返回 defaultVal 处理
		val = defaultVal
		err = nil
	}

	c.setCache(key, &data{val: val, error: err})
	return val
}

func (c *Asyncache) getCache(key string) (*data, bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	val, ok := c.data[key]
	return val, ok
}

func (c *Asyncache) setCache(key string, val *data) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.data[key] = val
}

// GetOrError 获取值。如果缓存中有值，则从缓存中获取，如果缓存中没有值，则通过指定的 Fetcher 从底层获取。
//
// 如果该操作穿透到底层（比如第1次调用），那么该方法会将底层返回的 value 和 error 原样返回给调用方。
// 另外该结果还会缓存起来，缓存期内后续对相同的 key 的 GetOrError 调用，会直接将本次缓存的 value 和 error 返回。
//
// 如果该方法使得缓存了一个错误，那么：
//
// 1. 对相同的 key 通过 Get(key, defaultValue) 方法取值的时候会返回传入的 defaultValue 值。
// 但是相应的 defaultValue 不会缓存，其它的 GetOrError 方法调用仍然会取到之前缓存中的 value 和 error。
//
// 2. refresh 刷新的时候，如果取到了正确的值，那么相应的用于判断和处理变更的方法中老值都将是 nil。
func (c *Asyncache) GetOrError(key string) (interface{}, error) {
	d, ok := c.getCache(key)
	if ok {
		return d.val, d.error
	}

	// 避免启动时, 并发的对同一个key产生大量请求
	val, err := c.sfg.Do(key, func() (interface{}, error) {
		return c.opt.Fetcher(key)
	})

	if err != nil && c.opt.ErrHandler != nil {
		c.opt.ErrHandler(key, err)
	}

	// 对于错误的情况也往缓存中种一个墓碑，避免后续的请求一直穿透。
	// 在墓碑缓存过期之前，GetOrError方法将会一直取到最后的错误信息
	c.setCache(key, &data{val: val, error: err})
	return val, err
}

// Dump .
func (c *Asyncache) Dump() map[string]interface{} {
	data := make(map[string]interface{})

	c.lock.RLock()
	defer c.lock.RUnlock()

	for k, v := range c.data {
		data[k] = v
	}
	return data
}

// Close .
func (c *Asyncache) Close() {
	close(c.exit)
}

// Clear .
func (c *Asyncache) Clear() {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.data = make(map[string]*data)
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
	for key, oldVal := range c.Dump() {
		newVal, err := c.opt.Fetcher(key)
		oldData := oldVal.(*data)

		// 对于取新值出错的情况，通知使用方；
		// 但是不更新当前存储的正常的值，使之还能使用；
		// 如果当前存储的是一个错误信息，那么则更新改错误信息，以便使用方感知。
		if err != nil {
			if c.opt.ErrHandler != nil {
				go c.opt.ErrHandler(key, err)
			}

			if oldData.error != nil {
				oldData.error = err
			}

			continue
		}

		// 获取新值成功的情况，需要判断新值与旧值是否相同。
		// 如果旧值是一个错误信息，那么判断是否相同以及处理变更这两个函数中旧值都将是nil
		// TODO 这儿可能会改变ChangeHandler和IsSame方法的语义，需要仔细评估
		if c.opt.IsSame != nil && !c.opt.IsSame(key, oldData.val, newVal) {
			if c.opt.ChangeHandler != nil {
				go c.opt.ChangeHandler(key, oldData.val, newVal)
			}
		}

		c.setCache(key, &data{val: newVal, error: nil})
	}
}
