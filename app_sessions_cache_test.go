package main

import (
	"testing"
	"time"
)

// TestSessionsCacheInvalidation 锁住 fetchSessions 短期缓存的两个不变量。
//
// 为什么重要：给 fetchSessions 加 TTL 缓存是为了修"切换审批模式按钮要等 1~3.6s"
// （MetaForTab/ContextUsageForTab 都走 findSession→fetchSessions→session.list）。
// 但缓存必须有确定性的失效路径，否则会出现"新建/重命名/归档会话后列表不更新"这类脏读。
// 这里断言：会话写操作走的统一入口 invalidateTabsCache() 会同时清掉两个缓存。
func TestSessionsCacheInvalidation(t *testing.T) {
	seedSessionsCache([]dshSession{{SessionID: "session-cache-test"}})
	seedTabsCache([]any{map[string]any{"id": "session-cache-test"}})

	if got := len(cachedSessionsSnapshot()); got != 1 {
		t.Fatalf("前置失败：会话缓存未种入（got %d）", got)
	}

	invalidateTabsCache()

	if got := cachedSessionsSnapshot(); got != nil {
		t.Errorf("invalidateTabsCache 后会话缓存应为空，实际 %v", got)
	}
	tabsCacheMu.Lock()
	tabs := tabsCache
	tabsCacheMu.Unlock()
	if tabs != nil {
		t.Errorf("invalidateTabsCache 后 tabs 缓存应为空，实际 %v", tabs)
	}
}

// TestSessionsCacheTTL 断言 TTL 语义：未过期命中缓存，过期后不再命中。
func TestSessionsCacheTTL(t *testing.T) {
	seedSessionsCache([]dshSession{{SessionID: "session-ttl-test"}})
	if got := len(cachedSessionsSnapshot()); got != 1 {
		t.Fatalf("前置失败：会话缓存未种入")
	}

	// 未过期：应命中缓存（这里直接读快照来验证 TTL 判断本身）
	if !sessionsCacheFresh() {
		t.Error("刚写入的缓存应被视为新鲜")
	}

	// 手工把时间戳推到 TTL 之前 → 视为过期
	sessionsCacheMu.Lock()
	sessionsCacheAt = time.Now().Add(-sessionsCacheTTL - time.Second)
	sessionsCacheMu.Unlock()
	if sessionsCacheFresh() {
		t.Error("超过 TTL 后不应再视为新鲜")
	}
	invalidateSessionsCache()
}

// seedSessionsCache 测试用：种入会话缓存。
func seedSessionsCache(items []dshSession) {
	sessionsCacheMu.Lock()
	sessionsCache = items
	sessionsCacheAt = time.Now()
	sessionsCacheMu.Unlock()
}

// seedTabsCache 测试用：种入 tabs 缓存。
func seedTabsCache(tabs []any) {
	tabsCacheMu.Lock()
	tabsCache = tabs
	tabsCacheAt = time.Now()
	tabsCacheMu.Unlock()
}

// cachedSessionsSnapshot 测试用：读会话缓存快照。
func cachedSessionsSnapshot() []dshSession {
	sessionsCacheMu.Lock()
	defer sessionsCacheMu.Unlock()
	return sessionsCache
}

// sessionsCacheFresh 测试用：判定缓存是否仍在 TTL 内。
func sessionsCacheFresh() bool {
	sessionsCacheMu.Lock()
	defer sessionsCacheMu.Unlock()
	return sessionsCache != nil && time.Since(sessionsCacheAt) < sessionsCacheTTL
}
