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
	"log"
	"net/http"
	"sync"
	"time"
)

// DefaultCacheTTL 默认缓存有效期。
const DefaultCacheTTL = 5 * time.Minute

// DefaultHTTPTimeout 拉单个 JSONL 的最长容忍时间。
// 详情页在网关超时（约 60s）内必须返回，留 5s 余量给解析+写响应。
const DefaultHTTPTimeout = 55 * time.Second

// MaxJSONLBytes 单文件最多读取 128MB。线上存在 600MB~2GB 的超大 session，
// 整文件读入会撑爆内存（OOM）也下不完。超过上限时优雅截断：保留已读部分，
// 解析器对截断的数组尾部可容忍（只丢最后一个不完整事件），详情页展示前半段
// 对话，远好于一直转圈 / 502。
const MaxJSONLBytes = 128 * 1024 * 1024

// MaxCacheEntries LRU 容量上限，超过后淘汰最久未访问的。
const MaxCacheEntries = 200

// cacheEntry 缓存中的一条记录。
type cacheEntry struct {
	url      string
	parsedAt time.Time
	result   *ParseResult
}

// Fetcher 负责拉取并解析 TOS JSONL，带 TTL+LRU 内存缓存。
type Fetcher struct {
	client *http.Client
	ttl    time.Duration
	max    int

	mu    sync.Mutex
	ll    *list.List               // 双向链表，头=最近，尾=最久
	index map[string]*list.Element // url → 链表节点

	// 防止同一 url 并发重复拉取（singleflight 简化版）
	inflight map[string]chan struct{}
}

// NewFetcher 构造默认 fetcher。
func NewFetcher() *Fetcher {
	return &Fetcher{
		client:   &http.Client{Timeout: DefaultHTTPTimeout},
		ttl:      DefaultCacheTTL,
		max:      MaxCacheEntries,
		ll:       list.New(),
		index:    map[string]*list.Element{},
		inflight: map[string]chan struct{}{},
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

	// 3) miss：实拉。
	body, err := f.download(url)
	if err != nil {
		return nil, err
	}
	res, err := Parse(body)
	if err != nil {
		return nil, err
	}

	f.putCache(url, res)
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
		f.ll.Remove(el)
		delete(f.index, url)
		return nil
	}
	f.ll.MoveToFront(el)
	return e.result
}

// putCache 写入缓存，超容量则淘汰最久未访问的。
func (f *Fetcher) putCache(url string, res *ParseResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if el, ok := f.index[url]; ok {
		el.Value.(*cacheEntry).parsedAt = time.Now()
		el.Value.(*cacheEntry).result = res
		f.ll.MoveToFront(el)
		return
	}
	el := f.ll.PushFront(&cacheEntry{url: url, parsedAt: time.Now(), result: res})
	f.index[url] = el
	for f.ll.Len() > f.max {
		old := f.ll.Back()
		if old == nil {
			break
		}
		f.ll.Remove(old)
		delete(f.index, old.Value.(*cacheEntry).url)
	}
}

func (f *Fetcher) download(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// 关键：声明支持 gzip，让 TOS 直接返回压缩流。
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("fetch %s: status=%d", url, resp.StatusCode)
	}

	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gr, gzErr := gzip.NewReader(resp.Body)
		if gzErr != nil {
			return nil, fmt.Errorf("gzip reader: %w", gzErr)
		}
		defer gr.Close()
		reader = gr
	}

	// 优雅截断：最多读 MaxJSONLBytes。超大文件保留已读部分交给解析器，
	// 而不是整文件读入内存（OOM）或直接报错（详情页转圈）。
	body, err := io.ReadAll(io.LimitReader(reader, MaxJSONLBytes))
	if err != nil {
		// 已读到内容时不致命：截断后仍可解析出前半段对话。
		if len(body) == 0 {
			return nil, fmt.Errorf("read body: %w", err)
		}
		log.Printf("tracelog: read %s truncated after %d bytes: %v", url, len(body), err)
	}
	return body, nil
}
