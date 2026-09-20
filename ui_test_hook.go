package main

// ui_test_hook.go — 供自动化 UI 验证用的调试钩子（**默认关闭**）。
//
// 为什么需要它：Wails v2 创建 WebView2 环境时会**覆盖** WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS
// （见 go-webview2 的 preventEnvAndRegistryOverrides：`os.Setenv(..., additionalBrowserArgs)`），
// 所以 `--remote-debugging-port` 这条路被上游封死 —— CDP 连不上，没法从外部驱动界面。
// 而本项目的功能验证需要"在真界面上点一遍"（例如供应商卡片的「拉取模型 → 勾选保存」链路），
// 因此从应用内部执行 JS 并把结果回传。
//
// 启用方式（启动应用前设置）：
//
//	$env:DSH_UI_TEST_PORT = "9310"     # 端口；不设置则完全不启动钩子
//	$env:DSH_UI_TEST_TOKEN = "..."     # 可选：要求请求带 X-DSH-Token 头
//
// 端点（仅绑定 127.0.0.1）：
//
//	GET  /health                       → {ok, title, url}
//	POST /eval  {"js": "<表达式>"}      → {ok, value} 在页面里求值并回传（表达式返回 Promise 会等 settle）
//	POST /click {"selector": "..."}    → 点击匹配的第一个元素（原生 el.click()）
//
// 三个实测坑（2026-09-20 真机验证时踩到，写在这儿免得下次再花时间）：
//  1) **标记陈旧会让"点错元素"伪装成"点击无效"**：先用 /eval 给元素打 data-uit-* 标记、再单独调
//     /click 时，若上一轮的标记没清掉，`querySelector('[data-uit-x="1"]')` 命中的是**旧元素**
//     （文档序靠前的那个）。现场表现是"点了页签但页面不切换"，看着像 React 不理 el.click()。
//     实测对照（四种方式轮流切 记忆→MCP 与工具）：原生 el.click()、pointerdown+click、
//     mousedown+click、五事件序列**全部都能正确切换且 3 秒内不漂移** —— 所以 el.click() 没问题，
//     错的是标记。打标记前务必先清 `[data-uit-*]`，或干脆在**同一次 /eval** 里按文本定位并点击。
//  2) **桥里不存在的方法返回永不 settle 的 Promise**：例如写成 Skills（真实名是 SkillsSettings）时，
//     在 /eval 里 await 它会一直挂到 20s 超时，报"求值超时"而看不出原因。探针调用前先判
//     `typeof window.go.main.App.X === 'function'`；桥上一共 ~478 个方法，用 Object.keys 先列一遍最稳。
//  3) 同一元素连发 pointerdown/up + mousedown/up + click 会一次点击产生多个事件；本应用无副作用，
//     但对"保存/删除"这类按钮不建议这么点 —— 用原生 click() 即可。
//
// 结果回传机制：Wails 的 WindowExecJS 没有返回值，所以注入的 JS 把结果通过桥方法
// UiTestReport(nonce+json) 送回 Go，再由 /eval 按 nonce 匹配返回。

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// uiTestHook 是调试钩子的运行时状态。
type uiTestHook struct {
	mu      sync.Mutex
	waiters map[string]chan uiTestResult
	token   string
	srv     *http.Server
}

// uiTestResult 是页面回传的一条执行结果。
type uiTestResult struct {
	OK    bool            `json:"ok"`
	Value json.RawMessage `json:"value"`
	Error string          `json:"error"`
}

// startUITestHook 在 DSH_UI_TEST_PORT 设置时启动钩子（否则什么都不做）。
func (a *App) startUITestHook() {
	port := strings.TrimSpace(os.Getenv("DSH_UI_TEST_PORT"))
	if port == "" {
		return
	}
	hook := &uiTestHook{
		waiters: map[string]chan uiTestResult{},
		token:   strings.TrimSpace(os.Getenv("DSH_UI_TEST_TOKEN")),
	}
	a.uiTest = hook

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if !hook.authorized(w, r) {
			return
		}
		// 标题由前端在出错时改写（注入的 head 错误钩子写 ERR:/REJ:），
		// 这里回一个"就绪"标志即可；窗口标题用 Win32 侧读取更可靠（见测试脚本）。
		writeJSON(w, map[string]any{"ok": true, "ready": a.ctx != nil})
	})
	mux.HandleFunc("/eval", func(w http.ResponseWriter, r *http.Request) {
		if !hook.authorized(w, r) {
			return
		}
		var req struct {
			JS    string `json:"js"`
			Async bool   `json:"async"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": "bad request: " + err.Error()})
			return
		}
		result, err := a.uiTestEval(req.JS)
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": result.OK, "value": result.Value, "error": result.Error})
	})
	mux.HandleFunc("/click", func(w http.ResponseWriter, r *http.Request) {
		if !hook.authorized(w, r) {
			return
		}
		var req struct {
			Selector string `json:"selector"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Selector == "" {
			writeJSON(w, map[string]any{"ok": false, "error": "需要 selector"})
			return
		}
		expr := fmt.Sprintf(`(function(){
  var el=document.querySelector(%q);
  if(!el) return {clicked:false,reason:"not-found"};
  if(el.disabled) return {clicked:false,reason:"disabled",tag:el.tagName,text:(el.textContent||"").trim().slice(0,80)};
  el.click();
  return {clicked:true,tag:el.tagName,text:(el.textContent||"").trim().slice(0,80),seq:"native-click"};
})()`, req.Selector)
		result, err := a.uiTestEval(expr)
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": result.OK, "value": result.Value, "error": result.Error})
	})

	addr := net.JoinHostPort("127.0.0.1", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		resumeLog("ui-test hook: 无法监听 %s: %v", addr, err)
		return
	}
	hook.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := hook.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			resumeLog("ui-test hook: %v", err)
		}
	}()
	resumeLog("ui-test hook: listening on http://%s（仅本机，DSH_UI_TEST_PORT 开启）", addr)
}

// authorized 校验可选 token。
func (h *uiTestHook) authorized(w http.ResponseWriter, r *http.Request) bool {
	if h.token == "" {
		return true
	}
	if r.Header.Get("X-DSH-Token") == h.token {
		return true
	}
	w.WriteHeader(http.StatusUnauthorized)
	writeJSON(w, map[string]any{"ok": false, "error": "token 不匹配"})
	return false
}

// writeJSON 写一个 JSON 响应。
func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// uiTestEval 在页面里求值一个 JS 表达式，并等待页面通过 UiTestReport 回传结果。
func (a *App) uiTestEval(expr string) (uiTestResult, error) {
	if a.ctx == nil {
		return uiTestResult{}, fmt.Errorf("窗口尚未就绪")
	}
	if a.uiTest == nil {
		return uiTestResult{}, fmt.Errorf("UI 测试钩子未启用（用 DSH_UI_TEST_PORT 启动）")
	}
	nonce := fmt.Sprintf("uitest-%d", time.Now().UnixNano())
	ch := make(chan uiTestResult, 1)
	a.uiTest.mu.Lock()
	a.uiTest.waiters[nonce] = ch
	a.uiTest.mu.Unlock()
	defer func() {
		a.uiTest.mu.Lock()
		delete(a.uiTest.waiters, nonce)
		a.uiTest.mu.Unlock()
	}()

	// 用 try/catch 包住求值；undefined 归一成 null 以便 JSON 序列化。
	// 表达式返回 Promise 时必须等它 settle 再回传——桥方法全是 async，
	// 否则 JSON.stringify(Promise) 会得到一个空对象，断言全是 undefined（实测踩过）。
	js := fmt.Sprintf(`(function(){
  var __n=%q;
  function __report(__r){ try{ window.go.main.App.UiTestReport(__n+JSON.stringify(__r)) }catch(e){} }
  var __v;
  try { __v=(%s) } catch(e) { __report({ok:false,error:String((e&&e.message)||e)}); return }
  if (__v && typeof __v.then==='function') {
    __v.then(function(r){ __report({ok:true,value:(typeof r==='undefined'?null:r)}) },
             function(e){ __report({ok:false,error:String((e&&e.message)||e)}) });
    return;
  }
  __report({ok:true,value:(typeof __v==='undefined'?null:__v)});
})()`, nonce, strings.TrimSpace(expr))
	wruntime.WindowExecJS(a.ctx, js)

	select {
	case result := <-ch:
		return result, nil
	case <-time.After(20 * time.Second):
		return uiTestResult{}, fmt.Errorf("求值超时（20s）：%s", truncateForMessage(expr, 120))
	}
}

// UiTestReport 接收页面回传的执行结果（仅供 ui_test_hook 使用）。
func (a *App) UiTestReport(payload string) {
	if a.uiTest == nil {
		return
	}
	idx := strings.Index(payload, "{")
	if idx <= 0 {
		return
	}
	nonce := payload[:idx]
	var result uiTestResult
	if err := json.Unmarshal([]byte(payload[idx:]), &result); err != nil {
		return
	}
	a.uiTest.mu.Lock()
	ch, ok := a.uiTest.waiters[nonce]
	a.uiTest.mu.Unlock()
	if ok {
		select {
		case ch <- result:
		default:
		}
	}
}
