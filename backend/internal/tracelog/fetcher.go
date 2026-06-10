// Package tracelog HTTP fetcher with TTL + LRU cache.
//
// 详情页每次打开都需拉取一份 TOS JSONL（10-30MB 不等）。
// 优化点：
//  1. 请求带 Accept-Encoding: gzip，TOS 通常支持，下载量砍 80%。
//  2. 5 分钟 TTL + LRU 容量上限（防 OOM）。
//  3. Prewarm 接口给列表页异步预热前 N 条 obj_url。
package tracelog

import (
	"compress/gzip"
	"container/list"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// DefaultCacheTTL 默认缓存有效期。
const DefaultCacheTTL = 5 * time.Minute

// DefaultHTTPTimeout 拉单个 JSONL 的最长容忍时间。
// 详情页在网关超时（约 60s）内必须返回，留 5s 余量给解析+写响应。
const DefaultHTTPTimeout = 55 * time.Second

// MaxCacheEntries LRU 条数上限（兜底），超过后淘汰最久未访问的。
const MaxCacheEntries = 200

// MaxCacheBytes 进程内缓存总字节上限。
//
// 关键修复：旧实现只按"条数"封顶（200 条），完全不看每条多大，
// 200 条 × 大文件可达数 GB，是 Pod OOM 的头号根因。
//
// 方案 C：详情页的持久缓存由 DB 的 bundle_json 承担（重复访问直接读库、
// 不进内存、不下载），因此进程内缓存只需当"小而快的临时缓冲"，
// 上限刻意取保守值，给运行时与并发请求峰值留足余量（Pod 仅 4GB）。
const MaxCacheBytes = 512 * 1024 * 1024

// MaxDownloadConcurrency 全局下载/解析并发上限，削掉"列表预热 + 详情拉取"
// 多个大文件同时流式解析造成的瞬时尖峰。
const MaxDownloadConcurrency = 3

// cacheEntry 缓存中的一条记录。size 为该条占用的近似字节数（取原始文件字节，
// 作为解析结果内存占用的低成本估算，用于按字节淘汰）。
type cacheEntry struct {
	url      string
	parsedAt time.Time
	result   *ParseResult
	size     int
}

// Fetcher 负责拉取并解析 TOS JSONL，带 TTL+LRU 内存缓存。
type Fetcher struct {
	client   *http.Client
	ttl      time.Duration
	max      int
	maxBytes int

	mu       sync.Mutex
	ll       *list.List               // 双向链表，头=最近，尾=最久
	index    map[string]*list.Element // url → 链表节点
	curBytes int                      // 当前缓存占用的近似总字节数

	// 防止同一 url 并发重复拉取（singleflight 简化版）
	inflight map[string]chan struct{}

	// 全局下载并发闸门，削掉多个大文件同时进内存的瞬时尖峰。
	dlSem chan struct{}
}

// NewFetcher 构造默认 fetcher。
func NewFetcher() *Fetcher {
	return &Fetcher{
		client:   &http.Client{Timeout: DefaultHTTPTimeout},
		ttl:      DefaultCacheTTL,
		max:      MaxCacheEntries,
		maxBytes: MaxCacheBytes,
		ll:       list.New(),
		index:    map[string]*list.Element{},
		inflight: map[string]chan struct{}{},
		dlSem:    make(chan struct{}, MaxDownloadConcurrency),
	}
}

// FetchAndParse 拉取 obj_url 的 JSONL 并解析；TTL 内命中缓存直接返回。
func (f *Fetcher) FetchAndParse(url string) (*ParseResult, error) {
	if url == "" {
		return nil, errors.New("empty url")
	}

	// 1) 命中缓存。
	if res := f.getFromCache(url); res != nil {
		return res, nil
	}

	// 2) 同一 url 并发去重：等待已有的 inflight 完成后再次查缓存。
	f.mu.Lock()
	if ch, ok := f.inflight[url]; ok {
		f.mu.Unlock()
		<-ch
		if res := f.getFromCache(url); res != nil {
			return res, nil
		}
		// 上一次失败了才会到这里，落到后续重试逻辑。
	} else {
		ch := make(chan struct{})
		f.inflight[url] = ch
		f.mu.Unlock()
		defer func() {
			f.mu.Lock()
			delete(f.inflight, url)
			f.mu.Unlock()
			close(ch)
		}()
	}

	// 3) miss：实拉 + 流式解析，不再把完整 JSONL 文件读入内存。
	res, size, err := f.downloadAndParse(url)
	if err != nil {
		return nil, err
	}

	f.putCache(url, res, size)
	return res, nil
}

// Prewarm 异步预热一批 url，命中已有缓存直接跳过。
// 列表页加载完后调用，提升点击详情页首屏速度。
func (f *Fetcher) Prewarm(urls []string) {
	for _, u := range urls {
		if u == "" {
			continue
		}
		if f.getFromCache(u) != nil {
			continue
		}
		go func(url string) {
			_, _ = f.FetchAndParse(url)
		}(u)
	}
}

// Invalidate 主动失效某个 url 的缓存。
func (f *Fetcher) Invalidate(url string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if el, ok := f.index[url]; ok {
		f.curBytes -= el.Value.(*cacheEntry).size
		if f.curBytes < 0 {
			f.curBytes = 0
		}
		f.ll.Remove(el)
		delete(f.index, url)
	}
}

// getFromCache 命中且未过期返回结果，并把节点移到链表头。
func (f *Fetcher) getFromCache(url string) *ParseResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	el, ok := f.index[url]
	if !ok {
		return nil
	}
	e := el.Value.(*cacheEntry)
	if time.Since(e.parsedAt) >= f.ttl {
		// 过期就清掉
		f.curBytes -= e.size
		if f.curBytes < 0 {
			f.curBytes = 0
		}
		f.ll.Remove(el)
		delete(f.index, url)
		return nil
	}
	f.ll.MoveToFront(el)
	return e.result
}

// putCache 写入缓存，按"总字节"与"条数"双上限淘汰最久未访问的。
// size 为该条的近似字节数（取原始文件字节）。
func (f *Fetcher) putCache(url string, res *ParseResult, size int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if el, ok := f.index[url]; ok {
		e := el.Value.(*cacheEntry)
		f.curBytes += size - e.size
		e.parsedAt = time.Now()
		e.result = res
		e.size = size
		f.ll.MoveToFront(el)
		f.evictLocked()
		return
	}
	el := f.ll.PushFront(&cacheEntry{url: url, parsedAt: time.Now(), result: res, size: size})
	f.index[url] = el
	f.curBytes += size
	f.evictLocked()
}

// evictLocked 在持锁状态下把缓存收敛到字节与条数双上限内，
// 从尾部（最久未访问）开始淘汰。至少保留一条，避免单条超限时被立刻清空。
func (f *Fetcher) evictLocked() {
	for f.ll.Len() > 1 && (f.curBytes > f.maxBytes || f.ll.Len() > f.max) {
		old := f.ll.Back()
		if old == nil {
			break
		}
		e := old.Value.(*cacheEntry)
		f.ll.Remove(old)
		delete(f.index, e.url)
		f.curBytes -= e.size
	}
	if f.curBytes < 0 {
		f.curBytes = 0
	}
}

func (f *Fetcher) downloadAndParse(url string) (*ParseResult, int, error) {
	// 全局并发闸门：限制同时下载/解析的大文件数量，削掉瞬时内存尖峰。
	f.dlSem <- struct{}{}
	defer func() { <-f.dlSem }()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	// 关键：声明支持 gzip，让 TOS 直接返回压缩流。
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, 0, fmt.Errorf("fetch %s: status=%d", url, resp.StatusCode)
	}

	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gr, gzErr := gzip.NewReader(resp.Body)
		if gzErr != nil {
			return nil, 0, fmt.Errorf("gzip reader: %w", gzErr)
		}
		defer gr.Close()
		reader = gr
	}

	res, size, err := ParseStream(reader)
	if err != nil {
		return nil, size, fmt.Errorf("parse stream %s after %d bytes: %w", url, size, err)
	}
	return res, size, nil
}
