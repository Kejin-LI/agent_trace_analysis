package fetcher

import "time"

type keyStat struct {
	key           string
	lastFetchTime time.Time
	updateId      int64     //初始为0，数据更新时更新
	beginTime     time.Time //判断是否超过时间间隔需要轮询
}

func NewKeyStat(key string) *keyStat {
	return &keyStat{
		key:           key,
		lastFetchTime: time.Time{},
		updateId:      0,
		beginTime:     time.Now(),
	}
}
func (k *keyStat) Update(updateId int64) {
	// auth failed时updateId为0，可以回退到0
	if updateId > 0 && updateId < k.updateId {
		return
	}
	k.lastFetchTime = time.Now()
	k.updateId = updateId
}

func (k *keyStat) needFetch(interval time.Duration) bool {
	if k.lastFetchTime.IsZero() && time.Since(k.beginTime) >= interval {
		return true
	}
	if !k.lastFetchTime.IsZero() && time.Since(k.lastFetchTime) >= interval {
		return true
	}
	return false
}

type pathStat struct {
	path          string
	lastFetchTime time.Time //初始为0，数据更新时更新
	beginTime     time.Time //判断是否需要轮询
}

// Update FixMe 有可能在push 链路下发数据中，不断刷新path的时间，之后push挂了并且不可恢复
// 同时刚好这段时间这个目录下有新的更新导致请求拉链路时，由于时间是path下发的最新一次数据的时间，导致拉不到更新
// 概率很小，暂时不考虑
func (p *pathStat) Update() {
	p.lastFetchTime = time.Now()
}

func (p *pathStat) needFetch(interval time.Duration) bool {
	if p.lastFetchTime.IsZero() && time.Since(p.beginTime) >= interval {
		return true
	}
	if !p.lastFetchTime.IsZero() && time.Since(p.lastFetchTime) >= interval {
		return true
	}
	return false
}

func (p *pathStat) getLastFetchTs() (getLastFetchTs int64) {
	if !p.lastFetchTime.IsZero() {
		getLastFetchTs = p.lastFetchTime.UnixNano()
	}
	return
}

func NewPathStat(path string) *pathStat {
	return &pathStat{
		path:          path,
		lastFetchTime: time.Time{},
		beginTime:     time.Now(),
	}
}
