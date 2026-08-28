package main

// app_dshstd.go — dsh-std 协议标准支持（DSH Standard）
//
// 参考 https://github.com/Yan-Zero/dsh-std（DSH 官方互操作协议标准）：
//   - @dsh-std/core 定义"元协议"：apiVersion+kind 标识、requires/supports 声明、纯函数协商
//   - 领域协议（Command/Tool/Model/Session/Presentation/Connection...）建立在核心之上
//   - dsh-plugin.json（Community v0.15）是静态插件清单，市场/主机无需运行代码即可算兼容
//
// 本文件用 Go 实现核心协议层（不依赖 npm 包），使 DSH-ReasonixUI 成为 dsh-std 合规 Host：
//   - 协议标识/声明/协商（等价 @dsh-std/core）
//   - dsh-plugin.json 解析与校验（等价 @dsh-std/manifest 核心）
//   - 桥方法暴露给前端（设置→能力/插件页可查看协议状态）

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ===== 协议核心类型（@dsh-std/core 等价） =====

// ApiReference 协议标识：apiVersion + kind。
type ApiReference struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
}

// ProtocolRequirement 协议需求声明。
type ProtocolRequirement struct {
	ApiReference
	Optional bool   `json:"optional,omitempty"`
	Spec     any    `json:"spec,omitempty"`
}

// ProtocolSupport 协议支持声明。
type ProtocolSupport struct {
	ApiReference
	Spec any `json:"spec,omitempty"`
}

// ProtocolDeclaration 参与者协议声明。
type ProtocolDeclaration struct {
	Participant ParticipantIdentity   `json:"participant"`
	Requires    []ProtocolRequirement `json:"requires,omitempty"`
	Supports    []ProtocolSupport     `json:"supports,omitempty"`
}

// ParticipantIdentity 参与者标识（协商作用域内唯一）。
type ParticipantIdentity struct {
	ID string `json:"id"`
}

// NegotiationReport 协商报告（core.dsh/report/v1alpha1）。
type NegotiationReport struct {
	APIVersion string                 `json:"apiVersion"`
	Evaluator  EvaluatorIdentity      `json:"evaluator"`
	Compatible bool                   `json:"compatible"`
	Protocols  []NegotiatedProtocol   `json:"protocols"`
	Issues     []ProtocolIssue        `json:"issues"`
}

// EvaluatorIdentity 协商器标识。
type EvaluatorIdentity struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// NegotiatedProtocol 单个协议的协商结果。
type NegotiatedProtocol struct {
	ApiReference
	Participants []string       `json:"participants"`
	Agreement    any            `json:"agreement,omitempty"`
	Issues       []ProtocolIssue `json:"issues"`
}

// ProtocolIssue 协商问题。
type ProtocolIssue struct {
	Code      string `json:"code"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Participant string `json:"participant,omitempty"`
}

// ===== apiVersion 解析 =====

var apiVersionRe = regexp.MustCompile(`^([a-z][a-z0-9.-]*)/v([1-9][0-9]*)(?:(alpha|beta)([1-9][0-9]*))?$`)

// parsedApiVersion 解析后的 apiVersion。
type parsedApiVersion struct {
	Group     string
	Major     int
	Stability string // stable | beta | alpha
	Revision  int
}

// parseApiVersion 解析 apiVersion 字符串。
func parseApiVersion(value string) (parsedApiVersion, error) {
	m := apiVersionRe.FindStringSubmatch(value)
	if m == nil {
		return parsedApiVersion{}, fmt.Errorf("invalid apiVersion %q (expected group/v1, group/v1beta1, group/v1alpha1)", value)
	}
	major := 0
	fmt.Sscanf(m[2], "%d", &major)
	stability := "stable"
	if m[3] != "" {
		stability = m[3]
	}
	revision := 0
	if m[4] != "" {
		fmt.Sscanf(m[4], "%d", &revision)
	}
	return parsedApiVersion{Group: m[1], Major: major, Stability: stability, Revision: revision}, nil
}

// protocolKey 协议唯一键。
func protocolKey(ref ApiReference) string {
	return ref.APIVersion + "\x00" + ref.Kind
}

// ===== 插件版本语义（dsh-std 版本管理：semver + manifestVersion 兼容） =====

// semverRe 宽松 semver：x.y.z（可带 -prerelease / +build 后缀）。
var semverRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

// validSemver 判断版本号是否为合法 semver（dsh-plugin.json 的 version 字段约定）。
func validSemver(v string) bool {
	return semverRe.MatchString(v)
}

// supportedManifestVersions DSH 生态当前接受的 dsh-plugin.json 清单格式版本
// （Community v0.15 基线；0.15 系列兼容）。
var supportedManifestVersions = []string{"0.15", "0.16", "0.17", "0.18"}

// isSupportedManifestVersion 判断 manifestVersion 是否在支持的 0.x 系列内
// （0.15 基线向上兼容，用于插件准入的版本协商）。
func isSupportedManifestVersion(v string) bool {
	for _, s := range supportedManifestVersions {
		if v == s {
			return true
		}
	}
	return false
}

// sameProtocol 判断两个引用是否同一协议。
func sameProtocol(a, b ApiReference) bool {
	return a.APIVersion == b.APIVersion && a.Kind == b.Kind
}

// ===== 协商器（@dsh-std/core negotiate 等价） =====

// Negotiate 执行协议协商：收集所有参与者的 requires/supports，
// 对每个被要求的协议找支持者，输出兼容性报告。
func Negotiate(declarations []ProtocolDeclaration) NegotiationReport {
	report := NegotiationReport{
		APIVersion: "core.dsh/report/v1alpha1",
		Evaluator:  EvaluatorIdentity{Name: "dsh-reasonix-bridge", Version: "0.2.0"},
		Protocols:  []NegotiatedProtocol{},
		Issues:     []ProtocolIssue{},
	}

	// 收集所有 requires（按协议键分组）
	type reqEntry struct {
		participant string
		req         ProtocolRequirement
	}
	type supEntry struct {
		participant string
		sup         ProtocolSupport
	}
	reqs := map[string][]reqEntry{}
	sups := map[string][]supEntry{}

	for _, d := range declarations {
		for _, r := range d.Requires {
			key := protocolKey(r.ApiReference)
			reqs[key] = append(reqs[key], reqEntry{d.Participant.ID, r})
		}
		for _, s := range d.Supports {
			key := protocolKey(s.ApiReference)
			sups[key] = append(sups[key], supEntry{d.Participant.ID, s})
		}
	}

	// 对每个需求协议协商
	for key, reqList := range reqs {
		firstReq := reqList[0].req
		np := NegotiatedProtocol{
			ApiReference: ApiReference{APIVersion: firstReq.APIVersion, Kind: firstReq.Kind},
			Participants: []string{},
			Issues:       []ProtocolIssue{},
		}
		// 支持者
		supporters := sups[key]
		if len(supporters) == 0 {
			// 检查是否 optional
			allOptional := true
			for _, e := range reqList {
				if !e.req.Optional {
					allOptional = false
				}
			}
			// 本端基础设施 requires（dsh-reasonixui）依赖 DSH 后端，未在协商集内匹配时降级 warning
			hostOnly := true
			for _, e := range reqList {
				if e.participant != "dsh-reasonixui" {
					hostOnly = false
				}
			}
			severity := "error"
			msg := "no participant supports this protocol"
			if allOptional || hostOnly {
				severity = "warning"
				if allOptional {
					msg = "no participant supports this optional protocol"
				} else {
					msg = "host infrastructure protocol (resolved by DSH backend)"
				}
			}
			issue := ProtocolIssue{Code: "unsupported-protocol", Severity: severity, Message: msg}
			np.Issues = append(np.Issues, issue)
			report.Issues = append(report.Issues, issue)
		} else {
			// 匹配成功：需求方 + 支持方都是参与者
			seen := map[string]bool{}
			for _, e := range reqList {
				if !seen[e.participant] {
					np.Participants = append(np.Participants, e.participant)
					seen[e.participant] = true
				}
			}
			for _, e := range supporters {
				if !seen[e.participant] {
					np.Participants = append(np.Participants, e.participant)
					seen[e.participant] = true
				}
			}
			sort.Strings(np.Participants)
			np.Agreement = map[string]any{
				"status": "matched",
				"providers": func() []string {
					out := []string{}
					for _, e := range supporters {
						out = append(out, e.participant)
					}
					sort.Strings(out)
					return out
				}(),
			}
		}
		report.Protocols = append(report.Protocols, np)
	}

	// 兼容性：无 error 级 issue
	report.Compatible = true
	for _, i := range report.Issues {
		if i.Severity == "error" {
			report.Compatible = false
			break
		}
	}

	// 排序保持确定性
	sort.Slice(report.Protocols, func(i, j int) bool {
		return report.Protocols[i].APIVersion+report.Protocols[i].Kind <
			report.Protocols[j].APIVersion+report.Protocols[j].Kind
	})
	return report
}

// ===== dsh-plugin.json 解析（@dsh-std/manifest 核心） =====

// dshPluginManifest Community v0.15 dsh-plugin.json 结构。
type dshPluginManifest struct {
	Schema          string `json:"$schema"`
	ManifestVersion string `json:"manifestVersion"`
	ID              string `json:"id"`
	Name            string `json:"name"`
	Version         string `json:"version"`
	Description     string `json:"description,omitempty"`
	Facets          struct {
		Host struct {
			Entry     string `json:"entry"`
			APIVersion string `json:"apiVersion"`
		} `json:"host"`
	} `json:"facets"`
	Requires    json.RawMessage `json:"requires,omitempty"`
	Supports    json.RawMessage `json:"supports,omitempty"`
	Permissions []struct {
		Name   string `json:"name"`
		Scope  string `json:"scope"`
		Reason string `json:"reason,omitempty"`
	} `json:"permissions,omitempty"`
}

// manifestContracts 从 requires/supports 提取协议契约。
type manifestContracts struct {
	Contracts []ApiReference `json:"contracts,omitempty"`
}

// ParseDshPluginManifest 解析并校验 dsh-plugin.json。
// 返回 {valid, manifest, issues}。
func ParseDshPluginManifest(data []byte) map[string]any {
	var m dshPluginManifest
	issues := []any{}
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]any{
			"valid": false, "manifest": nil,
			"issues": []any{map[string]any{"code": "parse-error", "severity": "error", "message": err.Error()}},
		}
	}
	// 校验必填字段
	if m.ManifestVersion == "" {
		issues = append(issues, map[string]any{"code": "missing-field", "severity": "error", "message": "manifestVersion is required"})
	} else if !isSupportedManifestVersion(m.ManifestVersion) {
		issues = append(issues, map[string]any{"code": "unsupported-manifestVersion", "severity": "error", "message": "manifestVersion " + m.ManifestVersion + " is not supported (expected 0.15 series)"})
	}
	if m.ID == "" {
		issues = append(issues, map[string]any{"code": "missing-field", "severity": "error", "message": "id is required"})
	}
	if m.Name == "" {
		issues = append(issues, map[string]any{"code": "missing-field", "severity": "error", "message": "name is required"})
	}
	if m.Version == "" {
		issues = append(issues, map[string]any{"code": "missing-field", "severity": "error", "message": "version is required"})
	} else if !validSemver(m.Version) {
		issues = append(issues, map[string]any{"code": "invalid-version", "severity": "error", "message": "version " + m.Version + " is not a valid semver (expected x.y.z)"})
	}
	if m.Facets.Host.Entry == "" {
		issues = append(issues, map[string]any{"code": "missing-field", "severity": "warning", "message": "facets.host.entry is missing (headless plugin)"})
	}

	// 提取 requires/supports 契约
	contracts := []any{}
	for _, raw := range []json.RawMessage{m.Requires, m.Supports} {
		if len(raw) == 0 {
			continue
		}
		var mc manifestContracts
		if err := json.Unmarshal(raw, &mc); err == nil {
			for _, c := range mc.Contracts {
				// 校验 apiVersion 格式
				if _, err := parseApiVersion(c.APIVersion); err == nil {
					contracts = append(contracts, map[string]any{
						"apiVersion": c.APIVersion, "kind": c.Kind,
					})
				} else {
					issues = append(issues, map[string]any{
						"code": "invalid-apiVersion", "severity": "error",
						"message": err.Error(), "path": c.APIVersion,
					})
				}
			}
		}
	}

	valid := true
	for _, i := range issues {
		if i.(map[string]any)["severity"] == "error" {
			valid = false
			break
		}
	}
	return map[string]any{
		"valid": valid, "manifest": map[string]any{
			"id": m.ID, "name": m.Name, "version": m.Version,
			"description": m.Description, "manifestVersion": m.ManifestVersion,
			"facets": map[string]any{"host": map[string]any{"entry": m.Facets.Host.Entry, "apiVersion": m.Facets.Host.APIVersion}},
			"contracts": contracts,
		},
		"issues": issues,
	}
}

// ===== 桥方法（前端可调用） =====

// dshStdDeclarations 本客户端的协议声明（dsh-std Host 身份）。
func dshStdDeclarations() ProtocolDeclaration {
	return ProtocolDeclaration{
		Participant: ParticipantIdentity{ID: "dsh-reasonixui"},
		Requires: []ProtocolRequirement{
			{ApiReference: ApiReference{APIVersion: "core.dsh/v1alpha1", Kind: "Negotiation"}},
			{ApiReference: ApiReference{APIVersion: "connection.dsh/v1alpha1", Kind: "Connection"}},
			{ApiReference: ApiReference{APIVersion: "session.dsh/v1alpha1", Kind: "Session"}},
			{ApiReference: ApiReference{APIVersion: "tool.dsh/v1", Kind: "Tool"}},
		},
		Supports: []ProtocolSupport{
			{ApiReference: ApiReference{APIVersion: "command.dsh/v1", Kind: "CommandRuntime"}},
			{ApiReference: ApiReference{APIVersion: "tool.dsh/v1", Kind: "Tool"}},
			{ApiReference: ApiReference{APIVersion: "model.dsh/v1", Kind: "ModelCatalog"}},
			{ApiReference: ApiReference{APIVersion: "presentation.dsh/v1alpha1", Kind: "Presentation"}},
			{ApiReference: ApiReference{APIVersion: "manifest.dsh/v1alpha1", Kind: "Manifest"}},
		},
	}
}

// DshStdCapabilities 返回本客户端的 dsh-std 能力清单（协议支持 + 身份）。
// 前端设置页可展示。
func (a *App) DshStdCapabilities() map[string]any {
	decl := dshStdDeclarations()
	supports := []any{}
	for _, s := range decl.Supports {
		supports = append(supports, map[string]any{"apiVersion": s.APIVersion, "kind": s.Kind})
	}
	requires := []any{}
	for _, r := range decl.Requires {
		requires = append(requires, map[string]any{
			"apiVersion": r.APIVersion, "kind": r.Kind, "optional": r.Optional,
		})
	}
	return map[string]any{
		"implementer": "dsh-reasonix-bridge",
		"version":     "0.2.0",
		"participant": "dsh-reasonixui",
		"standard":    "dsh-std",
		"requires":    requires,
		"supports":    supports,
		"pluginManifest": "dsh-plugin.json",
	}
}

// DshStdNegotiate 与对端协议声明协商。
// declarations 为 JSON 数组（[{participant:{id}, requires:[], supports:[]}]）。
// 返回 NegotiationReport（compatible/protocols/issues）。
func (a *App) DshStdNegotiate(declarationsJSON string) map[string]any {
	// 解析输入
	var decls []ProtocolDeclaration
	if err := json.Unmarshal([]byte(declarationsJSON), &decls); err != nil {
		return map[string]any{
			"apiVersion": "core.dsh/report/v1alpha1",
			"compatible": false,
			"protocols":  []any{},
			"issues": []any{map[string]any{
				"code": "parse-error", "severity": "error", "message": err.Error(),
			}},
		}
	}
	// 加上本端声明
	decls = append(decls, dshStdDeclarations())
	report := Negotiate(decls)
	// 转成前端友好结构
	protocols := []any{}
	for _, p := range report.Protocols {
		protocols = append(protocols, map[string]any{
			"apiVersion": p.APIVersion, "kind": p.Kind,
			"participants": p.Participants, "agreement": p.Agreement,
			"issues": p.Issues,
		})
	}
	issues := []any{}
	for _, i := range report.Issues {
		issues = append(issues, map[string]any{
			"code": i.Code, "severity": i.Severity, "message": i.Message,
			"participant": i.Participant,
		})
	}
	return map[string]any{
		"apiVersion": report.APIVersion,
		"evaluator":  map[string]any{"name": report.Evaluator.Name, "version": report.Evaluator.Version},
		"compatible": report.Compatible,
		"protocols":  protocols,
		"issues":     issues,
	}
}

// DshStdParseManifest 解析并校验 dsh-plugin.json。
// manifestJSON 为插件清单 JSON 字符串，返回 {valid, manifest, issues}。
func (a *App) DshStdParseManifest(manifestJSON string) map[string]any {
	return ParseDshPluginManifest([]byte(manifestJSON))
}

// DshStdSelfManifest 返回本客户端自己的 dsh-plugin.json 内容。
func (a *App) DshStdSelfManifest() map[string]any {
	self := map[string]any{
		"$schema": "https://dsh-std.dev/schemas/dsh-plugin-0.15.schema.json",
		"manifestVersion": "0.15",
		"id": "com.dsh-reasonix/desktop",
		"name": "DSH-ReasonixUI",
		"version": "0.2.0",
		"description": "DSH-Reasonix 桌面客户端（Wails）——dsh-std 合规 Host",
		"facets": map[string]any{
			"host": map[string]any{"entry": "dsh-reasonix-ui", "apiVersion": "host.dsh/v1alpha1"},
		},
		"requires": map[string]any{
			"contracts": []any{
				map[string]any{"apiVersion": "core.dsh/v1alpha1", "kind": "Negotiation"},
				map[string]any{"apiVersion": "connection.dsh/v1alpha1", "kind": "Connection"},
				map[string]any{"apiVersion": "session.dsh/v1alpha1", "kind": "Session"},
			},
		},
		"supports": map[string]any{
			"contracts": []any{
				map[string]any{"apiVersion": "command.dsh/v1", "kind": "CommandRuntime"},
				map[string]any{"apiVersion": "tool.dsh/v1", "kind": "Tool"},
				map[string]any{"apiVersion": "model.dsh/v1", "kind": "ModelCatalog"},
				map[string]any{"apiVersion": "presentation.dsh/v1alpha1", "kind": "Presentation"},
				map[string]any{"apiVersion": "manifest.dsh/v1alpha1", "kind": "Manifest"},
			},
		},
	}
	return self
}

// helper: strings 用于 sort/join 等（保持 import 干净）。
var _ = strings.TrimSpace
