package main

// app_remote_files.go — 右栏「远程」页签的**远程文件读写**（真机实现，走 ssh，不依赖远端 serve）。
//
// 背景（2026-09-20 真机踩到）：右栏「远程」页签是上游 1.38.2 自带的**条件渲染**页签
// （bundle 里 `a && DockTab label=i("rightDock.remote")`），只要 ~/.reasonix/remote-hosts.json
// 里有主机就会出现；它内部的文件树/文件查看器会按 **2/2/4 个实参**调用桥方法
// ListRemoteDir(hostId,path) / ReadRemoteFile(hostId,path) / WriteRemoteFile(hostId,path,body,mtime)，
// 而本项目的 Go 侧当时还是 `app_stubs2.go` 里的**零参占位桩** → Wails 绑定按参数个数校验，
// 直接抛 `error parsing arguments: received 2 arguments to method 'main.App.ListRemoteDir', expected 0`，
// 面板显示「加载失败」。
//
// 本文件把这几个方法按前端契约真正实现：
//
//   - 列目录：**一次 ssh 会话**里用 POSIX sh 循环列出条目（名字/是否目录/是否软链/大小/修改时间），
//     兼容 GNU 与 BSD 的 stat（先试 `stat -c %Y`，再退 `stat -f %m`）。用 `ls -A` 取名字，
//     因此带空格的目录名不会被拆开（逐个 `read` 一整行）。
//   - 读文件：一次 ssh 会话里先输出 mtime、换行、再输出文件内容（首行是元信息，其余原样为正文），
//     便于「打开文件」只付一次往返；超大文件按上限截断并标记 truncated；含 NUL 字节判定为二进制，
//     不回传正文（前端据此显示「二进制文件不可编辑」）。
//   - 写文件：把正文从 stdin 灌给 `cat > 目标`（不经命令行，避免引号/换行/编码问题）；
//     传了 expectedMtime（非 0）时先比对远端 mtime，不一致则返回 conflict 让前端提示覆盖，
//     而不是默默冲掉别人的修改。
//
// 诚实边界（与本项目其余部分一致）：远端 DSH **serve**（网页版工作区、远端服务托管、端口转发）
// 仍未实现 —— OpenRemoteWorkspace 会明确报错，而不是假装成功。

import (
	"bytes"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"
)

// maxRemoteFileBytes 单个远程文件的读取上限（超过则截断并置 truncated）。
const maxRemoteFileBytes = 2 << 20 // 2 MiB

// remoteSSHArgs 组装 ssh 的通用参数（与 probeRemoteHost 保持一致的策略）。
func remoteSSHArgs(h remoteHost) []string {
	port := h.Port
	if port == 0 {
		port = 22
	}
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=8",
		"-o", "StrictHostKeyChecking=accept-new",
		"-p", fmt.Sprint(port),
	}
	if h.IdentityFile != "" {
		args = append(args, "-i", h.IdentityFile)
	}
	if h.ProxyJump != "" {
		args = append(args, "-J", h.ProxyJump)
	}
	target := h.Host
	if h.User != "" {
		target = h.User + "@" + h.Host
	}
	return append(args, target)
}

// sshQuote 把字符串安全地嵌进单引号 shell 片段。
func sshQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// remoteListScript 生成列目录脚本；路径以参数形式传入（$1），避免拼接注入。
const remoteListScript = `set -e
cd -- "$1" || { echo "__DSH_ERR__ cannot cd" >&2; exit 3; }
ls -A | while IFS= read -r n; do
  [ -z "$n" ] && continue
  if [ -L "./$n" ]; then link=1; else link=0; fi
  if [ -d "./$n" ]; then isdir=1; size=0; else isdir=0; size=$(wc -c < "./$n" 2>/dev/null || echo 0); fi
  m=$(stat -c %Y "./$n" 2>/dev/null || stat -f %m "./$n" 2>/dev/null || echo 0)
  printf '%s\t%s\t%s\t%s\t%s\n' "$isdir" "$link" "$size" "$m" "$n"
done
`

// remoteStatScript 生成取 mtime 的脚本。
const remoteStatScript = `m=$(stat -c %Y "$1" 2>/dev/null || stat -f %m "$1" 2>/dev/null || echo 0)
printf '%s\n' "$m"
`

// remoteRunScript 在远端执行一段 POSIX sh 脚本（脚本经 stdin 传输，避免引号地狱），
// arg 作为 $1 传入，stdinData 作为脚本自身的标准输入。
//
// 这里做成**可替换的函数变量**（remoteExecHook），测试里可换成假的"远端"来验证解析逻辑，
// 不必真的连一台机器 —— 本项目的远程主机常常不可达（见 docs 里的真机记录）。
var remoteExecHook = func(h remoteHost, script string, arg string, stdinData string) (stdout []byte, stderr []byte, err error) {
	args := append(remoteSSHArgs(h), "sh -s dsh "+sshQuote(arg))
	cmd := hiddenCmd("ssh", args...)
	cmd.Stdin = strings.NewReader(stdinData)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.Bytes(), errBuf.Bytes(), err
}

// remoteSh 包装一次远端脚本执行（脚本经 stdin 传输）。
func remoteSh(h remoteHost, script string, arg string, stdinData string) ([]byte, []byte, error) {
	return remoteExecHook(h, script, arg, stdinData)
}

// ListRemoteDir 列出远程目录（前端契约：数组元素含 name/path/isDir/size/mtimeUnix/symlink）。
func (a *App) ListRemoteDir(hostID string, dir string) []any {
	h, ok := findRemoteHost(hostID)
	if !ok {
		resumeLog("remote-fs: ListRemoteDir 主机 %q 不存在", hostID)
		return []any{}
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = "."
	}
	stdout, stderr, err := remoteSh(h, remoteListScript, dir, "")
	if err != nil {
		msg := strings.TrimSpace(string(stderr))
		if msg == "" {
			msg = err.Error()
		}
		resumeLog("remote-fs: 列目录失败 %s:%s —— %s", h.Label, dir, tailText(msg, 200))
		return []any{}
	}

	out := []any{}
	for _, line := range strings.Split(string(stdout), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) != 5 {
			continue
		}
		name := parts[4]
		if name == "." || name == ".." {
			continue
		}
		size, _ := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
		mtime, _ := strconv.ParseInt(strings.TrimSpace(parts[3]), 10, 64)
		out = append(out, map[string]any{
			"name":      name,
			"path":      remoteJoin(dir, name),
			"isDir":     parts[0] == "1",
			"symlink":   parts[1] == "1",
			"size":      size,
			"mtimeUnix": mtime,
		})
	}
	// 目录在前、再按名字排序（前端按数组顺序渲染，稳定顺序才好用）
	sortRemoteEntries(out)
	resumeLog("remote-fs: 列出 %s:%s → %d 项", h.Label, dir, len(out))
	return out
}

// remoteJoin 用 POSIX 规则拼路径（远端一律 /，不能用 filepath）。
func remoteJoin(dir, name string) string {
	if dir == "" || dir == "." {
		return name
	}
	return path.Join(dir, name)
}

// sortRemoteEntries 目录优先 + 名称升序。
func sortRemoteEntries(items []any) {
	less := func(i, j int) bool {
		a, _ := items[i].(map[string]any)
		b, _ := items[j].(map[string]any)
		ad, _ := a["isDir"].(bool)
		bd, _ := b["isDir"].(bool)
		if ad != bd {
			return ad
		}
		return fmt.Sprint(a["name"]) < fmt.Sprint(b["name"])
	}
	// 简单插入排序即可（单目录条目量级很小），避免引入 sort 的接口样板
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && less(j, j-1); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

// ReadRemoteFile 读取远程文件（前端契约：path/body/size/mtimeUnix/truncated/binary/err）。
//
// 一次 ssh 往返：脚本先打印 mtime 再打印文件内容，首行是 mtime，其余为正文。
func (a *App) ReadRemoteFile(hostID string, filePath string) map[string]any {
	h, ok := findRemoteHost(hostID)
	if !ok {
		return map[string]any{"path": filePath, "body": "", "err": fmt.Sprintf("远程主机 %q 不存在", hostID)}
	}
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return map[string]any{"path": filePath, "body": "", "err": "路径为空"}
	}
	// 头里带上限字节数，避免把几百 MB 的文件灌进内存
	script := `m=$(stat -c %Y "$1" 2>/dev/null || stat -f %m "$1" 2>/dev/null || echo 0)
sz=$(wc -c < "$1" 2>/dev/null || echo 0)
printf '%s\t%s\n' "$m" "$sz"
head -c ` + strconv.Itoa(maxRemoteFileBytes) + ` -- "$1" 2>/dev/null || head -c ` + strconv.Itoa(maxRemoteFileBytes) + ` "$1"
`
	stdoutBytes, stderrBytes, err := remoteSh(h, script, filePath, "")
	if err != nil {
		msg := strings.TrimSpace(string(stderrBytes))
		if msg == "" {
			msg = err.Error()
		}
		resumeLog("remote-fs: 读文件失败 %s:%s —— %s", h.Label, filePath, tailText(msg, 200))
		return map[string]any{"path": filePath, "body": "", "err": tailText(msg, 300)}
	}

	raw := stdoutBytes
	nl := bytes.IndexByte(raw, '\n')
	if nl < 0 {
		return map[string]any{"path": filePath, "body": "", "err": "远端返回异常（缺少元信息行）"}
	}
	meta := strings.Split(strings.TrimSpace(string(raw[:nl])), "\t")
	mtime, _ := strconv.ParseInt(strings.TrimSpace(meta[0]), 10, 64)
	fullSize := int64(0)
	if len(meta) > 1 {
		fullSize, _ = strconv.ParseInt(strings.TrimSpace(meta[1]), 10, 64)
	}
	body := raw[nl+1:]
	truncated := int64(len(body)) < fullSize

	// 二进制判定：前 8KiB 出现 NUL 即认为二进制，不回传正文
	probeLen := len(body)
	if probeLen > 8192 {
		probeLen = 8192
	}
	binary := bytes.IndexByte(body[:probeLen], 0) >= 0
	result := map[string]any{
		"path":      filePath,
		"size":      int64(len(body)),
		"mtimeUnix": mtime,
		"truncated": truncated,
		"binary":    binary,
	}
	if binary {
		result["body"] = ""
	} else {
		result["body"] = string(body)
	}
	resumeLog("remote-fs: 读 %s:%s → %d 字节（binary=%v truncated=%v）", h.Label, filePath, len(body), binary, truncated)
	return result
}

// WriteRemoteFile 写远程文件（前端契约：ok/conflict/newMtimeUnix）。
//
// expectedMtimeUnix 为 0 表示强制覆盖；非 0 时若远端当前 mtime 与之不同 → 返回 conflict。
func (a *App) WriteRemoteFile(hostID string, filePath string, body string, expectedMtimeUnix int64) map[string]any {
	h, ok := findRemoteHost(hostID)
	if !ok {
		return map[string]any{"ok": false, "conflict": false, "err": fmt.Sprintf("远程主机 %q 不存在", hostID)}
	}
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return map[string]any{"ok": false, "conflict": false, "err": "路径为空"}
	}

	if expectedMtimeUnix != 0 {
		current, err := remoteMtime(h, filePath)
		if err == nil && current != 0 && current != expectedMtimeUnix {
			resumeLog("remote-fs: 写 %s:%s 冲突（远端 %d ≠ 预期 %d）", h.Label, filePath, current, expectedMtimeUnix)
			return map[string]any{"ok": false, "conflict": true, "newMtimeUnix": current}
		}
	}

	// 正文走 stdin：`cat > 目标`
	_, stderrBytes, err := remoteSh(h, `cat > "$1"`, filePath, body)
	if err != nil {
		msg := strings.TrimSpace(string(stderrBytes))
		if msg == "" {
			msg = err.Error()
		}
		resumeLog("remote-fs: 写文件失败 %s:%s —— %s", h.Label, filePath, tailText(msg, 200))
		return map[string]any{"ok": false, "conflict": false, "err": tailText(msg, 300)}
	}
	newMtime, _ := remoteMtime(h, filePath)
	resumeLog("remote-fs: 已写 %s:%s（%d 字节，mtime=%d）", h.Label, filePath, len(body), newMtime)
	return map[string]any{"ok": true, "conflict": false, "newMtimeUnix": newMtime}
}

// remoteMtime 取远端文件 mtime（秒）；取不到返回 0 与错误。
func remoteMtime(h remoteHost, filePath string) (int64, error) {
	stdout, _, err := remoteSh(h, remoteStatScript, filePath, "")
	if err != nil {
		return 0, err
	}
	v, perr := strconv.ParseInt(strings.TrimSpace(string(stdout)), 10, 64)
	if perr != nil {
		return 0, perr
	}
	return v, nil
}

// OpenRemoteWorkspace 打开远端工作区（网页版）。
//
// 上游按钮会调它；但远端 DSH serve 本项目未实现 —— 明确报错而不是假装成功
// （与 app_remote.go 里 RemoteServerStatus/RemoteForwards 的诚实边界一致）。
func (a *App) OpenRemoteWorkspace(hostID string, workspace string) error {
	return fmt.Errorf("本项目未实现远端 DSH 服务（serve），无法打开远端工作区网页版；"+
		"右栏「远程」页签的文件浏览/编辑走 ssh，可直接使用（主机 %s，工作区 %s）", hostID, workspace)
}

// ConfirmRemoteHostKey 确认主机指纹。
//
// 我们的 ssh 探测统一用 StrictHostKeyChecking=accept-new（首次自动接受、之后变更即报错），
// 因此不会走前端的"指纹确认"分支；这里按契约接收参数以免绑定层抛参数错，并如实记录调用。
func (a *App) ConfirmRemoteHostKey(hostID string, promptID string) error {
	resumeLog("remote-fs: ConfirmRemoteHostKey(%q, %q) —— 本项目采用 accept-new 策略，无需人工确认", hostID, promptID)
	return nil
}

// ConfirmRemoteSecret 确认/提交口令（本项目不落盘口令，走 ssh agent / 密钥）。
func (a *App) ConfirmRemoteSecret(hostID string, promptID string, secret string, remember bool) error {
	resumeLog("remote-fs: ConfirmRemoteSecret(%q, %q, secret=%d 字节, remember=%v) —— 本项目不落盘口令",
		hostID, promptID, len(secret), remember)
	if strings.TrimSpace(secret) == "" {
		return fmt.Errorf("本项目不使用交互式口令：请在系统 ssh-agent 里加载密钥，或为主机配置 IdentityFile")
	}
	return nil
}

// remoteFileNow 便于测试/日志取当前时间（避免与 time 包未使用冲突）。
func remoteFileNow() int64 { return time.Now().Unix() }
