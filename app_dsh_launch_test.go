package main

// app_dsh_launch_test.go — 锁住「启动 DSH」的修复（旧实现写死 3080、不查端口占用、不等待就绪）。

import (
	"bytes"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDshLaunchArgsUsesConfiguredPort(t *testing.T) {
	got := strings.Join(dshLaunchArgs("web", 3092), " ")
	want := "--profile web --no-open --port 3092"
	if got != want {
		t.Fatalf("启动参数应为 %q，实际 %q（旧实现写死 3080 且不带 --port）", want, got)
	}
	// 非默认端口必须体现出来
	if !strings.Contains(strings.Join(dshLaunchArgs("tauri", 4555), " "), "--port 4555") {
		t.Fatal("端口应取自配置")
	}
}

func TestDshLaunchProfilesDefaultAndOverride(t *testing.T) {
	t.Setenv("DSH_LAUNCH_PROFILES", "")
	got := dshLaunchProfiles()
	if len(got) != 2 || got[0] != "web" || got[1] != "tauri" {
		t.Fatalf("默认应先试 web 再兜底 tauri，实际 %v", got)
	}
	// web profile 缺 bundle 时需要靠 tauri 兜底（本机实测 dsh-tauri 死链）
	t.Setenv("DSH_LAUNCH_PROFILES", "web, tauri, headless")
	got = dshLaunchProfiles()
	if len(got) != 3 || got[2] != "headless" {
		t.Fatalf("环境变量应可覆盖（便于排障），实际 %v", got)
	}
}

func TestTcpPortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("无法起监听: %v", err)
	}
	defer ln.Close()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	if !tcpPortInUse("127.0.0.1", port) {
		t.Fatalf("已监听端口 %d 应判定为占用", port)
	}
	// 关掉后应判定为空闲（换一个几乎不可能被占的端口）
	ln.Close()
	time.Sleep(200 * time.Millisecond)
	if tcpPortInUse("127.0.0.1", 59321) {
		t.Fatal("空闲端口不应判定为占用")
	}
}

func TestErrTailKeepsLastLines(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("line1\nline2\nline3\nline4\nline5\nline6\n")
	got := errTail(&buf)
	if !strings.Contains(got, "line6") || strings.Contains(got, "line1") {
		t.Fatalf("应只保留末尾几行，实际 %q", got)
	}
	if errTail(&bytes.Buffer{}) != "" {
		t.Fatal("空 stderr 应返回空串")
	}
}

func TestLaunchDshAndWaitFailsFastOnBadExe(t *testing.T) {
	err := launchDshAndWait("definitely-not-a-real-exe-xyz", []string{"--profile", "web"}, "127.0.0.1", 59322, 2*time.Second)
	if err == nil {
		t.Fatal("不存在的可执行文件应报错")
	}
	if !strings.Contains(err.Error(), "启动失败") {
		t.Fatalf("错误信息应说明启动失败，实际 %v", err)
	}
}
