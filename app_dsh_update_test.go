package main

// app_dsh_update_test.go — DSH 更新检测桥测试（版本解析/比较 + 检测结果结构）。

import "testing"

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in      string
		ok      bool
		numsLen int
	}{
		{"0.1.1-rc.2", true, 3},
		{"0.1.1", true, 3},
		{"1.2", true, 2},
		{"1.2.3-alpha.1", true, 3},
		{"abc", false, 0},
		{"1.x.2", false, 0},
		{"", false, 0},
	}
	for _, c := range cases {
		p, ok := parseVersion(c.in)
		if ok != c.ok {
			t.Errorf("parseVersion(%q) ok=%v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && len(p.nums) != c.numsLen {
			t.Errorf("parseVersion(%q) nums=%v, want %d 段", c.in, p.nums, c.numsLen)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.1-rc.2", "0.1.1-rc.2", 0},
		{"0.1.1-rc.3", "0.1.1-rc.2", 1},
		{"0.1.1-rc.2", "0.1.1-rc.3", -1},
		{"0.1.1-rc.2", "0.1.1", -1}, // 预发布 < 正式
		{"0.1.1", "0.1.1-rc.2", 1},
		{"0.1.1", "0.1.1", 0},
		{"0.2.0", "0.1.9", 1},
		{"0.1.10", "0.1.9", 1}, // 数字比较，非字符串比较
		{"1.0.0", "0.9.9", 1},
		{"1.2", "1.2.0", 0}, // 段数不同按缺失为 0
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestDshUpdateCheckStructure(t *testing.T) {
	a := &App{}
	info := a.DshUpdateCheck()
	if info == nil {
		t.Fatal("DshUpdateCheck 不应返回 nil")
	}
	// 结构字段齐全（前端注入脚本依赖）
	for _, k := range []string{"current", "latest", "available", "channel", "updateUrl", "checkedAt"} {
		if _, ok := info[k]; !ok {
			t.Errorf("缺少字段 %q: %v", k, info)
		}
	}
	cur, _ := info["current"].(string)
	lat, _ := info["latest"].(string)
	if cur == "" || lat == "" {
		t.Fatalf("版本字段不应为空: %v", info)
	}
	// available 必须与比较结果一致（本地版本 >= latest 时不应提示更新）
	if av, _ := info["available"].(bool); av && compareVersions(lat, cur) <= 0 {
		t.Fatalf("available 与版本比较矛盾: %v", info)
	}
	// 更新渠道固定
	if info["updateUrl"] != dshUpdateURL {
		t.Fatalf("updateUrl 应为 DSH 更新渠道: %v", info["updateUrl"])
	}
}

func TestDshUpdateCheckLocatesLocalVersion(t *testing.T) {
	a := &App{}
	info := a.DshUpdateCheck()
	cur := info["current"].(string)
	if cur == "unknown" {
		t.Fatal("应定位到本地 DSH 核心包版本（本机已安装）")
	}
	// 本地版本应为合法版本串
	if _, ok := parseVersion(cur); !ok {
		t.Fatalf("本地版本 %q 解析失败", cur)
	}
}

func TestDshUpdateBannerText(t *testing.T) {
	s := dshUpdateBannerText("0.1.1-rc.2", "0.1.1-rc.3")
	if s == "" || !contains(s, "0.1.1-rc.2") || !contains(s, "0.1.1-rc.3") {
		t.Fatalf("横幅文案异常: %q", s)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
