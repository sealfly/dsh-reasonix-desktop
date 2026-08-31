package main

// app_compat.go — 插件兼容性标志（桌面端 = Reasonix 前端）。
// DSH 的主题/外观插件大多面向 DSH Web GUI 的 DOM/组件体系（dsh-client-ui-* 等），
// 桌面端渲染的是 Reasonix 前端（不渲染 DSH Web UI），因此需给用户区分兼容性：
//   native        完全兼容（后端/工具/技能类，与前端 UI 无关，桌面端透传生效）
//   partial       部分兼容（主题/皮肤/UI 增强：后端可装，外观改动不一定在桌面端可见）
//   incompatible  不兼容（明确面向 DSH Web UI 结构，桌面端不渲染该 UI）

import "strings"

// compatLevel 判断插件在桌面端的兼容性。
func compatLevel(p marketPlugin) string {
	name := strings.ToLower(p.Name)
	cat := p.Category
	text := strings.ToLower(p.Name + " " + p.En + " " + p.Zh)
	// 明确面向 DSH Web UI 结构：组件替换/注入类
	if containsAny(name, "client-ui", "webui", "web-ui", "tui", "mobile") && cat != "tools" {
		return "incompatible"
	}
	// 主题/外观/皮肤类：桌面端是 Reasonix 主题系统，DSH 皮肤不保证生效
	if cat == "theme" || cat == "ui" ||
		containsAny(name, "theme", "skin", "appearance", "syntax") ||
		containsAny(text, "主题", "皮肤", "外观", "skin", "theme") {
		return "partial"
	}
	// 其余（tools/skill/workflow/model/memory/session/notify 等）后端能力，桌面端透传生效
	return "native"
}

// compatNote 兼容性中文说明（前端展示用）。
func compatNote(level string) string {
	switch level {
	case "incompatible":
		return "面向 DSH Web 界面，桌面端不渲染，安装后外观不生效"
	case "partial":
		return "后端可安装；外观改动面向 DSH Web 界面，桌面端可能不生效"
	default:
		return "桌面端可正常使用（后端能力透传）"
	}
}
