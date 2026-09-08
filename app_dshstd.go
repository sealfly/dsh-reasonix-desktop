package main

// app_dshstd.go — dsh-std 协议标准支持（DSH Standard）
//
// 参考 https://github.com/Yan-Zero/dsh-std（DSH 官方互操作协议标准）与
// https://github.com/T-Auto/dsh-ecosystem-spec（社区准入规范，TUI Admission v0.15）：
//   - @dsh-std/core 定义"元协议"：apiVersion+kind 标识、requires/supports 声明、纯函数协商
//   - 领域协议（Command/Tool/Model/Session/Presentation/Connection...）建立在核心之上
//   - dsh-plugin.json（Community v0.15）是静态插件清单，市场/主机无需运行代码即可算兼容
//   - Admission v0.15 产品准入：五态协商（compatible/compatible_degraded/
//     waiting_authorization/rejected/unknown）、Host Descriptor、声明闭合
//     （declaration closure）、optional 契约必须带 fallback、未注册权限 fail-closed
//
// 本文件用 Go 实现核心协议层（不依赖 npm 包），使 DSH-ReasonixUI 成为 dsh-std 合规 Host：
//   - 协议标识/声明/协商（等价 @dsh-std/core）
//   - dsh-plugin.json 解析与校验（等价 @dsh-std/manifest 核心，Community v0.15 schema）
//   - Admission v0.15 五态准入（等价 dsh-ecosystem-spec 的 admission evaluator）
//   - Host Descriptor（TUI-HOST-001）
//   - 桥方法暴露给前端（设置→能力/插件页可查看协议状态）

import (
	"encoding/json"
	"fmt"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
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
	Fallback string `json:"fallback,omitempty"`
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
	APIVersion string               `json:"apiVersion"`
	Evaluator  EvaluatorIdentity    `json:"evaluator"`
	Compatible bool                 `json:"compatible"`
	Protocols  []NegotiatedProtocol `json:"protocols"`
	Issues     []ProtocolIssue      `json:"issues"`
}

// EvaluatorIdentity 协商器标识。
type EvaluatorIdentity struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// NegotiatedProtocol 单个协议的协商结果。
type NegotiatedProtocol struct {
	ApiReference
	Participants []string        `json:"participants"`
	Agreement    any             `json:"agreement,omitempty"`
	Issues       []ProtocolIssue `json:"issues"`
}

// ProtocolIssue 协商问题。
type ProtocolIssue struct {
	Code        string `json:"code"`
	Severity    string `json:"severity"`
	Message     string `json:"message"`
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

// ===== 协议目录（ProtocolCatalog，Admission v0.15 声明闭合） =====

// protocolCatalog 本 Host 可解析的协议坐标集合：
// dsh-std 官方领域协议 + Community v0.15 + TUI Profile 私有协议。
var protocolCatalog = map[string]bool{
	// core（元协议）
	"core.dsh/v1alpha1\x00Negotiation": true,
	"core.dsh/v1alpha1\x00Report":      true,
	// connection / session / tool / model / manifest / host
	"connection.dsh/v1alpha1\x00Connection":  true,
	"session.dsh/v1alpha1\x00Session":        true,
	"tool.dsh/v1\x00Tool":                    true,
	"model.dsh/v1\x00ModelCatalog":           true,
	"manifest.dsh/v1alpha1\x00Manifest":      true,
	"host.dsh/v1alpha1\x00Host":              true,
	// command / storage / messages / presentation / workspace（Community v0.15 域）
	"command.dsh/v1\x00CommandRuntime":          true,
	"commands.dsh/v1alpha1\x00Command":          true,
	"storage.dsh/v1alpha1\x00LocalStorage":      true,
	"storage.dsh/v1alpha1\x00Storage":           true,
	"messages.dsh/v1alpha1\x00MessageObserver":  true,
	"presentation.dsh/v1alpha1\x00Presentation": true,
	"workspace.dsh/v1alpha1\x00Workspace":       true,
	// TUI Profile 私有协议（tui.dsh/v1alpha1）
	"tui.dsh/v1alpha1\x00DecisionEvents":  true,
	"tui.dsh/v1alpha1\x00Channel":         true,
	"tui.dsh/v1alpha1\x00SettingsSection": true,
	"tui.dsh/v1alpha1\x00Scene":           true,
}

// protocolResolvable 判断协议坐标能否被本 profile 导入/拥有的 definition 解析
// （TUI-PKG-002 declaration closure）。注册表之外回答 false → unknown。
func protocolResolvable(ref ApiReference) bool {
	return protocolCatalog[protocolKey(ref)]
}

// ===== 权限注册表（Admission v0.15 授权存储语义） =====

// registeredPermissions 本 Host 的权限注册表（TUI Admission 8 权限）。
// defaultAllow: commands.invoke 默认允许，其余默认拒绝（fail-closed）。
var registeredPermissions = map[string]bool{
	"commands.invoke":         true, // 默认允许
	"session.input.intercept": false,
	"session.create":          false,
	"storage.local.read":      false,
	"storage.local.write":     false,
	"messages.observe.read":   false,
	"presentation.dialog":     false,
	"presentation.approval":   false,
}

// ===== 五态准入（Admission v0.15） =====

// AdmissionState 五态：compatible / compatible_degraded / waiting_authorization /
// rejected / unknown——优先级 unknown > rejected > waiting_authorization >
// compatible_degraded > compatible。
type AdmissionState string

const (
	StateCompatible           AdmissionState = "compatible"
	StateCompatibleDegraded   AdmissionState = "compatible_degraded"
	StateWaitingAuthorization AdmissionState = "waiting_authorization"
	StateRejected             AdmissionState = "rejected"
	StateUnknown              AdmissionState = "unknown"
)

// admissionPriority 状态优先级（越大越优先）。
var admissionPriority = map[AdmissionState]int{
	StateUnknown:              5,
	StateRejected:             4,
	StateWaitingAuthorization: 3,
	StateCompatibleDegraded:   2,
	StateCompatible:           1,
}

// admitProtocol 单个协议的准入评估。
func admitProtocol(ref ApiReference, optional bool, hasSupport bool, permissionDenied bool, resolvable bool) (AdmissionState, string) {
	if !resolvable {
		return StateUnknown, "protocol coordinate outside registry (cannot adjudicate)"
	}
	if permissionDenied {
		return StateWaitingAuthorization, "permission not granted for this protocol"
	}
	if !hasSupport {
		if optional {
			return StateCompatibleDegraded, "no participant supports this optional protocol"
		}
		return StateRejected, "no participant supports required protocol"
	}
	return StateCompatible, ""
}

// ===== 协商器（@dsh-std/core negotiate 等价） =====

// Negotiate 执行协议协商：收集所有参与者的 requires/supports，
// 对每个被要求的协议找支持者，输出兼容性报告（二态 compatible，兼容旧接口）。
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

// ===== dsh-plugin.json 解析（@dsh-std/manifest 核心，Community v0.15 schema） =====

// dshPluginManifest Community v0.15 dsh-plugin.json 结构（对齐官方 schema：
// vendor/dsh-std/packages/manifest/schema/dsh-plugin-0.15.schema.json——顶层无 supports、
// facets 仅 host 必填、$schema/manifestVersion/id/name/version/facets/requires/
// permissions/contributes/subscriptions 全部必填；未知顶层字段一律拒绝）。
type dshPluginManifest struct {
	Schema          string `json:"$schema"`
	ManifestVersion string `json:"manifestVersion"`
	ID              string `json:"id"`
	Name            string `json:"name"`
	Version         string `json:"version"`
	License         string `json:"license,omitempty"`
	Source          string `json:"source,omitempty"`
	Facets          struct {
		Host struct {
			Entry      string `json:"entry"`
			APIVersion string `json:"apiVersion"`
		} `json:"host"`
	} `json:"facets"`
	Requires struct {
		Contracts []struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			Optional   bool   `json:"optional"`
			Fallback   string `json:"fallback"`
		} `json:"contracts"`
		Services []json.RawMessage `json:"services"` // v0.15 maxItems 0 → 出现即拒绝
	} `json:"requires"`
	// 顶层必填（可为空数组）：permissions / contributes / subscriptions
	Permissions []struct {
		Name   string `json:"name"`
		Scope  string `json:"scope"`
		Reason string `json:"reason,omitempty"`
	} `json:"permissions"`
	Contributes struct {
		Commands []struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Description string `json:"description,omitempty"`
		} `json:"commands"`
		Panels []json.RawMessage `json:"panels"` // v0.15 maxItems 0 → 出现即拒绝
	} `json:"contributes"`
	Subscriptions []json.RawMessage `json:"subscriptions"`
	// 可选扩展字段（官方 schema 合法；不解析细节，仅允许存在）
	Artifact  json.RawMessage `json:"artifact,omitempty"`
	Compat    json.RawMessage `json:"compat,omitempty"`
	Overrides json.RawMessage `json:"overrides,omitempty"`
	// v0.15 直接拒绝的字段（显式捕获，避免 json 忽略后漏检）
	Provides json.RawMessage `json:"provides,omitempty"`
	Services json.RawMessage `json:"services,omitempty"`
}

// dshPluginTopAllowed v0.15 顶层合法字段白名单（unknown → reject，与官方 schema
// additionalProperties=false 一致）。
var dshPluginTopAllowed = map[string]bool{
	"$schema": true, "manifestVersion": true, "id": true, "name": true,
	"version": true, "facets": true, "requires": true, "permissions": true,
	"contributes": true, "subscriptions": true, "license": true, "source": true,
	"artifact": true, "compat": true, "overrides": true,
}

// dshNamespacedIDRe v0.15 namespacedId：小写段，至少含一个 . 或 - 分隔（com.example.x / a-b）。
var dshNamespacedIDRe = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9][a-z0-9-]*)+$`)

// validFacetEntry 校验 facets.host.entry：相对入口——非绝对路径、非盘符开头、无 .. 段
// （对应官方 schema entry 的 pattern；RE2 无 lookahead，故手写等价校验）。
func validFacetEntry(entry string) bool {
	if entry == "" || strings.ContainsAny(entry, "\\") {
		return false
	}
	if strings.HasPrefix(entry, "/") {
		return false // 绝对路径
	}
	if len(entry) >= 2 && entry[1] == ':' && ((entry[0] >= 'A' && entry[0] <= 'Z') || (entry[0] >= 'a' && entry[0] <= 'z')) {
		return false // 盘符 C:/
	}
	for _, seg := range strings.Split(entry, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// ParseDshPluginManifest 解析并校验 dsh-plugin.json（Community v0.15，对齐官方 schema）。
// 返回 {valid, manifest, issues}。
// 校验强度对齐官方 @dsh-std/manifest：未知顶层字段拒绝、顶层必填字段齐全、
// facets.host 必填(entry+apiVersion)、optional 契约必须带 fallback、命令 id 须 namespaced。
func ParseDshPluginManifest(data []byte) map[string]any {
	var m dshPluginManifest
	issues := []any{}
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]any{
			"valid": false, "manifest": nil,
			"issues": []any{map[string]any{"code": "parse-error", "severity": "error", "message": err.Error()}},
		}
	}

	// 0. 未知顶层字段 → 拒绝（官方 additionalProperties=false；description/supports 等全拒）
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err == nil {
		for k := range raw {
			if !dshPluginTopAllowed[k] {
				issues = append(issues, map[string]any{"code": "unknown-field", "severity": "error",
					"message": "community v0.15 plugin manifest contains unknown field \"" + k + "\""})
			}
		}
	}

	// 0b. 顶层 requires/services 与 provides 在 v0.15 直接拒绝
	if len(m.Provides) > 0 {
		issues = append(issues, map[string]any{"code": "rejected-field", "severity": "error", "message": "provides is rejected in manifest v0.15 (use requires.contracts + contributes)"})
	}
	if len(m.Services) > 0 {
		issues = append(issues, map[string]any{"code": "rejected-field", "severity": "error", "message": "top-level services is rejected in manifest v0.15"})
	}
	if len(m.Requires.Services) > 0 {
		issues = append(issues, map[string]any{"code": "rejected-field", "severity": "error", "message": "requires.services is rejected in manifest v0.15 (maxItems 0)"})
	}
	if len(m.Contributes.Panels) > 0 {
		issues = append(issues, map[string]any{"code": "rejected-field", "severity": "error", "message": "contributes.panels is rejected in manifest v0.15 (maxItems 0)"})
	}

	// 1. 顶层必填（schema required：$schema manifestVersion id name version facets
	//    requires permissions contributes subscriptions）
	if m.Schema == "" {
		issues = append(issues, map[string]any{"code": "missing-field", "severity": "error", "message": "$schema is required"})
	}
	if m.ManifestVersion == "" {
		issues = append(issues, map[string]any{"code": "missing-field", "severity": "error", "message": "manifestVersion is required"})
	} else if !isSupportedManifestVersion(m.ManifestVersion) {
		issues = append(issues, map[string]any{"code": "unsupported-manifestVersion", "severity": "error", "message": "manifestVersion " + m.ManifestVersion + " is not supported (expected 0.15 series)"})
	}
	if m.ID == "" {
		issues = append(issues, map[string]any{"code": "missing-field", "severity": "error", "message": "id is required"})
	} else if !dshNamespacedIDRe.MatchString(m.ID) {
		issues = append(issues, map[string]any{"code": "invalid-id", "severity": "error",
			"message": "id " + m.ID + " must be a namespaced id (lowercase, dot/hyphen separated, e.g. com.example.x)"})
	}
	if m.Name == "" {
		issues = append(issues, map[string]any{"code": "missing-field", "severity": "error", "message": "name is required"})
	}
	if m.Version == "" {
		issues = append(issues, map[string]any{"code": "missing-field", "severity": "error", "message": "version is required"})
	} else if !validSemver(m.Version) {
		issues = append(issues, map[string]any{"code": "invalid-version", "severity": "error", "message": "version " + m.Version + " is not a valid semver (expected x.y.z)"})
	}
	// permissions / contributes / subscriptions / requires 顶层必填（可空数组/空契约集）
	if !jsonFieldPresent(raw, "requires") {
		issues = append(issues, map[string]any{"code": "missing-field", "severity": "error", "message": "requires is required (contracts may be empty array)"})
	}
	if m.Permissions == nil {
		issues = append(issues, map[string]any{"code": "missing-field", "severity": "error", "message": "permissions is required (may be empty array)"})
	}
	if m.Contributes.Commands == nil && len(m.Contributes.Panels) == 0 && !jsonFieldPresent(raw, "contributes") {
		issues = append(issues, map[string]any{"code": "missing-field", "severity": "error", "message": "contributes is required (commands may be empty array)"})
	}
	if m.Subscriptions == nil {
		issues = append(issues, map[string]any{"code": "missing-field", "severity": "error", "message": "subscriptions is required (may be empty array)"})
	}

	// 2. facets：host 必填（entry + apiVersion 必填；官方 facets additionalProperties=false，
	//    无 plugin/其它 facet——对齐：仅校验 host 存在性）
	if m.Facets.Host.Entry == "" {
		issues = append(issues, map[string]any{"code": "missing-field", "severity": "error", "message": "facets.host.entry is required"})
	} else if !validFacetEntry(m.Facets.Host.Entry) {
		issues = append(issues, map[string]any{"code": "invalid-facet-entry", "severity": "error",
			"message": "facets.host.entry must be a relative path (no drive/absolute, no .. segments)"})
	}
	if m.Facets.Host.APIVersion == "" {
		issues = append(issues, map[string]any{"code": "missing-field", "severity": "error", "message": "facets.host.apiVersion is required (e.g. v1alpha1)"})
	} else if !regexp.MustCompile(`^v[1-9][0-9]*(?:(?:alpha|beta)[1-9][0-9]*)?$`).MatchString(m.Facets.Host.APIVersion) {
		issues = append(issues, map[string]any{"code": "invalid-facet-apiVersion", "severity": "error", "message": "facets.host.apiVersion must look like v1 / v1beta1 / v1alpha1"})
	}

	// 3. requires.contracts：坐标 + optional 必须带 fallback（TUI-PKG-002）
	reqContracts := []any{}
	for _, c := range m.Requires.Contracts {
		if c.APIVersion == "" || c.Kind == "" {
			issues = append(issues, map[string]any{"code": "missing-field", "severity": "error", "message": "requires.contracts entry must have apiVersion and kind"})
			continue
		}
		if _, err := parseApiVersion(c.APIVersion); err != nil {
			issues = append(issues, map[string]any{"code": "invalid-apiVersion", "severity": "error", "message": err.Error(), "path": c.APIVersion})
			continue
		}
		if c.Optional && c.Fallback == "" {
			issues = append(issues, map[string]any{"code": "missing-fallback", "severity": "error", "message": "optional contract " + c.APIVersion + "/" + c.Kind + " must declare a fallback"})
		}
		reqContracts = append(reqContracts, map[string]any{
			"apiVersion": c.APIVersion, "kind": c.Kind, "optional": c.Optional, "fallback": c.Fallback,
		})
	}

	// 4. permissions：name/scope 必填 + namespaced name + 未注册权限 fail-closed 警告
	perms := []any{}
	for _, p := range m.Permissions {
		if p.Name == "" || p.Scope == "" {
			issues = append(issues, map[string]any{"code": "missing-field", "severity": "error", "message": "permissions entry must have name and scope"})
			continue
		}
		if !dshNamespacedIDRe.MatchString(p.Name) {
			issues = append(issues, map[string]any{"code": "invalid-permission-name", "severity": "error",
				"message": "permission name " + p.Name + " must be namespaced (e.g. net.dsh.connect)"})
		}
		known := false
		for k := range registeredPermissions {
			if strings.HasPrefix(p.Name, strings.SplitN(k, ".", 2)[0]+".") {
				known = true
				break
			}
		}
		if !known {
			issues = append(issues, map[string]any{"code": "unregistered-permission", "severity": "warning", "message": "permission " + p.Name + " is not in the host permission registry"})
		}
		perms = append(perms, map[string]any{"name": p.Name, "scope": p.Scope, "reason": p.Reason})
	}

	// 5. contributes.commands：id(namespaced) + title 必填
	cmds := []any{}
	for _, c := range m.Contributes.Commands {
		if c.ID == "" || c.Title == "" {
			issues = append(issues, map[string]any{"code": "missing-field", "severity": "error", "message": "contributes.commands entry must have id and title"})
			continue
		}
		if !dshNamespacedIDRe.MatchString(c.ID) {
			issues = append(issues, map[string]any{"code": "invalid-command-id", "severity": "error",
				"message": "command id " + c.ID + " must be namespaced (e.g. com.example.x.hello)"})
		}
		cmds = append(cmds, map[string]any{"id": c.ID, "title": c.Title, "description": c.Description})
	}

	// 6. subscriptions：解析为可展示列表（string 或 {apiVersion,kind,scope}）
	subs := []any{}
	for _, rawm := range m.Subscriptions {
		var s string
		if json.Unmarshal(rawm, &s) == nil {
			subs = append(subs, map[string]any{"topic": s})
			continue
		}
		var sub struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			Scope      string `json:"scope"`
		}
		if json.Unmarshal(rawm, &sub) == nil && sub.APIVersion != "" && sub.Kind != "" {
			subs = append(subs, map[string]any{"apiVersion": sub.APIVersion, "kind": sub.Kind, "scope": sub.Scope})
		} else {
			issues = append(issues, map[string]any{"code": "invalid-subscription", "severity": "error", "message": "invalid subscription entry"})
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
			"manifestVersion": m.ManifestVersion,
			"license":         m.License, "source": m.Source,
			"facets":      map[string]any{"host": map[string]any{"entry": m.Facets.Host.Entry, "apiVersion": m.Facets.Host.APIVersion}},
			"requires":    reqContracts,
			"permissions": perms,
			"contributes": map[string]any{"commands": cmds},
			"subscriptions": subs,
		},
		"issues": issues,
	}
}

// jsonFieldPresent 判断原始 JSON 是否含某顶层键（用于区分"缺字段"与"空结构"）。
func jsonFieldPresent(raw map[string]json.RawMessage, key string) bool {
	if raw == nil {
		return false
	}
	_, ok := raw[key]
	return ok
}

// ===== Admission v0.15 准入评估（dsh-ecosystem-spec admission evaluator 等价） =====

// AdmitPlugin 对插件清单执行 Admission v0.15 五态准入。
// 输入：解析后的 manifest 结构（来自 ParseDshPluginManifest）。
// 返回 {state, compatible, issues}——state 为五态之一。
func AdmitPlugin(parsed map[string]any) (AdmissionState, bool, []any) {
	issues := []any{}
	if parsed == nil {
		return StateRejected, false, []any{map[string]any{"code": "parse-error", "severity": "error", "message": "manifest is not parseable"}}
	}
	if v, ok := parsed["valid"].(bool); ok && !v {
		return StateRejected, false, parsed["issues"].([]any)
	}

	man := parsed["manifest"].(map[string]any)
	worst := StateCompatible

	// 1. requires.contracts：声明闭合（TUI-PKG-002）+ 支持评估
	hostDecl := dshStdDeclarations()
	hostSupports := map[string]bool{}
	for _, s := range hostDecl.Supports {
		hostSupports[protocolKey(s.ApiReference)] = true
	}
	if reqs, ok := man["requires"].([]any); ok {
		for _, r := range reqs {
			rm := r.(map[string]any)
			ref := ApiReference{APIVersion: rm["apiVersion"].(string), Kind: rm["kind"].(string)}
			optional, _ := rm["optional"].(bool)
			resolvable := protocolResolvable(ref)
			// 权限门（waiting_authorization）
			permDenied := false
			for p := range rm {
				if p == "permissions" {
					permDenied = true
				}
			}
			state, msg := admitProtocol(ref, optional, hostSupports[protocolKey(ref)], permDenied, resolvable)
			se := map[string]any{
				"code": string(state), "severity": map[AdmissionState]string{
					StateCompatible: "info", StateCompatibleDegraded: "warning",
					StateWaitingAuthorization: "warning", StateRejected: "error", StateUnknown: "warning",
				}[state],
				"message": msg, "path": ref.APIVersion + "/" + ref.Kind,
			}
			issues = append(issues, se)
			if admissionPriority[state] > admissionPriority[worst] {
				worst = state
			}
		}
	}

	// 2. 权限声明：注册表外 → 保持（fail-closed 提示），不阻断但降级
	if perms, ok := man["permissions"].([]any); ok && len(perms) > 0 {
		for _, p := range perms {
			pm := p.(map[string]any)
			name, _ := pm["name"].(string)
			if _, known := registeredPermissions[name]; !known && name != "" {
				issues = append(issues, map[string]any{
					"code": "unregistered-permission", "severity": "warning",
					"message": "permission " + name + " not in host registry",
				})
			}
		}
	}

	compatible := worst == StateCompatible || worst == StateCompatibleDegraded
	return worst, compatible, issues
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
			{ApiReference: ApiReference{APIVersion: "core.dsh/v1alpha1", Kind: "Negotiation"}},
			{ApiReference: ApiReference{APIVersion: "connection.dsh/v1alpha1", Kind: "Connection"}},
			{ApiReference: ApiReference{APIVersion: "session.dsh/v1alpha1", Kind: "Session"}},
			{ApiReference: ApiReference{APIVersion: "command.dsh/v1", Kind: "CommandRuntime"}},
			{ApiReference: ApiReference{APIVersion: "tool.dsh/v1", Kind: "Tool"}},
			{ApiReference: ApiReference{APIVersion: "model.dsh/v1", Kind: "ModelCatalog"}},
			{ApiReference: ApiReference{APIVersion: "presentation.dsh/v1alpha1", Kind: "Presentation"}},
			{ApiReference: ApiReference{APIVersion: "manifest.dsh/v1alpha1", Kind: "Manifest"}},
		},
	}
}

// DshStdCapabilities 返回本客户端的 dsh-std 能力清单（协议支持 + 身份 + 信任披露）。
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
	perms := []any{}
	for name, allow := range registeredPermissions {
		perms = append(perms, map[string]any{"name": name, "defaultAllow": allow})
	}
	sort.Slice(perms, func(i, j int) bool { return perms[i].(map[string]any)["name"].(string) < perms[j].(map[string]any)["name"].(string) })
	return map[string]any{
		"implementer": "dsh-reasonix-bridge",
		"version":     "0.2.0",
		"participant": "dsh-reasonixui",
		"standard":    "dsh-std",
		"requires":    requires,
		"supports":    supports,
		"pluginManifest": "dsh-plugin.json",
		"admission": map[string]any{
			"baseline":  "Community v0.15",
			"states":    []string{"compatible", "compatible_degraded", "waiting_authorization", "rejected", "unknown"},
			"permissions": perms,
			"trust": map[string]any{
				"level":      "trusted-in-process",
				"disclosure": "Plugins run in-process with the host. Permission checks are behavioral constraints, not a security isolation boundary.",
			},
		},
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

// DshStdAdmit 对插件清单执行 Admission v0.15 五态准入。
// manifestJSON 为插件清单 JSON 字符串，返回 {state, compatible, issues}。
func (a *App) DshStdAdmit(manifestJSON string) map[string]any {
	parsed := ParseDshPluginManifest([]byte(manifestJSON))
	state, compatible, issues := AdmitPlugin(parsed)
	issuesOut := []any{}
	for _, i := range issues {
		switch v := i.(type) {
		case map[string]any:
			issuesOut = append(issuesOut, v)
		default:
			issuesOut = append(issuesOut, map[string]any{"code": "admission-issue", "severity": "warning", "message": fmt.Sprint(v)})
		}
	}
	return map[string]any{
		"state":      string(state),
		"compatible": compatible,
		"issues":     issuesOut,
	}
}

// DshStdHostDescriptor 返回本客户端的 Host Descriptor（TUI-HOST-001）。
// 精确列出 facet API、protocol supports、权限、runtime generation、headless、
// trust level 与平台。
func (a *App) DshStdHostDescriptor() map[string]any {
	decl := dshStdDeclarations()
	supports := []any{}
	for _, s := range decl.Supports {
		supports = append(supports, map[string]any{"apiVersion": s.APIVersion, "kind": s.Kind})
	}
	perms := []any{}
	for name, allow := range registeredPermissions {
		perms = append(perms, map[string]any{"name": name, "defaultAllow": allow})
	}
	sort.Slice(perms, func(i, j int) bool { return perms[i].(map[string]any)["name"].(string) < perms[j].(map[string]any)["name"].(string) })
	return map[string]any{
		"schemaVersion": "host-descriptor/0.15",
		"id":            "com.dsh-reasonix.desktop",
		"name":          "DSH-ReasonixUI",
		"version":       "0.2.0",
		"facets": map[string]any{
			"host": map[string]any{"entry": "dsh-reasonix-ui", "apiVersion": "v1alpha1"},
		},
		"protocolSupports": supports,
		"permissions":      perms,
		"runtime": map[string]any{
			"generationId": fmt.Sprintf("%x", time.Now().UnixNano()),
			"headless":     false,
		},
		"trust": map[string]any{
			"level":      "trusted-in-process",
			"disclosure": "Plugins run in-process with the host. Permission checks are behavioral constraints, not a security isolation boundary.",
		},
		"platform":         runtime.GOOS + "/" + runtime.GOARCH,
		"definitionSource": "builtin dsh-std core (Community v0.15) + dsh-reasonix profile",
	}
}

// DshStdSelfManifest 返回本客户端自己的 dsh-plugin.json 内容（官方 v0.15 形态——
// 无 description/supports；permissions/contributes/subscriptions 顶层必填；requires
// 空契约集 = registry 声明闭合可过（官方 conformance 实测 compatible，
// core.dsh#Negotiation 属协商层声明，不在 tui profile registry 的 manifest 坐标集）。
func (a *App) DshStdSelfManifest() map[string]any {
	self := map[string]any{
		"$schema": "https://dsh-std.dev/schemas/dsh-plugin-0.15.schema.json",
		"manifestVersion": "0.15",
		"id": "com.dsh-reasonix.desktop",
		"name": "DSH-ReasonixUI",
		"version": "0.2.0",
		"facets": map[string]any{
			"host": map[string]any{"entry": "dsh-reasonix-ui", "apiVersion": "v1alpha1"},
		},
		"requires":     map[string]any{"contracts": []any{}},
		"permissions":  []any{},
		"contributes":  map[string]any{"commands": []any{}},
		"subscriptions": []any{},
	}
	return self
}

// helper: strings 用于 sort/join 等（保持 import 干净）。
var _ = strings.TrimSpace
