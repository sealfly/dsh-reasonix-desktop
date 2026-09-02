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
	"io"
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
	dshGHReleases = "https://api.github.com/repos/sdkwork-ai/deepseek-harness-desktop/releases/latest"
)

// dshVersionInfo 返回给前端的更新检测结果。
type dshVersionInfo struct {
	Current      string `json:"current"`   // 本地已装版本
	Latest       string `json:"latest"`    // 检测到的最新版（未知时 = current）
	Available    bool   `json:"available"` // 是否有新版
	Channel      string `json:"channel"`   // 更新渠道说明
	UpdateURL    string `json:"updateUrl"` // 更新渠道链接
	DownloadURL  string `json:"downloadUrl,omitempty"`  // Windows 安装包直链（一键下载更新）
	ReleaseURL   string `json:"releaseUrl,omitempty"`   // 最新 Release 详情页
	CheckedAt    int64  `json:"checkedAt"` // 检测时间（epoch ms）
	Error        string `json:"error,omitempty"`
}

// dshInstallRoot 定位 DSH 核心包 package.json（安装目录可被多路径探测）。
// 覆盖：官方桌面版(dependencies/dsh)、源码仓库(~/deepseek-harness)、
// npm 全局安装(安装包预装 @deepseek-ai/dsh 的落点，APPDATA/npm 与 ProgramFiles/nodejs)。
func dshInstallRoot() string {
	roots := []string{
		filepath.Join(os.Getenv("APPDATA"), "io.github.hairyf.deepseek-harness-desktop", "dependencies", "dsh"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "io.github.hairyf.deepseek-harness-desktop", "dependencies", "dsh"),
		// npm 全局安装（安装包预装路径）
		filepath.Join(os.Getenv("APPDATA"), "npm", "node_modules", "@deepseek-ai", "dsh"),
		filepath.Join(os.Getenv("ProgramFiles"), "nodejs", "node_modules", "@deepseek-ai", "dsh"),
		filepath.Join(os.Getenv("ProgramFiles"), "nodejs", "node_modules", "@deepseek-ai", "dsh"),
	}
	// 源码仓库运行（本机开发）：deepseek-harness 根目录自带 package.json。
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, "deepseek-harness"))
		// npm 全局(用户级, pnpm/nvm 布局)
		roots = append(roots, filepath.Join(home, "AppData", "Roaming", "npm", "node_modules", "@deepseek-ai", "dsh"))
	}
	seen := map[string]bool{}
	for _, r := range roots {
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		if _, err := os.Stat(filepath.Join(r, "package.json")); err == nil {
			return r
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
	// 先试独立 @deepseek-ai/dsh 包（桌面版布局），再试源码仓库根（root 即 DSH workspace）。
	rel := []string{dshCorePkgRel, "package.json"}
	for _, relPath := range rel {
		data, err := os.ReadFile(filepath.Join(root, relPath))
		if err != nil {
			continue
		}
		var pkg struct { Version string `json:"version"` }
		if json.Unmarshal(data, &pkg) != nil || pkg.Version == "" {
			continue
		}
		return strings.TrimSpace(pkg.Version)
	}
	return ""
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

// githubDesktopLatest 查 DeepSeek Harness Desktop 最新 Release：
// 返回 {tag, htmlURL, winDownloadURL}（Windows x64 安装包直链）。
// 失败返回空串，静默降级（更新提醒仍可用 npm 版本对比）。
func githubDesktopLatest() (tag, htmlURL, winURL string) {
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(dshGHReleases)
	if err != nil {
		return "", "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", ""
	}
	var rel struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if json.NewDecoder(resp.Body).Decode(&rel) != nil {
		return "", "", ""
	}
	for _, a := range rel.Assets {
		if strings.Contains(a.Name, "win-x64.exe") {
			return rel.TagName, rel.HTMLURL, a.BrowserDownloadURL
		}
	}
	return rel.TagName, rel.HTMLURL, ""
}

// DshUpdateCheck 检测 DSH 是否有新版（供前端注入脚本调用）。
func (a *App) DshUpdateCheck() map[string]any {
	info := dshVersionInfo{
		Current:   dshCoreVersion(),
		Channel:   "DeepSeek Harness Desktop (npm @deepseek-ai/dsh)",
		UpdateURL: dshUpdateURL,
		CheckedAt: time.Now().UnixMilli(),
	}
	// 更新通道实体：GitHub 最新 Release 的 Windows 安装包直链 + 详情页
	ghTag, ghURL, winURL := githubDesktopLatest()
	if ghURL != "" {
		info.ReleaseURL = ghURL
	}
	if winURL != "" {
		info.DownloadURL = winURL
	}
	if ghTag != "" {
		info.Channel = "DeepSeek Harness Desktop (release " + ghTag + ", npm @deepseek-ai/dsh)"
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
	if i.DownloadURL != "" {
		m["downloadUrl"] = i.DownloadURL
	}
	if i.ReleaseURL != "" {
		m["releaseUrl"] = i.ReleaseURL
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

// ---------- 设置页版本管理（DshVersionManage / DshDownloadVersion） ----------

// dshReleaseMeta 一条 GitHub Release 的展示信息。
type dshReleaseMeta struct {
	Tag      string `json:"tag"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Date     string `json:"date"`
	WinURL   string `json:"winUrl,omitempty"`
	Prerelease bool `json:"prerelease"`
}

// npmVersions 查 npm registry 的版本列表（含 dist-tags），失败返回空（静默降级）。
func npmVersions() (tags map[string]string, versions []string) {
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get("https://registry.npmjs.org/@deepseek-ai/dsh")
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}
	var doc struct {
		DistTags map[string]string            `json:"dist-tags"`
		Versions map[string]json.RawMessage   `json:"versions"`
	}
	if json.NewDecoder(resp.Body).Decode(&doc) != nil {
		return nil, nil
	}
	versions = make([]string, 0, len(doc.Versions))
	for v := range doc.Versions {
		versions = append(versions, v)
	}
	// 倒序（新→旧），截前 24 个
	sortVersionsDesc(versions)
	if len(versions) > 24 {
		versions = versions[:24]
	}
	return doc.DistTags, versions
}

// sortVersionsDesc 按版本号倒序（解析失败按字符串倒序）。
func sortVersionsDesc(vs []string) {
	less := func(a, b string) bool { return compareVersions(a, b) > 0 }
	// 简单插入排序（版本列表小）
	for i := 1; i < len(vs); i++ {
		for j := i; j > 0 && less(vs[j], vs[j-1]); j-- {
			vs[j], vs[j-1] = vs[j-1], vs[j]
		}
	}
}

// githubDesktopReleases 查 DeepSeek Harness Desktop 最近 Releases（per_page 个）。
func githubDesktopReleases(perPage int) []dshReleaseMeta {
	client := &http.Client{Timeout: 8 * time.Second}
	url := "https://api.github.com/repos/sdkwork-ai/deepseek-harness-desktop/releases?per_page=" + strconv.Itoa(perPage)
	resp, err := client.Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var rels []struct {
		TagName    string `json:"tag_name"`
		Name       string `json:"name"`
		HTMLURL    string `json:"html_url"`
		Published  string `json:"published_at"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if json.NewDecoder(resp.Body).Decode(&rels) != nil {
		return nil
	}
	out := make([]dshReleaseMeta, 0, len(rels))
	for _, r := range rels {
		m := dshReleaseMeta{Tag: r.TagName, Name: r.Name, URL: r.HTMLURL, Date: r.Published, Prerelease: r.Prerelease}
		for _, a := range r.Assets {
			if strings.Contains(a.Name, "win-x64.exe") {
				m.WinURL = a.BrowserDownloadURL
				break
			}
		}
		out = append(out, m)
	}
	return out
}

// DshVersionManage 返回设置页「DSH 核心版本」区块需要的完整数据。
// 失败字段一律空值/空列表（原则 3：兜底不崩溃，前端显示占位）。
func (a *App) DshVersionManage() map[string]any {
	m := map[string]any{
		"current":   dshCoreVersion(),
		"latest":    "",
		"available": false,
		"channel":   "DeepSeek Harness Desktop (npm @deepseek-ai/dsh)",
		"checkedAt": time.Now().UnixMilli(),
		"versions":  []any{},
		"releases":  []any{},
	}
	if m["current"] == "" {
		m["current"] = "unknown"
		m["error"] = "cannot locate local @deepseek-ai/dsh package.json"
		return m
	}
	tags, vers := npmVersions()
	if len(vers) > 0 {
		vs := make([]any, 0, len(vers))
		for _, v := range vers {
			vs = append(vs, v)
		}
		m["versions"] = vs
		if tags != nil {
			if l := tags["latest"]; l != "" {
				m["latest"] = l
				m["available"] = compareVersions(l, m["current"].(string)) > 0
			}
		}
	}
	rels := githubDesktopReleases(8)
	if len(rels) > 0 {
		rs := make([]any, 0, len(rels))
		for i := range rels {
			rs = append(rs, map[string]any{
				"tag":        rels[i].Tag,
				"name":       rels[i].Name,
				"url":        rels[i].URL,
				"date":       rels[i].Date,
				"winUrl":     rels[i].WinURL,
				"prerelease": rels[i].Prerelease,
			})
		}
		m["releases"] = rs
	}
	return m
}

// DshDownloadVersion 下载 DSH 安装包/核心包到 ~/.reasonix/downloads/。
// url 可以是 GitHub Release 的 win-x64.exe 直链或 npm tarball。
// 返回 {ok, path, size, fileName, error}。
func (a *App) DshDownloadVersion(url, fileName string) map[string]any {
	dir := filepath.Join(reasonixDataDir(), "downloads")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return map[string]any{"ok": false, "error": "cannot create downloads dir: " + err.Error()}
	}
	if fileName == "" {
		fileName = "dsh-download"
		// 从 URL 提取文件名
		if i := strings.LastIndexByte(url, '/'); i >= 0 && i+1 < len(url) {
			fileName = url[i+1:]
		}
	}
	// 文件名净化（防路径穿越）
	fileName = strings.Map(func(r rune) rune {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		return r
	}, fileName)
	target := filepath.Join(dir, fileName)

	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return map[string]any{"ok": false, "error": "download failed: " + err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return map[string]any{"ok": false, "error": "download failed: HTTP " + strconv.Itoa(resp.StatusCode)}
	}
	out, err := os.Create(target)
	if err != nil {
		return map[string]any{"ok": false, "error": "cannot create file: " + err.Error()}
	}
	n, err := io.Copy(out, resp.Body)
	out.Close()
	if err != nil {
		_ = os.Remove(target)
		return map[string]any{"ok": false, "error": "download interrupted: " + err.Error()}
	}
	return map[string]any{
		"ok":       true,
		"path":     target,
		"size":     n,
		"fileName": fileName,
	}
}
