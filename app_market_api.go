// app_market_api.go — 插件市场动态源（imsai / deepseek1024 公共 API）+ 离线回退。
//
// 数据流：PluginMarket / MarketPage 优先调 imsai API（联网最新目录），
// 失败/超时/离线模式（DSH_MARKET_OFFLINE=1）时回退 go:embed 的 registry.json（2961 条离线兜底）。
// 端点（见 https://github.com/imsai-sh/dsh-1024store/blob/main/web/docs/api.md）：
//   - 搜索: api.deepseek1024.com/v1/plugins/search?q=&category=&limit=&page=  （q 必填，全量目录，分页）
//   - 浏览: deepseek1024.com/api/v1/plugins?category=                       （至多 500 条 npm 可安装，stars 排序）
// 匿名限流 50 次/天、10 次/分。分页缓存策略（减少 API 调用）：
//   - 浏览：500 条快照按分类缓存 5 分钟，翻页纯本地切页（零额外调用）
//   - 搜索：每页结果缓存 5 分钟，翻回已加载页不重调
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	imsaiSearchBase = "https://api.deepseek1024.com/v1/plugins/search"
	imsaiListBase   = "https://deepseek1024.com/api/v1/plugins"
	imsaiTimeout    = 6 * time.Second
	imsaiCacheTTL   = 5 * time.Minute
	marketPageSize  = 100
)

// imsaiPlugin imsai 返回的插件条目（search 与 list 共用字段子集）。
type imsaiPlugin struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Owner       string `json:"owner"`
	URL         string `json:"url"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Install     string `json:"install"`
	Stars       int    `json:"stars"`
}

type imsaiSearchResp struct {
	Total      int           `json:"total"`
	TotalPages int           `json:"totalPages"`
	Results    []imsaiPlugin `json:"results"`
}

type imsaiCat struct {
	ID string `json:"id"`
	En string `json:"en"`
	Zh string `json:"zh"`
}

type imsaiListResp struct {
	Packages   []imsaiPlugin `json:"packages"`
	Categories []imsaiCat    `json:"categories"`
	Meta       struct {
		Total        int `json:"total"`
		CatalogTotal int `json:"catalogTotal"`
	} `json:"meta"`
}

// 浏览缓存：分类 → 500 条快照（TTL 5 分钟）。
type listCacheEntry struct {
	result map[string]any
	at     time.Time
}

var (
	marketCacheMu sync.Mutex
	marketCache   = map[string]listCacheEntry{}
)

// 搜索分页缓存：key=query|category|page → 结果（TTL 5 分钟，上限防膨胀）。
type pageCacheEntry struct {
	result map[string]any
	at     time.Time
}

var (
	searchPageCacheMu sync.Mutex
	searchPageCache   = map[string]pageCacheEntry{}
)

// dynamicMarket 尝试 imsai 动态源；失败返回 ok=false（调用方回退离线 embed）。
func dynamicMarket(query, category string) (map[string]any, bool) {
	if os.Getenv("DSH_MARKET_OFFLINE") == "1" {
		return nil, false
	}
	client := &http.Client{Timeout: imsaiTimeout}
	if strings.TrimSpace(query) != "" {
		return dynamicSearch(client, query, category)
	}
	return dynamicList(client, category)
}

// dynamicSearch 关键词搜索（q 必填，实时最新，取第一页 100 条）。
func dynamicSearch(client *http.Client, query, category string) (map[string]any, bool) {
	u, err := url.Parse(imsaiSearchBase)
	if err != nil {
		return nil, false
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("limit", "100")
	if category != "" {
		q.Set("category", category)
	}
	u.RawQuery = q.Encode()
	var resp imsaiSearchResp
	if err := imsaiGet(client, u.String(), &resp); err != nil {
		return nil, false
	}
	items := []any{}
	for _, p := range resp.Results {
		items = append(items, imsaiToItem(p))
	}
	return marketResult(items, resp.Total, []any{}), true
}

// dynamicList 浏览（空查询）：500 条 npm 可安装，按分类缓存 5 分钟。
func dynamicList(client *http.Client, category string) (map[string]any, bool) {
	marketCacheMu.Lock()
	if e, ok := marketCache[category]; ok && time.Since(e.at) < imsaiCacheTTL {
		marketCacheMu.Unlock()
		return e.result, true
	}
	marketCacheMu.Unlock()

	u := imsaiListBase
	if category != "" {
		u += "?category=" + url.QueryEscape(category)
	}
	var resp imsaiListResp
	if err := imsaiGet(client, u, &resp); err != nil {
		return nil, false
	}
	items := []any{}
	for _, p := range resp.Packages {
		items = append(items, imsaiToItem(p))
	}
	cats := []any{}
	for _, c := range resp.Categories {
		cats = append(cats, map[string]any{"id": c.ID, "en": c.En, "zh": c.Zh})
	}
	result := marketResult(items, resp.Meta.CatalogTotal, cats)
	marketCacheMu.Lock()
	marketCache[category] = listCacheEntry{result: result, at: time.Now()}
	marketCacheMu.Unlock()
	return result, true
}

// MarketPage 分页市场查询（imsai 动态源 + 分页缓存，减少 API 调用）。
// page 从 1 开始。返回 {items, total, page, hasMore, source}。
//   - 搜索（query 非空）：search API 分页（limit=100），每页缓存 5 分钟——翻回已加载页零调用
//   - 浏览（query 空）：500 条快照缓存后本地切页——翻页零 API 调用
//   - 失败/离线：回退 embed 2961 本地分页
func (a *App) MarketPage(query, category string, page int) map[string]any {
	if page < 1 {
		page = 1
	}
	if strings.TrimSpace(query) != "" {
		if r, ok := dynamicSearchPage(query, category, page); ok {
			return r
		}
	}
	if r, ok := dynamicListPage(category, page); ok {
		return r
	}
	return embedMarketPage(query, category, page)
}

// dynamicSearchPage 搜索分页（页结果缓存）。
func dynamicSearchPage(query, category string, page int) (map[string]any, bool) {
	if os.Getenv("DSH_MARKET_OFFLINE") == "1" {
		return nil, false
	}
	key := strings.ToLower(strings.TrimSpace(query)) + "|" + category + "|" + fmt.Sprint(page)
	searchPageCacheMu.Lock()
	if e, ok := searchPageCache[key]; ok && time.Since(e.at) < imsaiCacheTTL {
		searchPageCacheMu.Unlock()
		return e.result, true
	}
	searchPageCacheMu.Unlock()

	client := &http.Client{Timeout: imsaiTimeout}
	u, err := url.Parse(imsaiSearchBase)
	if err != nil {
		return nil, false
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("limit", fmt.Sprint(marketPageSize))
	q.Set("page", fmt.Sprint(page))
	if category != "" {
		q.Set("category", category)
	}
	u.RawQuery = q.Encode()
	var resp imsaiSearchResp
	if err := imsaiGet(client, u.String(), &resp); err != nil {
		return nil, false
	}
	items := []any{}
	for _, p := range resp.Results {
		items = append(items, imsaiToItem(p))
	}
	r := marketPageResult(items, resp.Total, page, page*marketPageSize < resp.Total)
	if resp.TotalPages > 0 {
		r["totalPages"] = resp.TotalPages
	}
	searchPageCacheMu.Lock()
	searchPageCache[key] = pageCacheEntry{result: r, at: time.Now()}
	searchPageCacheMu.Unlock()
	return r, true
}

// dynamicListPage 浏览分页：500 条快照缓存后本地切页（翻页零 API 调用）。
func dynamicListPage(category string, page int) (map[string]any, bool) {
	client := &http.Client{Timeout: imsaiTimeout}
	full, ok := dynamicList(client, category)
	if !ok {
		return nil, false
	}
	items, _ := full["items"].([]any)
	start := (page - 1) * marketPageSize
	if start >= len(items) {
		start = len(items)
	}
	end := start + marketPageSize
	if end > len(items) {
		end = len(items)
	}
	return marketPageResult(items[start:end], len(items), page, end < len(items)), true
}

// embedMarketPage 离线回退：embed registry 本地分页。
func embedMarketPage(query, category string, page int) map[string]any {
	reg := getPluginManager().loadMarket()
	all := []any{}
	q := strings.ToLower(strings.TrimSpace(query))
	for _, p := range reg.Plugins {
		if category != "" && p.Category != category {
			continue
		}
		if q != "" {
			hay := strings.ToLower(p.Name + " " + p.Owner + " " + p.En + " " + p.Zh + " " + p.URL)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		all = append(all, map[string]any{
			"name": p.Name, "owner": p.Owner, "url": p.URL,
			"category": p.Category, "description": p.En, "descriptionZh": p.Zh,
			"stars": p.Stars, "install": p.Install, "npm": p.Npm,
			"risk": riskLevel(p.En + " " + p.Name),
		})
	}
	start := (page - 1) * marketPageSize
	if start >= len(all) {
		start = len(all)
	}
	end := start + marketPageSize
	if end > len(all) {
		end = len(all)
	}
	return marketPageResult(all[start:end], len(all), page, end < len(all))
}

// marketPageResult 组装分页返回结构（含 totalPages 供前端页码式翻页）。
func marketPageResult(items []any, total, page int, hasMore bool) map[string]any {
	tp := (total + marketPageSize - 1) / marketPageSize
	if tp < 1 {
		tp = 1
	}
	return map[string]any{
		"items": items, "count": len(items), "total": total,
		"page": page, "hasMore": hasMore, "totalPages": tp,
		"updated": time.Now().Format("2006-01-02"),
		"source":  "imsai",
	}
}

// marketResult 组装统一返回结构（兼容旧 PluginMarket 契约）。
func marketResult(items []any, total int, cats []any) map[string]any {
	return map[string]any{
		"items": items, "count": len(items), "total": total,
		"updated": time.Now().Format("2006-01-02"), "source": "imsai",
		"categories": cats,
	}
}

func imsaiGet(client *http.Client, url string, out any) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "dsh-reasonix-market/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("imsai HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// imsaiToItem imsai 条目 → 前端插件视图（含风险分级）。
func imsaiToItem(p imsaiPlugin) map[string]any {
	name := p.Name
	if name == "" {
		name = p.ID
	}
	return map[string]any{
		"name": name, "owner": p.Owner, "url": p.URL,
		"category": p.Category, "description": p.Description,
		"descriptionZh": "", "stars": p.Stars, "install": p.Install,
		"npm": "", "risk": riskLevel(p.Description + " " + name),
	}
}

// riskLevel 启发式风险分级（low/medium/high），供 UI 展示风险标签。
func riskLevel(text string) string {
	s := strings.ToLower(text)
	switch {
	case containsAny(s, "root", "admin", "sudo", "shutdown", "rm -rf", "credential", "password", "secret", "exfiltrat", "提权", "凭据", "密码", "删除", "关机"):
		return "high"
	case containsAny(s, "http", "fetch", "network", "remote", "ssh", "webhook", "api key", "upload", "网络", "远程", "上传"):
		return "medium"
	default:
		return "low"
	}
}

func containsAny(s string, kws ...string) bool {
	for _, k := range kws {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// ===== 插件详情（GitHub README 功能介绍 + 预览图）=====

const detailCacheTTL = 10 * time.Minute

type detailCacheEntry struct {
	result map[string]any
	at     time.Time
}

var (
	detailCacheMu sync.Mutex
	detailCache   = map[string]detailCacheEntry{}
)

// MarketPluginDetail 插件详情：拉取 GitHub README（功能介绍）+ 提取预览图。
// url 为插件 GitHub 地址。按需调用（前端点击"详情"才拉取），走 GitHub raw 直连
// （对 imsai 限流零影响），结果缓存 10 分钟。失败返回 {error}（前端降级显示安装命令）。
func (a *App) MarketPluginDetail(url string) map[string]any {
	owner, repo := repoFromURL(url)
	if owner == "" || repo == "" {
		return map[string]any{"error": "无效的仓库地址", "url": url}
	}
	if os.Getenv("DSH_MARKET_OFFLINE") == "1" {
		return map[string]any{"error": "离线模式", "url": url, "owner": owner, "repo": repo}
	}
	key := owner + "/" + repo
	detailCacheMu.Lock()
	if e, ok := detailCache[key]; ok && time.Since(e.at) < detailCacheTTL {
		detailCacheMu.Unlock()
		return e.result
	}
	detailCacheMu.Unlock()

	result := map[string]any{"url": url, "owner": owner, "repo": repo, "image": ""}
	client := &http.Client{Timeout: 8 * time.Second}
	readme := ""
	rawURL := "https://raw.githubusercontent.com/" + key + "/HEAD/README.md"
	if body, err := httpGetBody(client, rawURL); err == nil {
		readme = body
	} else {
		for _, br := range []string{"main", "master"} {
			u2 := "https://raw.githubusercontent.com/" + key + "/" + br + "/README.md"
			if body, err := httpGetBody(client, u2); err == nil {
				readme = body
				break
			}
		}
	}
	if img := extractFirstImage(readme, owner, repo); img != "" {
		result["image"] = img
	} else {
		// GitHub 仓库 social preview（og:image）——前端加载失败会自动隐藏
		result["image"] = "https://opengraph.githubassets.com/1/" + key
	}
	const maxLen = 2000
	if len(readme) > maxLen {
		readme = readme[:maxLen]
	}
	result["readme"] = readme
	if readme == "" {
		result["error"] = "未找到 README"
	}
	detailCacheMu.Lock()
	detailCache[key] = detailCacheEntry{result: result, at: time.Now()}
	detailCacheMu.Unlock()
	return result
}

func httpGetBody(client *http.Client, u string) (string, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "dsh-reasonix-market/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// repoFromURL 从 GitHub URL 提取 owner/repo。
func repoFromURL(u string) (string, string) {
	u = strings.TrimSpace(u)
	u = strings.TrimSuffix(u, ".git")
	u = strings.TrimSuffix(u, "/")
	idx := strings.Index(u, "github.com/")
	if idx < 0 {
		return "", ""
	}
	rest := u[idx+len("github.com/"):]
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", ""
	}
	return parts[0], parts[1]
}

// extractFirstImage 从 markdown 提取第一张图片 URL；相对路径转为 GitHub raw 地址。
func extractFirstImage(md, owner, repo string) string {
	re := regexp.MustCompile(`!\[[^\]]*\]\(([^)\s]+)\)`)
	m := re.FindStringSubmatch(md)
	if m == nil {
		return ""
	}
	u := strings.Trim(m[1], `"'`)
	if u == "" {
		return ""
	}
	if strings.HasPrefix(u, "http") {
		return u
	}
	u = strings.TrimPrefix(u, "./")
	u = strings.TrimPrefix(u, "/")
	return "https://raw.githubusercontent.com/" + owner + "/" + repo + "/HEAD/" + u
}
