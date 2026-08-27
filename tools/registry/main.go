// tools/registry — 从 GitHub topic:dsh-plugin 抓取全量插件目录，重生成 registry.json。
//
// 用法:  $env:GH_TOKEN=(gh auth token); go run ./tools/registry
// 说明:
//   - 数据源与 dsh-plugin-market(chnjames) 的 build-registry.mjs 同源:
//     api.github.com/search/repositories?q=topic:dsh-plugin&sort=stars&order=desc
//   - 保留现有 registry.json 条目的 zh 描述与分类（按 name 合并，旧条目优先）
//   - 新条目按名称/描述关键词启发式分类
//   - 抓取失败/为空时保留旧 registry.json（不覆盖好文件）
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

type marketPlugin struct {
	Name     string `json:"name"`
	Owner    string `json:"owner"`
	URL      string `json:"url"`
	Category string `json:"category"`
	En       string `json:"en"`
	Zh       string `json:"zh"`
	Stars    int    `json:"stars"`
	Install  string `json:"install"`
	Npm      string `json:"npm"`
}

type registry struct {
	Updated    string                      `json:"updated"`
	Count      int                         `json:"count"`
	Categories map[string]map[string]string `json:"categories"`
	Plugins    []marketPlugin              `json:"plugins"`
}

type ghRepo struct {
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	Stargazers  int    `json:"stargazers_count"`
}

type ghSearchResult struct {
	TotalCount int      `json:"total_count"`
	Items      []ghRepo `json:"items"`
}

var excluded = map[string]bool{
	"deepseek-ai/deepseek-harness": true,
	"sealfly/dsh-reasonix-desktop": true,
}

func main() {
	token := os.Getenv("GH_TOKEN")
	client := &http.Client{Timeout: 30 * time.Second}

	// 1. 读现有 registry（保留 zh/分类）
	old := registry{}
	if b, err := os.ReadFile("registry.json"); err == nil {
		_ = json.Unmarshal(b, &old)
	}
	oldByKey := map[string]marketPlugin{}
	for _, p := range old.Plugins {
		oldByKey[strings.ToLower(p.Name)] = p
	}

	// 2. 抓取 topic:dsh-plugin（search API 每种排序只放行前 1000，用多种排序取并集突破限制）
	sorts := []string{"stars", "updated", "forks", "name"}
	all := []ghRepo{}
	seenFull := map[string]bool{}
	for _, sortBy := range sorts {
		fmt.Printf("== 抓取排序: %s ==\n", sortBy)
		for page := 1; page <= 12; page++ {
			url := fmt.Sprintf("https://api.github.com/search/repositories?q=topic:dsh-plugin&sort=%s&order=desc&per_page=100&page=%d", sortBy, page)
			req, _ := http.NewRequest("GET", url, nil)
			req.Header.Set("Accept", "application/vnd.github+json")
			req.Header.Set("User-Agent", "dsh-reasonix-registry")
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			var res ghSearchResult
			if err := fetchJSON(client, req, &res); err != nil {
				fmt.Fprintf(os.Stderr, "  %s page %d failed: %v\n", sortBy, page, err)
				break
			}
			if len(res.Items) == 0 {
				break
			}
			added := 0
			for _, it := range res.Items {
				k := strings.ToLower(it.FullName)
				if !seenFull[k] {
					seenFull[k] = true
					all = append(all, it)
					added++
				}
			}
			fmt.Printf("  %s page %d: +%d (unique %d)\n", sortBy, page, added, len(all))
			time.Sleep(1 * time.Second) // search API 限流缓冲（认证 30 req/min）
		}
	}
	fmt.Printf("抓取完成(去重): %d 个仓库\n", len(all))

	// 3. 构造新条目
	plugins := make([]marketPlugin, 0, len(all))
	seen := map[string]bool{}
	for _, r := range all {
		if excluded[r.FullName] {
			continue
		}
		parts := strings.SplitN(r.FullName, "/", 2)
		if len(parts) != 2 {
			continue
		}
		owner, name := parts[0], parts[1]
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		en := strings.TrimSpace(r.Description)
		if len(en) > 200 {
			en = en[:200]
		}
		cat := "tools"
		zh := ""
		npm := ""
		if o, ok := oldByKey[key]; ok {
			cat = o.Category
			zh = o.Zh
			npm = o.Npm
		} else {
			cat = classify(name, en)
		}
		plugins = append(plugins, marketPlugin{
			Name:     name,
			Owner:    owner,
			URL:      "https://github.com/" + r.FullName,
			Category: cat,
			En:       en,
			Zh:       zh,
			Stars:    r.Stargazers,
			Install:  "dsh plugin --profile web add github:" + r.FullName,
			Npm:      npm,
		})
	}

	// 2b. 合并旧条目（旧 registry 中未出现在新抓取里的条目也保留——不丢 zh/分类）
	oldSeen := map[string]bool{}
	for _, p := range plugins {
		oldSeen[strings.ToLower(p.Name)] = true
	}
	keptOld := 0
	for _, o := range old.Plugins {
		k := strings.ToLower(o.Name)
		if !oldSeen[k] {
			plugins = append(plugins, o)
			oldSeen[k] = true
			keptOld++
		}
	}
	if keptOld > 0 {
		fmt.Printf("合并旧条目: +%d（不在新抓取中）\n", keptOld)
	}

	// 4. 按 stars 降序
	sort.Slice(plugins, func(i, j int) bool { return plugins[i].Stars > plugins[j].Stars })

	// 5. 分类定义（保留旧 + 补充新出现的）
	cats := map[string]map[string]string{}
	for k, v := range old.Categories {
		cats[k] = v
	}
	if _, ok := cats["tools"]; !ok {
		cats["tools"] = map[string]string{"en": "Tools & Capabilities", "zh": "工具与能力"}
	}
	for _, p := range plugins {
		if _, ok := cats[p.Category]; !ok {
			cats[p.Category] = map[string]string{"en": p.Category, "zh": p.Category}
		}
	}

	out := registry{
		Updated:    time.Now().Format("2006-01-02"),
		Count:      len(plugins),
		Categories: cats,
		Plugins:    plugins,
	}
	b, err := json.Marshal(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal failed: %v\n", err)
		return
	}
	if err := os.WriteFile("registry.json", b, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write failed: %v\n", err)
		return
	}
	fmt.Printf("已生成 registry.json: %d 插件 (旧 %d -> 新 %d, zh 保留 %d)\n",
		len(plugins), len(old.Plugins), len(plugins), len(oldByKey))
}

func fetchJSON(client *http.Client, req *http.Request, out any) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 403 || resp.StatusCode == 429 {
		// 限流：等 Retry-After 后重试一次
		wait := 10 * time.Second
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if n, err := time.ParseDuration(ra + "s"); err == nil {
				wait = n
			}
		}
		fmt.Fprintf(os.Stderr, "rate-limited %d, wait %v\n", resp.StatusCode, wait)
		time.Sleep(wait)
		resp2, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp2.Body.Close()
		resp = resp2
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func containsAny(s string, kws ...string) bool {
	for _, k := range kws {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// classify 新条目启发式分类（名称 + 描述关键词）。
func classify(name, desc string) string {
	s := strings.ToLower(name + " " + desc)
	switch {
	case containsAny(s, "theme", "skin", "appearance", "主题", "皮肤", "外观"):
		return "theme"
	case containsAny(s, "pet", "game", "play", "quiz", "arcade", "chess", "gomoku", "wordle", "小游戏", "宠物", "游戏", "棋"):
		return "fun"
	case containsAny(s, "market", "registry", "store", "marketplace", "目录", "市场"):
		return "market"
	case containsAny(s, "memory", "记忆"):
		return "memory"
	case containsAny(s, "model", "provider", "llm", "模型", "接入"):
		return "model"
	case containsAny(s, "notify", "notification", "通知", "提醒", "badge"):
		return "notify"
	case containsAny(s, "skill", "技能", "prompt"):
		return "skill"
	case containsAny(s, "workflow", "pipeline", "工作流"):
		return "workflow"
	case containsAny(s, "session", "conversation", "chat", "会话", "对话"):
		return "session"
	case containsAny(s, "git", "code", "review", "lint", "test", "build", "cli", "diagnos", "doctor", "guard", "scan", "代码", "开发", "审查", "诊断"):
		return "dev"
	case containsAny(s, "sidebar", "panel", "widget", "dock", "statusbar", "侧边栏", "面板", "组件"):
		return "ui"
	default:
		return "tools"
	}
}
