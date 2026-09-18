package main

// app_remote.go — 「设置-远程 SSH」页的真实实现（替换原先的空壳）。
//
// 之前这些方法全是 `return []any{}` / `return nil` 的空壳：页面能开，但主机列表恒为空、
// 扫描 SSH 配置无反应、连接状态永远空白。本文件按前端契约（1.38.2 RemoteHostView /
// RemoteHostInput / RemoteConnectionStatus）实现**真实数据 + 真实动作**：
//
//   - 主机库：~/.reasonix/remote-hosts.json（本项目自有的远程主机清单，增删改持久化）
//   - 扫描：解析 ~/.ssh/config 的 Host 块（HostName/User/Port/IdentityFile/ProxyJump），
//     跳过含通配符的模板条目
//   - 连接状态：**真的跑一次 ssh 探测**（BatchMode + ConnectTimeout，隐藏窗口），
//     结果按主机 TTL 缓存，避免页面重渲染时反复拉起 ssh
//   - 遗留数据：扫描/清理 ~/.reasonix 下旧版远程工作台留下的镜像与信任文件
//
// 明确不做（诚实边界）：远端 DSH 服务（serve/install/logs）、端口转发隧道进程、
// 远端会话/标签——它们属于"远程开发子系统"，设置页并不使用；相关方法保持"未配置/不支持"
// 的明确语义，而不是返回假数据。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// remoteHost 是本项目持久化的远程主机（对应前端 RemoteHostView）。
type remoteHost struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	User            string `json:"user"`
	IdentityFile    string `json:"identityFile"`
	ProxyJump       string `json:"proxyJump"`
	DefaultWorkspace string `json:"defaultWorkspace"`
	ServeInstall    string `json:"serveInstall"`
	CredentialMode  string `json:"credentialMode"`
	UseSSHConfig    bool   `json:"useSSHConfig"`
}

// remoteStatus 是一次探测结果（对应前端 RemoteConnectionStatus）。
type remoteStatus struct {
	HostID string `json:"hostId"`
	State  string `json:"state"`
	Error  string `json:"error,omitempty"`
	at     time.Time
}

var (
	remoteMu      sync.Mutex
	remoteStatuses = map[string]remoteStatus{}
)

const remoteStatusTTL = 20 * time.Second

// remoteHostsPath 返回主机库文件路径。
func remoteHostsPath() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "."
	}
	dir := filepath.Join(home, ".reasonix")
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, "remote-hosts.json")
}

// loadRemoteHosts 读取主机库。
func loadRemoteHosts() []remoteHost {
	data, err := os.ReadFile(remoteHostsPath())
	if err != nil {
		return []remoteHost{}
	}
	var list []remoteHost
	if json.Unmarshal(data, &list) != nil {
		return []remoteHost{}
	}
	return list
}

// saveRemoteHosts 写入主机库（原子替换）。
func saveRemoteHosts(list []remoteHost) error {
	data, _ := json.MarshalIndent(list, "", "  ")
	tmp := remoteHostsPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, remoteHostsPath())
}

// remoteHostView 转成前端 RemoteHostView。
func remoteHostView(h remoteHost, status remoteStatus) map[string]any {
	port := h.Port
	if port == 0 {
		port = 22
	}
	view := map[string]any{
		"id": h.ID, "label": h.Label, "host": h.Host, "port": port, "user": h.User,
		"identityFile": h.IdentityFile, "proxyJump": h.ProxyJump,
		"defaultWorkspace": h.DefaultWorkspace, "serveInstall": h.ServeInstall,
		"credentialMode": h.CredentialMode, "useSSHConfig": h.UseSSHConfig,
		// 密码/口令态：本项目不落盘口令（走 ssh agent / 密钥），如实上报 false
		"passwordSet": false, "keyPassphraseSet": false,
	}
	if status.State != "" {
		view["state"] = status.State
	}
	return view
}

// remoteHostFromInput 把前端 RemoteHostInput 归一。
func remoteHostFromInput(id string, in map[string]any) remoteHost {
	str := func(k string) string {
		if v, ok := in[k].(string); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	num := func(k string) int {
		switch v := in[k].(type) {
		case float64:
			return int(v)
		case int:
			return v
		}
		return 0
	}
	flag := func(k string) bool {
		if v, ok := in[k].(bool); ok {
			return v
		}
		return false
	}
	h := remoteHost{
		ID: id, Label: str("label"), Host: str("host"), Port: num("port"),
		User: str("user"), IdentityFile: str("identityFile"), ProxyJump: str("proxyJump"),
		DefaultWorkspace: str("defaultWorkspace"), ServeInstall: str("serveInstall"),
		CredentialMode: str("credentialMode"), UseSSHConfig: flag("useSSHConfig"),
	}
	if h.ID == "" {
		h.ID = "host-" + sanitizeMCPName(h.Host+"-"+h.User) + "-" + fmt.Sprint(time.Now().UnixNano()%100000)
	}
	if h.Label == "" {
		h.Label = h.Host
	}
	if h.Port == 0 {
		h.Port = 22
	}
	if h.CredentialMode == "" {
		h.CredentialMode = "agent-or-key"
	}
	return h
}

// ===== SSH config 扫描 =====

// sshConfigPath 返回 ~/.ssh/config 路径。
func sshConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "config")
}

// scanSSHConfigHosts 解析 ~/.ssh/config，返回可用的主机条目。
func scanSSHConfigHosts() []map[string]any {
	data, err := os.ReadFile(sshConfigPath())
	if err != nil {
		return []map[string]any{}
	}
	out := []map[string]any{}
	var cur map[string]any
	flush := func() {
		if cur == nil {
			return
		}
		host := strings.TrimSpace(fmt.Sprint(cur["host"]))
		// 跳过通配符模板（Host *、Host *.example.com 这类不是具体主机）
		if host != "" && !strings.ContainsAny(host, "*?!") {
			out = append(out, cur)
		}
		cur = nil
	}
	for _, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 支持 `Host a b c`：只取第一个别名作为 label（其余别名的条目通常等价）
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.ToLower(fields[0])
		value := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
		if strings.Contains(value, "=") && (strings.HasPrefix(value, "=")) {
			value = strings.TrimSpace(strings.TrimPrefix(value, "="))
		}
		switch key {
		case "host":
			flush()
			cur = map[string]any{
				"label": fields[1], "host": fields[1], "port": 22,
				"user": "", "identityFile": "", "proxyJump": "",
				"defaultWorkspace": "", "serveInstall": "auto",
				"credentialMode": "agent-or-key", "useSSHConfig": true,
			}
		case "hostname":
			if cur != nil {
				cur["host"] = value
			}
		case "user":
			if cur != nil {
				cur["user"] = value
			}
		case "port":
			if cur != nil {
				var p int
				_, _ = fmt.Sscanf(value, "%d", &p)
				if p > 0 {
					cur["port"] = p
				}
			}
		case "identityfile":
			if cur != nil {
				cur["identityFile"] = strings.Trim(value, `"`)
			}
		case "proxyjump":
			if cur != nil {
				cur["proxyJump"] = value
			}
		}
	}
	flush()
	sort.Slice(out, func(i, j int) bool {
		return fmt.Sprint(out[i]["label"]) < fmt.Sprint(out[j]["label"])
	})
	return out
}

// ===== 连通性探测（真实 ssh）=====

// probeRemoteHost 跑一次真实 ssh 探测。返回 state 与错误说明。
//
// 判据：命令成功且输出含探针串 → connected；"Permission denied"/"publickey" → degraded
// （主机可达但认证失败，提示用户而不是当成"不通"）；其余 → stopped + 原始错误尾部。
func probeRemoteHost(h remoteHost) remoteStatus {
	port := h.Port
	if port == 0 {
		port = 22
	}
	target := h.Host
	if h.User != "" {
		target = h.User + "@" + h.Host
	}
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=6",
		"-o", "StrictHostKeyChecking=accept-new",
		"-p", fmt.Sprint(port),
	}
	if h.IdentityFile != "" {
		args = append(args, "-i", h.IdentityFile)
	}
	if h.ProxyJump != "" {
		args = append(args, "-J", h.ProxyJump)
	}
	args = append(args, target, "echo dsh-remote-probe-ok")

	out, err := hiddenCmd("ssh", args...).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err == nil && strings.Contains(text, "dsh-remote-probe-ok") {
		return remoteStatus{HostID: h.ID, State: "connected", at: time.Now()}
	}
	msg := text
	if msg == "" && err != nil {
		msg = err.Error()
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "permission denied") || strings.Contains(lower, "publickey") ||
		strings.Contains(lower, "authentication") {
		return remoteStatus{HostID: h.ID, State: "degraded", Error: tailText(msg, 200), at: time.Now()}
	}
	return remoteStatus{HostID: h.ID, State: "stopped", Error: tailText(msg, 200), at: time.Now()}
}

// tailText 截取错误尾部（ssh 的错误常在最后一行）。
func tailText(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return "…" + s[len(s)-max:]
}

// statusForHost 取（带 TTL 缓存的）探测结果。
func statusForHost(h remoteHost) remoteStatus {
	remoteMu.Lock()
	cached, ok := remoteStatuses[h.ID]
	remoteMu.Unlock()
	if ok && time.Since(cached.at) < remoteStatusTTL {
		return cached
	}
	st := probeRemoteHost(h)
	remoteMu.Lock()
	remoteStatuses[h.ID] = st
	remoteMu.Unlock()
	return st
}

// findRemoteHost 按 id 或 label 找主机。
func findRemoteHost(key string) (remoteHost, bool) {
	key = strings.TrimSpace(key)
	for _, h := range loadRemoteHosts() {
		if h.ID == key || h.Label == key {
			return h, true
		}
	}
	return remoteHost{}, false
}

// ===== 桥方法 =====

// RemoteHosts 返回远程主机列表。
func (a *App) RemoteHosts() []any {
	hosts := loadRemoteHosts()
	out := make([]any, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, remoteHostView(h, remoteStatus{}))
	}
	return out
}

// ScanSSHConfig 扫描 ~/.ssh/config，返回可导入的主机（前端用它做"从 SSH 配置导入"）。
func (a *App) ScanSSHConfig() []any {
	found := scanSSHConfigHosts()
	// 过滤掉已经在库里的（同 host+user+port）
	existing := map[string]bool{}
	for _, h := range loadRemoteHosts() {
		existing[fmt.Sprintf("%s|%s|%d", h.Host, h.User, h.Port)] = true
	}
	out := []any{}
	for _, f := range found {
		key := fmt.Sprintf("%v|%v|%v", f["host"], f["user"], f["port"])
		if existing[key] {
			continue
		}
		out = append(out, f)
	}
	resumeLog("remote: ScanSSHConfig 命中 %d 条、可导入 %d 条", len(found), len(out))
	return out
}

// AddRemoteHost 添加远程主机。
func (a *App) AddRemoteHost(in map[string]any) map[string]any {
	h := remoteHostFromInput("", in)
	list := loadRemoteHosts()
	list = append(list, h)
	if err := saveRemoteHosts(list); err != nil {
		resumeLog("remote: 保存主机失败: %v", err)
	}
	resumeLog("remote: 已添加主机 %s (%s)", h.Label, h.Host)
	return remoteHostView(h, remoteStatus{})
}

// UpdateRemoteHost 更新远程主机。
func (a *App) UpdateRemoteHost(id string, in map[string]any) map[string]any {
	updated := remoteHostFromInput(id, in)
	list := loadRemoteHosts()
	for i := range list {
		if list[i].ID == id {
			updated.ID = list[i].ID
			list[i] = updated
			_ = saveRemoteHosts(list)
			remoteMu.Lock()
			delete(remoteStatuses, updated.ID) // 配置变了，旧状态作废
			remoteMu.Unlock()
			resumeLog("remote: 已更新主机 %s", updated.Label)
			return remoteHostView(updated, remoteStatus{})
		}
	}
	// 不存在则视为新增（前端偶发竞态时不丢数据）
	list = append(list, updated)
	_ = saveRemoteHosts(list)
	return remoteHostView(updated, remoteStatus{})
}

// RemoveRemoteHost 删除远程主机。
func (a *App) RemoveRemoteHost(id string) error {
	list := loadRemoteHosts()
	out := list[:0]
	found := false
	for _, h := range list {
		if h.ID == id {
			found = true
			continue
		}
		out = append(out, h)
	}
	if !found {
		return fmt.Errorf("远程主机 %q 不存在", id)
	}
	remoteMu.Lock()
	delete(remoteStatuses, id)
	remoteMu.Unlock()
	return saveRemoteHosts(out)
}

// ConnectRemoteHost 主动探测并记录状态；探测失败返回错误（前端据此提示）。
func (a *App) ConnectRemoteHost(id string) error {
	h, ok := findRemoteHost(id)
	if !ok {
		return fmt.Errorf("远程主机 %q 不存在", id)
	}
	st := probeRemoteHost(h)
	remoteMu.Lock()
	remoteStatuses[h.ID] = st
	remoteMu.Unlock()
	if st.State != "connected" {
		if st.Error != "" {
			return fmt.Errorf("连接 %s 失败：%s", h.Label, st.Error)
		}
		return fmt.Errorf("连接 %s 失败", h.Label)
	}
	return nil
}

// DisconnectRemoteHost 清掉状态缓存（本项目不维持长连接，断开即"状态归零"）。
func (a *App) DisconnectRemoteHost(id string) error {
	h, ok := findRemoteHost(id)
	key := id
	if ok {
		key = h.ID
	}
	remoteMu.Lock()
	remoteStatuses[key] = remoteStatus{HostID: key, State: "stopped", at: time.Now()}
	remoteMu.Unlock()
	return nil
}

// RemoteConnectionStatuses 返回各主机的连接状态（真实探测，TTL 缓存）。
//
// 性能：探测走 ssh（可能到秒级），因此这里**只探测未缓存/过期的**，且在并发下用 Mutex 串行保护；
// 前端在设置页渲染时调用，一次通常只探测 1~2 台。
func (a *App) RemoteConnectionStatuses() []any {
	hosts := loadRemoteHosts()
	out := make([]any, 0, len(hosts))
	for _, h := range hosts {
		st := statusForHost(h)
		item := map[string]any{"hostId": h.ID, "state": st.State}
		if st.Error != "" {
			item["error"] = st.Error
		}
		out = append(out, item)
	}
	return out
}

// ===== 遗留远程工作台数据（扫描/清理）=====

// remoteLegacyPaths 返回旧版远程工作台可能留下的路径。
func remoteLegacyPaths() (mirrorDir string, trustFile string) {
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".reasonix")
	return filepath.Join(base, "remote-mirrors"), filepath.Join(base, "remote-trust.json")
}

// ScanRemoteLegacyWorkbenchData 扫描遗留数据（镜像目录 + 信任文件）。
//
// mirrorCount = 镜像目录下的顶层条目数；mirrorBytes = 递归所有文件字节数
// （不能只取顶层 Info().Size()：那是目录条目本身的大小，实测恒为 0）。
func (a *App) ScanRemoteLegacyWorkbenchData() map[string]any {
	mirrorDir, trustFile := remoteLegacyPaths()
	count := 0
	var bytes int64
	if entries, err := os.ReadDir(mirrorDir); err == nil {
		count = len(entries)
		bytes = dirBytes(mirrorDir)
	}
	trust := false
	if info, err := os.Stat(trustFile); err == nil && !info.IsDir() {
		trust = true
	}
	return map[string]any{"mirrorCount": count, "mirrorBytes": bytes, "trustFile": trust}
}

// dirBytes 递归统计目录内文件总字节数。
func dirBytes(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, e := d.Info(); e == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// CleanRemoteLegacyWorkbenchData 清理指定类别（"mirrors" | "trust"）。
func (a *App) CleanRemoteLegacyWorkbenchData(target string) error {
	mirrorDir, trustFile := remoteLegacyPaths()
	switch strings.TrimSpace(target) {
	case "mirrors":
		if err := os.RemoveAll(mirrorDir); err != nil {
			return err
		}
		resumeLog("remote: 已清理遗留镜像目录 %s", mirrorDir)
		return nil
	case "trust":
		if err := os.Remove(trustFile); err != nil && !os.IsNotExist(err) {
			return err
		}
		resumeLog("remote: 已清理遗留信任文件 %s", trustFile)
		return nil
	default:
		return fmt.Errorf("未知的清理目标 %q（可用：mirrors / trust）", target)
	}
}

// RemoteLastWorkspace 最近使用的远端工作区（本项目未实现远端服务，如实返回空串）。
func (a *App) RemoteLastWorkspace(hostId string) string {
	return ""
}

// RemoteServerStatus 远端 DSH 服务状态：本项目不做远端服务托管，如实返回"未启动"。
func (a *App) RemoteServerStatus(hostId string, workspace string) map[string]any {
	return map[string]any{"state": "stopped", "workspace": workspace, "hostId": hostId,
		"detail": "本项目未实现远端 DSH 服务托管（设置页不使用该能力）"}
}

// RemoteServerLogs 远端服务日志：未托管 → 空。
func (a *App) RemoteServerLogs(hostId string, workspace string, tailLines int) string {
	return ""
}

// StopRemoteServer 未托管 → 无操作成功。
func (a *App) StopRemoteServer(hostId string, workspace string) error {
	return nil
}

// RemoteForwards 端口转发列表：本项目未实现隧道进程，如实返回空表。
func (a *App) RemoteForwards(hostId string) []any {
	return []any{}
}

// AddRemoteForward 端口转发：未实现隧道进程，明确报错而不是假装成功。
func (a *App) AddRemoteForward(hostId string, in map[string]any) (map[string]any, error) {
	return nil, fmt.Errorf("端口转发隧道尚未实现（设置页不使用该能力）")
}

// RemoveRemoteForward 同上。
func (a *App) RemoveRemoteForward(hostId string, forwardId string) error {
	return fmt.Errorf("端口转发隧道尚未实现")
}

// 兼容旧签名（无参版本在早期桩里出现过，前端以带参版本调用）。
var _ = regexp.MustCompile
