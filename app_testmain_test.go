package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// app_testmain_test.go — 测试进程的全局安全守卫。
//
// 事故背景（2026-09-18）：一组 MCP 测试断言的是"写本地 JSON"的旧契约、又没有隔离 DSH_HOME，
// 于是它们把 MCP 服务器条目写进了**用户真实的** ~/.dsh/profiles/web/cordis.patch.yml
// （真实配置被塞进 gh / filesystem / srv / b 四条测试数据；靠写入器留下的
// .bak-dsh-mcp-* 备份才恢复原状）。
//
// 教训：任何"会写到用户配置目录"的桥代码，其测试都必须隔离 home；但只依赖每个测试自觉
// 不可靠 —— 这里用 TestMain 做**兜底**：测试进程启动时若 DSH_HOME 未设置，就指向一个临时目录。
// 想跑真机集成测试的用例请显式设置 DSH_HOME（例如 DSH_LIVE_TEST=1 的场景）。
func TestMain(m *testing.M) {
	if os.Getenv("DSH_HOME") == "" {
		dir, err := os.MkdirTemp("", "dsh-test-home-")
		if err == nil {
			_ = os.MkdirAll(filepath.Join(dir, "profiles", "web"), 0o755)
			_ = os.Setenv("DSH_HOME", dir)
			defer os.RemoveAll(dir)
		} else {
			// 拿不到临时目录时宁可让测试失败，也不要落到真实 ~/.dsh
			fmt.Fprintln(os.Stderr, "测试守卫：无法创建隔离 DSH_HOME，拒绝运行以避免污染真实 DSH 配置")
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}
