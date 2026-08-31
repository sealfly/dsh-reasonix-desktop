package main

import "testing"

// 模拟前端调用链: 切到交付模式 → 执行任务 → 接受交付 → 回到标准
func TestDeliveryModeEndToEnd(t *testing.T) {
	a := newTestApp()

	// 1. 前端: 用户切到"交付"模式
	//    (前端调用 SetQualityFloorForTab(tabID, "delivery"))
	r := a.SetQualityFloorForTab("tab-1", "delivery")
	if r["ok"] != true || r["qualityFloor"] != "delivery" {
		t.Fatalf("切交付模式失败: %v", r)
	}
	// 持久化确认
	if a.st.QualityFloor() != "delivery" {
		t.Fatalf("qualityFloor 未持久化: %v", a.st.QualityFloor())
	}

	// 2. 模拟 DSH 执行任务(写文件等)- 交付模式要求验证
	//    (这由前端编排, 这里验证状态一致)

	// 3. 前端: 用户点"接受交付"
	//    (前端调用 AcceptDeliveryToTab(tabID))
	ar := a.AcceptDeliveryToTab("tab-1")
	if ar["ok"] != true || ar["accepted"] != true {
		t.Fatalf("接受交付失败: %v", ar)
	}
	// 接受后回到标准
	if a.st.QualityFloor() != "standard" {
		t.Fatalf("接受交付后应为 standard: %v", a.st.QualityFloor())
	}

	// 4. 再次切交付 → 会话级
	r2 := a.SetQualityFloor("delivery")
	if r2["qualityFloor"] != "delivery" {
		t.Fatalf("会话级切交付失败: %v", r2)
	}
	// 接受
	a.AcceptDelivery()
	if a.st.QualityFloor() != "standard" {
		t.Fatalf("AcceptDelivery 后应为 standard")
	}
}
