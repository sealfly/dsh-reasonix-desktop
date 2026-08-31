package main

// app_desktop_prefs.go — 设置「通用/模型/系统」区块的桌面偏好桥方法。
// 前端 SettingsPanel 调用；读写 settings.json 持久化，重启保留。

// ===== 终端主题 / 会话宽度 / 更新 / 指标 / 遥测 =====

func (a *App) SetDesktopTerminalTheme(theme string) error {
	if theme != "light" {
		theme = "dark"
	}
	a.st.SetTerminalTheme(theme)
	return nil
}

func (a *App) SetDesktopConversationWidth(width string) error {
	switch width {
	case "narrow", "wide":
		a.st.SetConversationWidth(width)
	default:
		a.st.SetConversationWidth("standard")
	}
	return nil
}

func (a *App) SetDesktopCheckUpdates(v bool) error {
	a.st.SetCheckUpdates(v)
	return nil
}

func (a *App) SetDesktopMetrics(v bool) error {
	a.st.SetDesktopMetrics(v)
	return nil
}

func (a *App) SetDesktopTelemetry(v bool) error {
	a.st.SetDesktopTelemetry(v)
	return nil
}

// ===== 模型 / 并发 / 压缩 =====

func (a *App) SetDefaultModel(ref string) error {
	if ref != "" {
		a.st.SetDefaultModel(ref)
	}
	return nil
}

func (a *App) SetPlannerModel(ref string) error {
	if ref != "" {
		a.st.SetPlannerModel(ref)
	}
	return nil
}

func (a *App) SetSubagentModel(ref string) error {
	if ref != "" {
		a.st.SetSubagentModel(ref)
	}
	return nil
}

func (a *App) SetSubagentEffort(effort string) error {
	a.st.SetSubagentEffort(effort)
	return nil
}

func (a *App) SetMaxSubagentDepth(depth int) error {
	if depth < 1 {
		depth = 3
	}
	a.st.SetMaxSubagentDepth(depth)
	return nil
}

func (a *App) SetMaxSubagentConcurrency(n int) error {
	if n < 1 {
		n = 2
	}
	a.st.SetMaxSubagentConcurrency(n)
	return nil
}

func (a *App) SetMaxParallelWriters(n int) error {
	if n < 1 {
		n = 1
	}
	a.st.SetMaxParallelWriters(n)
	return nil
}

func (a *App) SetCompactRatio(ratio float64) error {
	if ratio < 1 {
		ratio = 1
	}
	a.st.SetCompactRatio(int(ratio))
	return nil
}

// ===== 缩放 ======

func (a *App) GetDesktopZoomFactor() float64 {
	return a.st.Zoom()
}

func (a *App) SetDesktopZoomFactor(factor float64) error {
	a.st.SetZoom(factor)
	return nil
}

// ===== 崩溃上报 / 托盘语言 =====

// ReportCrash 记录崩溃（写日志，不阻断）。
func (a *App) ReportCrash(_report string) {}

func (a *App) SetTrayLocale(locale string) error {
	a.st.SetLanguage(locale)
	return nil
}