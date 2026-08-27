// app_market_api.go — 插件市场动态源（imsai / deepseek1024 公共 API）+ 离线回退。
//
// 数据流：PluginMarket(query, category) 优先调 imsai API（联网最新目录），
// 失败/超时/离线模式（DSH_MARKET_OFFLINE=1）时回退 go:embed 的 registry.json（2961 条离线兜底）。
// 端点（见 https://github.com/imsai-sh/dsh-1024store/blob/main/web/docs/api.md）：
//   - 搜索: api.deepseek1024.com/v1/plugins/search?q=&category=&limit=100  （q 必填，全量目录）
//   - 浏览: deepseek1024.com/api/v1/plugins?category=                      （至多 500 条 npm 可安装，stars 排序）
// 匿名限流 50 次/天、10 次/分；浏览结果内存缓存 5 分钟，搜索实时。

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	imsaiSearchBase = "https://api.deepseek1024.com/v1/plugins/search"
	imsaiListBase   = "https://deepseek1024.com/api/v1/plugins"
	imsaiTimeout    = 6 * time.Second
	imsaiCacheTTL   = 5 * time.Minute
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
	Total   int           `json:"total"`
	Results []imsaiPlugin `json:"results"`
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

var (
	marketCacheMu sync.Mutex
	marketCache   map[string]any
	marketCacheAt time.Time
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

// dynamicSearch 关键词搜索（q 必填，实时最新）。
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

// dynamicList 浏览（空查询）：500 条 npm 可安装，stars 排序，缓存 5 分钟。
func dynamicList(client *http.Client, category string) (map[string]any, bool) {
	marketCacheMu.Lock()
	defer marketCacheMu.Unlock()
	if marketCache != nil && time.Since(marketCacheAt) < imsaiCacheTTL {
		return marketCache, true
	}
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
	marketCache = result
	marketCacheAt = time.Now()
	return result, true
}

// marketResult 组装统一返回结构。
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
