package main

// app_dsh_update.go — DSH 更新检测桥（仿 Reasonix 前端"检测到新版时提醒用户"）。
//
// DSH 无版本 RPC（探测 version/dsh.version/system.info 全 404），按原则 1
// （展示与持久化适配）+ 原则 3（失败兜底不崩溃）：
//   - current：读 DSH 核心包 @deepseek-ai/dsh 的 package.json（本地安装目录）
//   - latest：查 npm registry（registry.npmjs.org/@deepseek-ai/dsh/latest），
//     失败降级为静默（available=false），不阻塞前端
//   - updateUrl：DeepSeek Harness Desktop 的 GitHub Releases（用户实际更新渠道）
//
// 前端注入脚本（index.html）启动后调用 DshUpdateCheck()，available=true 时
// 显示可关闭的更新横幅（与 Reasonix 自身更新提醒并列、互不干扰）。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DSH 核心包相对安装目录的路径（DeepSeek Harness Desktop 的 dependencies/dsh）。
const (
	dshCorePkgRel = "node_modules" + string(os.PathSeparator) + "@deepseek-ai" + string(os.PathSeparator) + "dsh" + string(os.PathSeparator) + "package.json"
	dshNpmLatest  = "https://registry.npmjs.org/@deepseek-ai/dsh/latest"
	dshUpdateURL  = "https://github.com/sdkwork-ai/deepseek-harness-desktop/releases"
)

// dshVersionInfo 返回给前端的更新检测结果。
type dshVersionInfo struct {
	Current   string `json:"current"`   // 本地已装版本
	Latest    string `json:"latest"`    // 检测到的最新版（未知时 = current）
	Available bool   `json:"available"` // 是否有新版
	Channel   string `json:"channel"`   // 更新渠道说明
	UpdateURL string `json:"updateUrl"` // 更新渠道链接
	CheckedAt int64  `json:"checkedAt"` // 检测时间（epoch ms）
	Error     string `json:"error,omitempty"`
}

// dshInstallRoot 定位 DSH 核心包 package.json（安装目录可被多路径探测）。
func dshInstallRoot() string {
	roots := []string{
		filepath.Join(os.Getenv("APPDATA"), "io.github.hairyf.deepseek-harness-desktop", "dependencies", "dsh"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "io.github.hairyf.deepseek-harness-desktop", "dependencies", "dsh"),
	}
	for _, r := range roots {
		if r != "" {
			if _, err := os.Stat(filepath.Join(r, "package.json")); err == nil {
				return r
			}
		}
	}
	return ""
}

// dshCoreVersion 读 DSH 核心包版本（失败返回空串，调用方兜底）。
func dshCoreVersion() string {
	root := dshInstallRoot()
	if root == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(root, dshCorePkgRel))
	if err != nil {
		return ""
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return ""
	}
	return strings.TrimSpace(pkg.Version)
}

// npmLatestVersion 查 npm registry 最新版（网络失败返回空串，静默降级）。
func npmLatestVersion() string {
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(dshNpmLatest)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if json.NewDecoder(resp.Body).Decode(&pkg) != nil {
		return ""
	}
	return strings.TrimSpace(pkg.Version)
}

// parsedVer 解析后的版本（数字部分 + 预发布标识）。
type parsedVer struct {
	nums []int
	pre  string
}

// parseVersion 解析 semver 风格版本（x.y.z[-pre[.n]]），失败返回 ok=false。
func parseVersion(v string) (parsedVer, bool) {
	s := strings.TrimSpace(v)
	var p parsedVer
	main := s
	if i := strings.IndexByte(s, '-'); i >= 0 {
		main = s[:i]
		p.pre = s[i+1:]
	}
	parts := strings.Split(main, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return p, false
	}
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return p, false
		}
		p.nums = append(p.nums, n)
	}
	return p, true
}

// compareVersions 比较两个版本串：-1 a<b、0 相等、1 a>b。
// 语义：数字部分先比；预发布版本 < 正式版本（0.1.1-rc.2 < 0.1.1）；
// 预发布内部按整段字符串比较（rc.2 < rc.3，rc.2 < rc.10 靠数字段比较）。
func compareVersions(a, b string) int {
	pa, oka := parseVersion(a)
	pb, okb := parseVersion(b)
	if !oka || !okb {
		// 解析失败退化为字符串比较（保证可判定）
		return strings.Compare(a, b)
	}
	for i := 0; i < len(pa.nums) || i < len(pb.nums); i++ {
		na, nb := 0, 0
		if i < len(pa.nums) {
			na = pa.nums[i]
		}
		if i < len(pb.nums) {
			nb = pb.nums[i]
		}
		if na != nb {
			if na < nb {
				return -1
			}
			return 1
		}
	}
	// 数字部分相等：预发布比较
	switch {
	case pa.pre == "" && pb.pre == "":
		return 0
	case pa.pre == "":
		return 1 // 正式 > 预发布
	case pb.pre == "":
		return -1
	default:
		return strings.Compare(pa.pre, pb.pre)
	}
}

// DshUpdateCheck 检测 DSH 是否有新版（供前端注入脚本调用）。
func (a *App) DshUpdateCheck() map[string]any {
	info := dshVersionInfo{
		Current:   dshCoreVersion(),
		Channel:   "DeepSeek Harness Desktop (npm @deepseek-ai/dsh)",
		UpdateURL: dshUpdateURL,
		CheckedAt: time.Now().UnixMilli(),
	}
	if info.Current == "" {
		info.Current = "unknown"
		info.Error = "cannot locate local @deepseek-ai/dsh package.json"
		return dshInfoMap(info)
	}
	info.Latest = npmLatestVersion()
	if info.Latest == "" {
		info.Latest = info.Current
		info.Error = "update check unavailable (network/npm)"
		return dshInfoMap(info)
	}
	info.Available = compareVersions(info.Latest, info.Current) > 0
	return dshInfoMap(info)
}

// dshInfoMap 转成前端 map。
func dshInfoMap(i dshVersionInfo) map[string]any {
	m := map[string]any{
		"current":   i.Current,
		"latest":    i.Latest,
		"available": i.Available,
		"channel":   i.Channel,
		"updateUrl": i.UpdateURL,
		"checkedAt": i.CheckedAt,
	}
	if i.Error != "" {
		m["error"] = i.Error
	}
	return m
}

// dshUpdateBannerHTML 供注入脚本使用的横幅模板信息（便于单点维护文案）。
func dshUpdateBannerText(current, latest string) string {
	return fmt.Sprintf("DSH 有新版可用：%s → %s（点击查看更新渠道）", current, latest)
}
