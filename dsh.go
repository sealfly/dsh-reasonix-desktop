package main

// DshClient — DSH 后端通用 RPC 客户端（直连 127.0.0.1:3080）
// 项目原则核心：通用透传（transparent passthrough），不设方法白名单。
// 任意 DSH 方法（含插件动态注册的）都能通过 RPC 调用，不限制 DSH 原生能力。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DshClient 是 DSH 后端的通用 RPC 客户端。
// DSH 后端是本会话共享实例（端口 3080），多客户端多路复用，绝不主动关闭它。
type DshClient struct {
	host string
	port int
	http *http.Client
}

// NewDshClient 创建 DSH 客户端（默认 127.0.0.1:3080）。
func NewDshClient(port int) *DshClient {
	return NewDshClientAt("127.0.0.1", port)
}

// NewDshClientAt 创建指向指定 host:port 的 DSH 客户端（支持用户自选 DSH 后端）。
func NewDshClientAt(host string, port int) *DshClient {
	if host == "" {
		host = "127.0.0.1"
	}
	return &DshClient{
		host: host,
		port: port,
		http: &http.Client{Timeout: 60 * time.Second},
	}
}

// rpcResponse 匹配 DSH 的 RPC 响应信封。
type rpcResponse struct {
	Result struct {
		OK    bool            `json:"ok"`
		Value json.RawMessage `json:"value"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"result"`
}

// RPC 调用 DSH 方法并返回原始 value（json.RawMessage）。
// 调用方用 DecodeRPC 把 value 解码成具体结构，或直接透传。
// 请求信封：POST /api/<method>，body={type:"client-request",rpcId,method,payload}。
func (d *DshClient) RPC(method string, payload any) (json.RawMessage, error) {
	rpcID := fmt.Sprintf("dsh-%d", time.Now().UnixNano())
	body, err := json.Marshal(map[string]any{
		"type":    "client-request",
		"rpcId":   rpcID,
		"method":  method,
		"payload": payload,
	})
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("http://%s:%d/api/%s", d.host, d.port, method)
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rpc %s: %w", method, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("rpc %s read: %w", method, err)
	}
	var parsed rpcResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("rpc %s bad response: %w", method, err)
	}
	if !parsed.Result.OK {
		if parsed.Result.Error != nil && parsed.Result.Error.Message != "" {
			return nil, fmt.Errorf("rpc %s: %s", method, parsed.Result.Error.Message)
		}
		return nil, fmt.Errorf("rpc %s failed", method)
	}
	return parsed.Result.Value, nil
}

// DecodeRPC 把 RPC 返回的原始 JSON 解码成目标结构（any 或具体类型）。
// 用于需要把 DSH 数据转换成前端期望结构的场景。
func DecodeRPC(raw json.RawMessage, target any) error {
	if raw == nil {
		return fmt.Errorf("empty rpc value")
	}
	return json.Unmarshal(raw, target)
}

// RPCAny 调用 RPC 并把 value 解码成 any（map[string]any / []any），
// 直接透传给前端（Wails 序列化为 JSON）。
func (d *DshClient) RPCAny(method string, payload any) (any, error) {
	raw, err := d.RPC(method, payload)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return v, nil
}
