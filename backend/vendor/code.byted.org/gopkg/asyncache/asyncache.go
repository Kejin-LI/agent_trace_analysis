package asyncache

import (
	"errors"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"code.byted.org/gopkg/logs"
)

const (
	// DefaultExpiration .
	DefaultExpiration = time.Hour * 24 * 365

	stateNotPending = 0
	statePending    = 1
)

var nowFunc = time.Now // hack this func for test

type item struct {
	data     interface{} // data stored in this item
	dataLock sync.RWMutex

	lastUtime time.Time // last update time
	lastAtime time.Time // last access time
	timeLock  sync.RWMutex

	pending uint32 // if pending is ture, this item is being updated by some goroutine
}

func newEmptyItem() *item {
	return &item{lastAtime: nowFunc(), pending: stateNotPending}
}

func (si *item) getData() interface{} {
	si.dataLock.RLock()
	defer si.dataLock.RUnlock()
	return si.data
}

func (si *item) setData(data interface{}) {
	si.dataLock.Lock()
	si.data = data
	si.dataLock.Unlock()
}

func (si *item) updateAtime() {
	si.timeLock.Lock()
	si.lastAtime = nowFunc()
	si.timeLock.Unlock()
}

func (si *item) beingUsed(interval time.Duration) bool {
	si.timeLock.RLock()
	atime := si.lastAtime
	si.timeLock.RUnlock()
	return atime.Add(interval).After(nowFunc()) // 如果在interval内被访问过, 则认为正在被使用
}

func (si *item) updateUtime() {
	si.timeLock.Lock()
	si.lastUtime = nowFunc()
	si.timeLock.Unlock()
}

func (si *item) expired(expiration time.Duration) bool {
	si.timeLock.RLock()
	utime := si.lastUtime
	si.timeLock.RUnlock()
	return utime.Add(expiration).Before(nowFunc()) // 根据utime来判断是否过期
}

func (si *item) setPendingIfNot() (succ bool) {
	return atomic.CompareAndSwapUint32(&si.pending, stateNotPending, statePending)
}

func (si *item) isPending() bool {
	return atomic.LoadUint32(&si.pending) == statePending
}

func (si *item) rmPendingFlag() {
	atomic.StoreUint32(&si.pending, stateNotPending)
}

// EmptyErr represents the data of this key has not been feteched now for some reasons
var EmptyErr = errors.New("empty value")

// Getter used to get the data of this key
type Getter func(key string) (interface{}, error)

// SingleAsyncCache deprecated
type SingleAsyncCache struct {
	rlock sync.RWMutex
	f     Getter
	data  map[string]*item

	expiration time.Duration

	blocked      bool
	maxBlockTime time.Duration
}

// NewBlockedAsyncCache deprecated
func NewBlockedAsyncCache(f Getter, maxBlockTime time.Duration) *SingleAsyncCache {
	c := NewSingleAsyncCache(f)
	c.blocked = true
	c.maxBlockTime = maxBlockTime
	return c
}

// NewSingleAsyncCache deprecated
func NewSingleAsyncCache(f Getter) *SingleAsyncCache {
	cache := &SingleAsyncCache{
		f:          f,
		data:       make(map[string]*item),
		expiration: DefaultExpiration,
	}
	go cache.asyncRefresh()
	return cache
}

// Get return the value of the specified key, if the key is not in local
// call func to get it, in this func should control timeout
func (c *SingleAsyncCache) Get(key string) (interface{}, error) {
	c.rlock.RLock()
	v, ok := c.data[key]
	c.rlock.RUnlock()
	if ok {
		defer v.updateAtime()
		if !v.expired(c.expiration) { // not expred,
			return c.convertEmptyData(v.getData())
		}

		// expired, try to set pending flag and update it below
		if !v.setPendingIfNot() { // be being updated by other goroutine, use old data

			if c.blocked {
				begin := time.Now()
				for {
					if time.Now().Sub(begin) > c.maxBlockTime {
						break
					}

					if !v.isPending() {
						break
					}

					time.Sleep(time.Millisecond * 2)
				}
			}

			return c.convertEmptyData(v.getData())
		}
	} else { // create this item and prepare to fetching data
		v = newEmptyItem()
		v.setPendingIfNot()

		c.rlock.Lock()
		if newV, ok := c.data[key]; ok { // created by other goroutine
			c.rlock.Unlock()
			newV.updateAtime()
			return c.convertEmptyData(newV.getData())
		}
		c.data[key] = v
		c.rlock.Unlock()
	}

	// v is set pending by this gorouine, and update it now;
	defer v.rmPendingFlag()
	data, err := c.f(key)
	if err == nil {
		v.setData(data)
		v.updateUtime()
	} else {
		logs.Noticef("SingleAsyncCache.asyncKeys Get %s error: %s", key, err)
		return nil, err
	}

	return data, nil
}

func (c *SingleAsyncCache) convertEmptyData(data interface{}) (interface{}, error) {
	if data == nil {
		return nil, EmptyErr
	}
	return data, nil
}

func (c *SingleAsyncCache) refresh() {
	keys := make([]string, 0, 50)
	c.rlock.RLock()
	for key := range c.data {
		keys = append(keys, key)
	}
	c.rlock.RUnlock()

	for i := range keys {
		key := keys[i]
		c.rlock.RLock()
		item := c.data[key]
		c.rlock.RUnlock()

		// if !item.beingUsed(c.expiration) || !item.expired(c.expiration) {
		// 	continue
		// }

		if !item.setPendingIfNot() { // be being updated by other goroutine
			continue
		}

		data, err := c.f(key)
		if err == nil {
			item.setData(data)
			item.updateUtime()
		} else {
			logs.Noticef("SingleAsyncCache.asyncKeys Get %s error: %s", key, err)
		}
		item.rmPendingFlag()
	}
}

func (c *SingleAsyncCache) asyncRefresh() {
	interval := 3000 + rand.Intn(2000)
	ticker := time.NewTicker(time.Duration(interval) * time.Millisecond)
	for range ticker.C {
		c.refresh()
	}
}
