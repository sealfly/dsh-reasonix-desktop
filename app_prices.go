package main

// 费用计算（DSH tokenUsage × prices.json 定价表 → 会话费用）。
// prices.json 是外置可编辑的定价文件（元/百万 tokens），go:embed 编译时嵌入。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// priceTable 是单个模型的定价（元/百万 tokens）。
type priceTable struct {
	CacheHit float64 `json:"cacheHit"`
	Input    float64 `json:"input"`
	Output   float64 `json:"output"`
}

// pricesFile 是 prices.json 的结构。
type pricesFile struct {
	DeepseekOfficial map[string]priceTable `json:"deepseekOfficial"`
	Relays           map[string]priceTable `json:"relays"`
}

var (
	pricesOnce sync.Once
	prices     pricesFile
)

// loadPrices 加载 prices.json（运行时读文件；失败用内嵌默认价）。
func loadPrices() pricesFile {
	pricesOnce.Do(func() {
		_ = json.Unmarshal(readPricesFile(), &prices)
	})
	return prices
}

// readPricesFile 读取 prices.json（项目目录或可执行文件旁，可编辑）。
func readPricesFile() []byte {
	candidates := []string{
		"prices.json",
		filepath.Join(".", "prices.json"),
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "prices.json"))
	}
	for _, p := range candidates {
		if b, err := os.ReadFile(p); err == nil {
			return b
		}
	}
	// 兜底：内置最小定价表（与 prices.json 的 default 一致）
	return []byte(`{"deepseekOfficial":{"default":{"cacheHit":0.02,"input":1,"output":2}},"relays":{}}`)
}

// calcCost 计算会话费用（元）：cacheHit + input + output，除以 1e6（百万 tokens）。
func calcCost(cacheHit, input, output int, provider, model string) float64 {
	p := loadPrices()
	var table priceTable
	key := ""
	for i := 0; i < len(model); i++ {
		if model[i] >= 'A' && model[i] <= 'Z' {
			key += string(model[i] + 32)
		} else {
			key += string(model[i])
		}
	}
	if t, ok := p.Relays[provider]; ok {
		table = t
	} else if t, ok := p.DeepseekOfficial[key]; ok {
		table = t
	} else if t, ok := p.DeepseekOfficial["default"]; ok {
		table = t
	} else {
		table = priceTable{CacheHit: 0.02, Input: 1, Output: 2}
	}
	return (float64(cacheHit)*table.CacheHit + float64(input)*table.Input + float64(output)*table.Output) / 1e6
}
