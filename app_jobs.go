package main

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
)

// 后台任务（DSH session/jobs）缓存。
//
// DSH 通过 events.mux 推送 `session/jobs` 帧，payload = {type, sessionId, jobs:[...]}，
// 每个 job 含 {id, kind, label, status, detail, startedAt, finishedAt}——
// 这就是"本会话的后台任务及其状态"（pwsh / bash 等后台命令），比按进程名猜要准确得多。
//
// 该帧没有 `event` 字段，parseEventFrame 会返回 nil 丢弃（见 app_events.go 注释），
// 因此在 handleEventFrame 最前面单独截获并缓存，供「子代理」面板读取。

type dshJob struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Label      string `json:"label"`
	Status     string `json:"status"`
	Detail     string `json:"detail"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt"`
}

var (
	jobsCacheMu sync.Mutex
	jobsCache   = map[string][]dshJob{}   // sessionId → 最近一次 jobs 帧
	jobsSeenAt  = map[string]time.Time{}  // sessionId → 收到时间
)

// captureJobsFrame 截获 session/jobs 帧并缓存。返回 true 表示这是 jobs 帧（调用方无需再转发）。
func captureJobsFrame(data []byte) bool {
	var frame struct {
		Method  string `json:"method"`
		Payload struct {
			Type      string   `json:"type"`
			SessionID string   `json:"sessionId"`
			Jobs      []dshJob `json:"jobs"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		return false
	}
	if frame.Method != "session/jobs" && frame.Payload.Type != "session/jobs" {
		return false
	}
	if strings.TrimSpace(frame.Payload.SessionID) == "" {
		return false
	}
	jobsCacheMu.Lock()
	jobsCache[frame.Payload.SessionID] = frame.Payload.Jobs
	jobsSeenAt[frame.Payload.SessionID] = time.Now()
	jobsCacheMu.Unlock()
	return true
}

// cachedJobs 取某会话最近一次的后台任务列表（按开始时间倒序）。
func cachedJobs(sessionID string) []dshJob {
	jobsCacheMu.Lock()
	jobs := jobsCache[sessionID]
	jobsCacheMu.Unlock()
	out := make([]dshJob, len(jobs))
	copy(out, jobs)
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out
}

// jobsCacheAge 返回该会话 jobs 数据的年龄（未知返回 -1）。
func jobsCacheAge(sessionID string) time.Duration {
	jobsCacheMu.Lock()
	defer jobsCacheMu.Unlock()
	t, ok := jobsSeenAt[sessionID]
	if !ok {
		return -1
	}
	return time.Since(t)
}

// jobStatusLabel 把 DSH 的 status 归一为面板用标签（未知值原样保留，不臆造）。
func jobStatusLabel(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "done", "success", "succeeded":
		return "已完成"
	case "running", "active", "in_progress", "started":
		return "运行中"
	case "failed", "error":
		return "失败"
	case "cancelled", "canceled", "killed":
		return "已取消"
	case "queued", "pending", "created":
		return "未启动"
	case "":
		return "未知"
	default:
		return status
	}
}

// buildJobRows 组装面板行（附带归一状态、耗时、命令摘要）。
func buildJobRows(jobs []dshJob) []map[string]any {
	rows := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		durMs := int64(-1)
		if j.StartedAt > 0 {
			end := j.FinishedAt
			if end <= 0 {
				end = time.Now().UnixMilli()
			}
			durMs = end - j.StartedAt
		}
		rows = append(rows, map[string]any{
			"id":          j.ID,
			"kind":        j.Kind,
			"label":       j.Label,
			"status":      j.Status,
			"statusLabel": jobStatusLabel(j.Status),
			"detail":      j.Detail,
			"startedAt":   j.StartedAt,
			"finishedAt":  j.FinishedAt,
			"durationMs":  durMs,
			"running":     jobStatusLabel(j.Status) == "运行中",
		})
	}
	return rows
}

func countJobsRunning(jobs []dshJob) int {
	n := 0
	for _, j := range jobs {
		if jobStatusLabel(j.Status) == "运行中" {
			n++
		}
	}
	return n
}
