package main

// app_remote_files_test.go — 远程文件读写的**离线**验证：用假的"远端"替换 ssh 执行钩子，
// 覆盖解析与冲突逻辑，不依赖任何真实主机（本机到用户那台 172.16.65.70 常常不可达，
// 真机验证另见 docs；这里保证"代码路径本身正确"）。
//
// 为什么值得留：这几个方法曾经是零参占位桩，真机上直接抛
// `received 2 arguments to method 'main.App.ListRemoteDir', expected 0`，
// 而错误只在界面上一行小字里出现，很容易再次漏掉。

import (
	"fmt"
	"strings"
	"testing"
)

// fakeRemote 用给定脚本→输出映射替换 ssh 执行钩子，并在测试结束恢复。
type remoteCall struct {
	script string
	arg    string
	stdin  string
}

func withFakeRemote(t *testing.T, respond func(call remoteCall) (string, string, error)) *[]remoteCall {
	t.Helper()
	calls := &[]remoteCall{}
	orig := remoteExecHook
	remoteExecHook = func(h remoteHost, script string, arg string, stdinData string) ([]byte, []byte, error) {
		*calls = append(*calls, remoteCall{script: script, arg: arg, stdin: stdinData})
		out, errText, err := respond(remoteCall{script: script, arg: arg, stdin: stdinData})
		return []byte(out), []byte(errText), err
	}
	t.Cleanup(func() { remoteExecHook = orig })
	return calls
}

// withTempHost 造一台临时主机并让 findRemoteHost 能找到它（写临时主机库）。
func withTempHost(t *testing.T) remoteHost {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	h := remoteHost{ID: "host-test-1", Label: "测试机", Host: "10.0.0.9", Port: 22, User: "root"}
	if err := saveRemoteHosts([]remoteHost{h}); err != nil {
		t.Fatalf("写入测试主机库失败: %v", err)
	}
	return h
}

func TestListRemoteDirParsesEntries(t *testing.T) {
	withTempHost(t)
	// 模拟远端 sh 循环的输出：目录/普通文件/软链/带空格的名字
	fakeOut := strings.Join([]string{
		"1\t0\t0\t1700000000\tsrc",
		"0\t0\t1024\t1700000500\tREADME.md",
		"0\t1\t12\t1700000600\tlink.txt",
		"0\t0\t7\t1700000700\twith space.txt",
		"",
	}, "\n")
	calls := withFakeRemote(t, func(remoteCall) (string, string, error) { return fakeOut, "", nil })

	a := &App{}
	got := a.ListRemoteDir("host-test-1", "/srv/app")
	if len(got) != 4 {
		t.Fatalf("应解析出 4 个条目，实际 %d：%v", len(got), got)
	}
	// 目录优先排序：第一项应是 src
	first, _ := got[0].(map[string]any)
	if fmt.Sprint(first["name"]) != "src" || first["isDir"] != true {
		t.Fatalf("目录应排在最前，实际 %v", first)
	}
	// 路径拼接与字段
	var readme map[string]any
	for _, it := range got {
		m, _ := it.(map[string]any)
		if m["name"] == "README.md" {
			readme = m
		}
	}
	if readme == nil {
		t.Fatal("未找到 README.md 条目")
	}
	if readme["path"] != "/srv/app/README.md" {
		t.Fatalf("path 拼接错误：%v", readme["path"])
	}
	if readme["isDir"] != false || readme["symlink"] != false {
		t.Fatalf("isDir/symlink 解析错误：%v", readme)
	}
	if fmt.Sprint(readme["size"]) != "1024" || fmt.Sprint(readme["mtimeUnix"]) != "1700000500" {
		t.Fatalf("size/mtime 解析错误：%v", readme)
	}
	// 空格名字不能被截断
	found := false
	for _, it := range got {
		m, _ := it.(map[string]any)
		if m["name"] == "with space.txt" {
			found = true
		}
	}
	if !found {
		t.Fatal("带空格的文件名应完整保留")
	}
	// 传给远端的是 $1 参数（脚本经 stdin），不能把路径拼进脚本
	if len(*calls) != 1 || (*calls)[0].arg != "/srv/app" {
		t.Fatalf("远端调用参数错误：%+v", *calls)
	}
	if strings.Contains((*calls)[0].script, "/srv/app") {
		t.Fatal("路径不应被拼进脚本正文（应作为 $1 传入）")
	}
}

func TestListRemoteDirFailureIsEmptyNotPanic(t *testing.T) {
	withTempHost(t)
	withFakeRemote(t, func(remoteCall) (string, string, error) {
		return "", "ssh: connect to host 10.0.0.9 port 22: Connection timed out", fmt.Errorf("exit status 255")
	})
	a := &App{}
	if got := a.ListRemoteDir("host-test-1", "."); len(got) != 0 {
		t.Fatalf("连不上时应返回空表，实际 %v", got)
	}
}

func TestReadRemoteFileSplitsMetaAndBody(t *testing.T) {
	withTempHost(t)
	withFakeRemote(t, func(remoteCall) (string, string, error) {
		// 元信息：mtime + 远端全文大小 14（与正文一致 → 未截断）
		return "1700000500\t14\n# hello\nworld\n", "", nil
	})
	a := &App{}
	got := a.ReadRemoteFile("host-test-1", "/srv/app/README.md")
	if got["mtimeUnix"] != int64(1700000500) {
		t.Fatalf("mtimeUnix 解析错误：%v", got["mtimeUnix"])
	}
	if got["body"] != "# hello\nworld\n" {
		t.Fatalf("正文解析错误：%q", got["body"])
	}
	if got["binary"] != false || got["truncated"] != false {
		t.Fatalf("binary/truncated 判定错误：%v", got)
	}
	if got["err"] != nil {
		t.Fatalf("不应有错误：%v", got["err"])
	}
}

func TestReadRemoteFileDetectsBinary(t *testing.T) {
	withTempHost(t)
	withFakeRemote(t, func(remoteCall) (string, string, error) {
		return "1700000500\t8\nBIN\x00DATA\x01", "", nil
	})
	a := &App{}
	got := a.ReadRemoteFile("host-test-1", "/srv/app/blob.bin")
	if got["binary"] != true {
		t.Fatalf("含 NUL 应判定为二进制：%v", got)
	}
	if got["body"] != "" {
		t.Fatalf("二进制不应回传正文：%q", got["body"])
	}
}

func TestReadRemoteFileMarksTruncation(t *testing.T) {
	withTempHost(t)
	// 元信息声明 9999 字节，实际只回传 5 字节（模拟 head -c 截断）
	withFakeRemote(t, func(remoteCall) (string, string, error) {
		return "1700000500\t9999\nshort", "", nil
	})
	a := &App{}
	got := a.ReadRemoteFile("host-test-1", "/srv/app/big.txt")
	if got["truncated"] != true {
		t.Fatalf("实际字节少于远端大小时应置 truncated：%v", got)
	}
}

func TestWriteRemoteFileConflictAndSuccess(t *testing.T) {
	withTempHost(t)
	a := &App{}

	// 1) 远端 mtime 与预期不一致 → conflict，且**不写**
	wrote := false
	withFakeRemote(t, func(c remoteCall) (string, string, error) {
		if strings.Contains(c.script, "cat >") {
			wrote = true
		}
		return "1700009999\n", "", nil // 远端当前 mtime
	})
	got := a.WriteRemoteFile("host-test-1", "/srv/app/README.md", "new body", 1700000500)
	if got["conflict"] != true || got["ok"] != false {
		t.Fatalf("mtime 不一致应返回 conflict：%v", got)
	}
	if wrote {
		t.Fatal("检测到冲突时不应写入")
	}

	// 2) 强制覆盖（expected=0）→ 写入成功并回传新 mtime
	var writtenBody string
	withFakeRemote(t, func(c remoteCall) (string, string, error) {
		if strings.Contains(c.script, "cat >") {
			writtenBody = c.stdin
			return "", "", nil
		}
		return "1700010000\n", "", nil // stat 查询
	})
	got2 := a.WriteRemoteFile("host-test-1", "/srv/app/README.md", "hello\nworld\n", 0)
	if got2["ok"] != true || got2["conflict"] != false {
		t.Fatalf("强制覆盖应成功：%v", got2)
	}
	if writtenBody != "hello\nworld\n" {
		t.Fatalf("正文应经 stdin 原样传递，实际 %q", writtenBody)
	}
	if fmt.Sprint(got2["newMtimeUnix"]) != "1700010000" {
		t.Fatalf("应回传远端新 mtime，实际 %v", got2["newMtimeUnix"])
	}
}

func TestRemoteFileMethodsRejectUnknownHost(t *testing.T) {
	withTempHost(t)
	a := &App{}
	if got := a.ListRemoteDir("不存在的ID", "."); len(got) != 0 {
		t.Fatalf("未知主机应返回空目录：%v", got)
	}
	read := a.ReadRemoteFile("不存在的ID", "/x")
	if read["err"] == nil {
		t.Fatal("未知主机读取应带 err")
	}
	wr := a.WriteRemoteFile("不存在的ID", "/x", "b", 0)
	if wr["ok"] != false {
		t.Fatal("未知主机写入应失败")
	}
}

func TestOpenRemoteWorkspaceReportsUnimplemented(t *testing.T) {
	withTempHost(t)
	a := &App{}
	err := a.OpenRemoteWorkspace("host-test-1", "/srv/app")
	if err == nil {
		t.Fatal("远端 serve 未实现，应明确报错而不是假装成功")
	}
	if !strings.Contains(err.Error(), "serve") {
		t.Fatalf("错误信息应说明原因，实际：%v", err)
	}
}

func TestRemoteSSHArgsBuildsExpectedCommand(t *testing.T) {
	h := remoteHost{Host: "10.0.0.9", Port: 2222, User: "deploy", IdentityFile: "C:\\keys\\id", ProxyJump: "jump@bastion"}
	args := remoteSSHArgs(h)
	joined := strings.Join(args, " ")
	for _, want := range []string{"BatchMode=yes", "-p 2222", "-i C:\\keys\\id", "-J jump@bastion", "deploy@10.0.0.9"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("ssh 参数缺少 %q：%s", want, joined)
		}
	}
	// 默认端口 22
	if got := strings.Join(remoteSSHArgs(remoteHost{Host: "h", User: "u"}), " "); !strings.Contains(got, "-p 22") {
		t.Fatalf("未指定端口时应回落到 22：%s", got)
	}
}

func TestSSHQuoteEscapesSingleQuotes(t *testing.T) {
	got := sshQuote("a'b")
	want := `'a'\''b'`
	if got != want {
		t.Fatalf("引号转义错误：期望 %s 实际 %s", want, got)
	}
}
