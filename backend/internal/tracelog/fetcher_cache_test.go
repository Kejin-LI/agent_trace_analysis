package tracelog

import (
	"strconv"
	"testing"
	"time"
)

// newTestFetcher 构造一个不依赖网络、便于设定上限的 fetcher，用于单测缓存淘汰逻辑。
func newTestFetcher(maxBytes, maxEntries int) *Fetcher {
	f := NewFetcher()
	f.maxBytes = maxBytes
	f.max = maxEntries
	return f
}

// TestPutCacheEvictsByBytes 锁定核心防 OOM 行为：缓存按总字节封顶淘汰，
// 而非只按条数。这是修复 OOM 的关键逻辑，回归必须保住。
func TestPutCacheEvictsByBytes(t *testing.T) {
	f := newTestFetcher(100, 1000) // 字节上限 100，条数上限很大，确保只由字节触发淘汰

	f.putCache("a", &ParseResult{}, 60)
	f.putCache("b", &ParseResult{}, 60) // a+b=120 > 100，应淘汰最久的 a

	if f.getFromCache("a") != nil {
		t.Fatalf("expected oldest entry 'a' to be evicted by byte cap")
	}
	if f.getFromCache("b") == nil {
		t.Fatalf("expected newest entry 'b' to remain")
	}
	if f.curBytes != 60 {
		t.Fatalf("expected curBytes=60 after eviction, got %d", f.curBytes)
	}
}

// TestPutCacheEvictsByEntries 字节充足时仍受条数上限约束，淘汰最久未访问的。
func TestPutCacheEvictsByEntries(t *testing.T) {
	f := newTestFetcher(1<<30, 2) // 字节几乎无限，条数上限 2

	f.putCache("a", &ParseResult{}, 1)
	f.putCache("b", &ParseResult{}, 1)
	f.putCache("c", &ParseResult{}, 1) // 超过 2 条，淘汰最久的 a

	if f.getFromCache("a") != nil {
		t.Fatalf("expected 'a' evicted by entry cap")
	}
	if f.getFromCache("b") == nil || f.getFromCache("c") == nil {
		t.Fatalf("expected 'b' and 'c' to remain")
	}
}

// TestPutCacheKeepsAtLeastOne 单条超过字节上限时，仍保留这一条（至少能用），
// 避免刚写入就被立即清空导致永远 miss。
func TestPutCacheKeepsAtLeastOne(t *testing.T) {
	f := newTestFetcher(100, 1000)

	f.putCache("big", &ParseResult{}, 500) // 单条 500 > 上限 100

	if f.getFromCache("big") == nil {
		t.Fatalf("expected single oversized entry to be retained")
	}
}

// TestGetFromCacheExpiryUpdatesBytes 命中过期条目时清理，并同步扣减 curBytes，
// 防止字节计数泄漏导致后续误淘汰。
func TestGetFromCacheExpiryUpdatesBytes(t *testing.T) {
	f := newTestFetcher(1000, 1000)
	f.ttl = time.Millisecond

	f.putCache("x", &ParseResult{}, 50)
	time.Sleep(5 * time.Millisecond)

	if f.getFromCache("x") != nil {
		t.Fatalf("expected expired entry to be a miss")
	}
	if f.curBytes != 0 {
		t.Fatalf("expected curBytes reset to 0 after expiry cleanup, got %d", f.curBytes)
	}
}

// TestInvalidateUpdatesBytes 主动失效时同步扣减 curBytes。
func TestInvalidateUpdatesBytes(t *testing.T) {
	f := newTestFetcher(1000, 1000)
	f.putCache("x", &ParseResult{}, 50)
	f.Invalidate("x")
	if f.curBytes != 0 {
		t.Fatalf("expected curBytes=0 after invalidate, got %d", f.curBytes)
	}
}

// TestPutCacheUpdateExistingAdjustsBytes 同 url 重复写入时，按差值更新字节计数，
// 而不是重复累加。
func TestPutCacheUpdateExistingAdjustsBytes(t *testing.T) {
	f := newTestFetcher(1000, 1000)
	f.putCache("x", &ParseResult{}, 30)
	f.putCache("x", &ParseResult{}, 80) // 同 url 更新，curBytes 应为 80 而非 110
	if f.curBytes != 80 {
		t.Fatalf("expected curBytes=80 after in-place update, got %d", f.curBytes)
	}
}

// TestByteEvictionUnderManyEntries 模拟大量大文件写入，验证总字节始终收敛在上限内。
func TestByteEvictionUnderManyEntries(t *testing.T) {
	f := newTestFetcher(1000, 1000)
	for i := 0; i < 100; i++ {
		f.putCache("u"+strconv.Itoa(i), &ParseResult{}, 200)
	}
	if f.curBytes > f.maxBytes {
		t.Fatalf("expected curBytes<=%d, got %d", f.maxBytes, f.curBytes)
	}
}
