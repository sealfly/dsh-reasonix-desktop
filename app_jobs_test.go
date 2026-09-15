package main

import (
	"testing"
	"time"
)

func TestJobStatusLabel(t *testing.T) {
	cases := map[string]string{
		"completed":  "已完成",
		"done":       "已完成",
		"success":    "已完成",
		"running":    "运行中",
		"active":     "运行中",
		"in_progress": "运行中",
		"failed":     "失败",
		"error":      "失败",
		"cancelled":  "已取消",
		"killed":     "已取消",
		"queued":     "未启动",
		"pending":    "未启动",
		"":           "未知",
		"weird-state": "weird-state", // 未知值原样保留，不臆造
	}
	for in, want := range cases {
		if got := jobStatusLabel(in); got != want {
			t.Fatalf("jobStatusLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildJobRows(t *testing.T) {
	jobs := []dshJob{
		{ID: "pwsh-1", Kind: "pwsh", Label: "echo hi", Status: "completed", Detail: "exit code: 0", StartedAt: 1000, FinishedAt: 3500},
		{ID: "pwsh-2", Kind: "pwsh", Label: "long task", Status: "running", StartedAt: 5000},
	}
	rows := buildJobRows(jobs)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows got %d", len(rows))
	}
	if rows[0]["statusLabel"] != "已完成" || rows[0]["running"] != false {
		t.Fatalf("row0 wrong: %v", rows[0])
	}
	if rows[0]["durationMs"] != int64(2500) {
		t.Fatalf("row0 duration want 2500 got %v", rows[0]["durationMs"])
	}
	if rows[1]["statusLabel"] != "运行中" || rows[1]["running"] != true {
		t.Fatalf("row1 should be running: %v", rows[1])
	}
	// running 任务的耗时按"到现在"计（>0 且不等于 -1）
	if d, ok := rows[1]["durationMs"].(int64); !ok || d <= 0 {
		t.Fatalf("row1 duration should be positive, got %v", rows[1]["durationMs"])
	}
	if got := countJobsRunning(jobs); got != 1 {
		t.Fatalf("countJobsRunning want 1 got %d", got)
	}
}

func TestCaptureJobsFrame(t *testing.T) {
	jobsCacheMu.Lock()
	jobsCache = map[string][]dshJob{}
	jobsSeenAt = map[string]time.Time{}
	jobsCacheMu.Unlock()

	jobsFrame := []byte(`{"method":"session/jobs","payload":{"type":"session/jobs","sessionId":"session-abc","jobs":[{"id":"pwsh-1","kind":"pwsh","label":"x","status":"completed","startedAt":10,"finishedAt":20}]}}`)
	if !captureJobsFrame(jobsFrame) {
		t.Fatal("jobs frame should be captured")
	}
	got := cachedJobs("session-abc")
	if len(got) != 1 || got[0].ID != "pwsh-1" || got[0].Status != "completed" {
		t.Fatalf("cached jobs wrong: %+v", got)
	}
	if age := jobsCacheAge("session-abc"); age < 0 {
		t.Fatalf("age should be >= 0, got %v", age)
	}

	// 非 jobs 帧 → 不拦截
	if captureJobsFrame([]byte(`{"method":"session/event","payload":{"sessionId":"s","event":{"type":"turn/start"}}}`)) {
		t.Fatal("non-jobs frame must not be captured")
	}
	// 缺 sessionId → 不缓存
	if captureJobsFrame([]byte(`{"method":"session/jobs","payload":{"jobs":[]}}`)) {
		t.Fatal("jobs frame without sessionId must be rejected")
	}
	// 坏 JSON → 不 panic、返回 false
	if captureJobsFrame([]byte(`{not json`)) {
		t.Fatal("bad json must return false")
	}
}

func TestCachedJobsOrder(t *testing.T) {
	jobsCacheMu.Lock()
	jobsCache = map[string][]dshJob{"s1": {
		{ID: "old", StartedAt: 100},
		{ID: "new", StartedAt: 900},
		{ID: "mid", StartedAt: 500},
	}}
	jobsSeenAt = map[string]time.Time{}
	jobsCacheMu.Unlock()

	got := cachedJobs("s1")
	if len(got) != 3 || got[0].ID != "new" || got[2].ID != "old" {
		t.Fatalf("jobs should be newest-first: %+v", got)
	}
	if n := len(cachedJobs("unknown-session")); n != 0 {
		t.Fatalf("unknown session should be empty, got %d", n)
	}
}
