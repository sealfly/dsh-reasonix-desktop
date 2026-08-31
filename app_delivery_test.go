package main

import "testing"

func TestQualityFloorPersistence(t *testing.T) {
	s := NewSettings()
	s.SetQualityFloor("delivery")
	if got := s.QualityFloor(); got != "delivery" {
		t.Fatalf("QualityFloor = %q, want delivery", got)
	}
	s.SetQualityFloor("standard")
	if got := s.QualityFloor(); got != "standard" {
		t.Fatalf("QualityFloor = %q, want standard", got)
	}
}

func TestAcceptDelivery(t *testing.T) {
	a := newTestApp()
	v := a.AcceptDelivery()
	if v["ok"] != true || v["accepted"] != true {
		t.Fatalf("AcceptDelivery 应返回 ok: %v", v)
	}
}
