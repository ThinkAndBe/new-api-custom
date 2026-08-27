package controller

// qiniu_models.go — 七牛模型目录（https://www.qiniu.com/ai/models）抓取与解析。
//
// 该页为服务端渲染，完整模型目录（99+ 条，含参数与价格）内嵌在 HTML 的
// JSON 里：每条形如 {"id":"deepseek/deepseek-v3.2-251201","name":...,
// "model_constraints":{...},"architecture":{...},"pricing_rules_v2":[...]}。
// 这里做字符串感知的花括号配对扫描提取，不依赖页面的 state 包装结构。

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	qiniuModelsPageURL = "https://www.qiniu.com/ai/models"
	qiniuCacheTTL      = 10 * time.Minute
)

// QiniuModelInfo 从七牛目录解析出的单模型信息。价格为 元/1M tokens，
// 0 表示目录里没有该项（不覆盖本地）。
type QiniuModelInfo struct {
	ContextLen int
	MaxOut     int
	Tool       bool
	Vision     bool
	Reasoning  bool
	InPrice    float64
	OutPrice   float64
	CachePrice float64
}

type qiniuPriceUnit struct {
	UnitName  string  `json:"unit_name"`
	UnitSize  float64 `json:"unit_size"`
	UnitPrice float64 `json:"unit_price"`
}

type qiniuRawModel struct {
	Id               string `json:"id"`
	Name             string `json:"name"`
	ModelConstraints struct {
		ContextLength        int `json:"context_length"`
		MaxCompletionTokens  int `json:"max_completion_tokens"`
		MaxTokens            int `json:"max_tokens"`
		MaxDefaultCompletion int `json:"max_default_completion_tokens"`
	} `json:"model_constraints"`
	Architecture struct {
		InputModalities []string `json:"input_modalities"`
		FunctionCalling struct {
			Supported bool `json:"supported"`
		} `json:"function_calling"`
		Reasoning struct {
			Supported bool `json:"supported"`
		} `json:"reasoning"`
	} `json:"architecture"`
	PricingRulesV2 []struct {
		DetailsV2 map[string]qiniuPriceUnit `json:"details_v2"`
	} `json:"pricing_rules_v2"`
}

var (
	qiniuCacheMu   sync.Mutex
	qiniuCacheData map[string]QiniuModelInfo
	qiniuCacheAt   time.Time
)

// FetchQiniuModels 拉取七牛模型目录（10 分钟内存缓存）。
func FetchQiniuModels() (map[string]QiniuModelInfo, error) {
	qiniuCacheMu.Lock()
	defer qiniuCacheMu.Unlock()
	if qiniuCacheData != nil && time.Since(qiniuCacheAt) < qiniuCacheTTL {
		return qiniuCacheData, nil
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", qiniuModelsPageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 new-api/qiniu-sync")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求七牛模型页失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("七牛模型页返回状态 %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	result := make(map[string]QiniuModelInfo)
	for _, raw := range scanQiniuModelObjects(body) {
		info := parseQiniuModel(raw)
		if info.ContextLen > 0 || info.InPrice > 0 || info.OutPrice > 0 {
			result[raw.Id] = info
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("七牛模型页解析到 0 条数据（页面结构可能已变化）")
	}
	qiniuCacheData = result
	qiniuCacheAt = time.Now()
	return result, nil
}

// LookupQiniuModel 按模型名查找：先精确匹配（含厂商前缀全名），再按
// 「厂商/模型名」后缀匹配（本地 doubao-seed-evolving ↔ 七牛 volcengine/doubao-seed-evolving）。
func LookupQiniuModel(catalog map[string]QiniuModelInfo, name string) (QiniuModelInfo, bool) {
	if info, ok := catalog[name]; ok {
		return info, true
	}
	suffix := "/" + name
	for id, info := range catalog {
		if strings.HasSuffix(id, suffix) {
			return info, true
		}
	}
	return QiniuModelInfo{}, false
}

// scanQiniuModelObjects 从页面 HTML 中提取模型 JSON 对象。
// 用字符串感知扫描配对花括号，避免中文描述里的花括号干扰。
func scanQiniuModelObjects(body []byte) []qiniuRawModel {
	s := string(body)
	var models []qiniuRawModel
	for i := 0; i < len(s); i++ {
		if s[i] != '{' || !strings.HasPrefix(s[i:], `{"id":"`) {
			continue
		}
		end := matchQiniuBrace(s, i)
		if end < 0 {
			continue
		}
		var raw qiniuRawModel
		if err := common.Unmarshal([]byte(s[i:end+1]), &raw); err == nil &&
			raw.Id != "" && (raw.ModelConstraints.ContextLength > 0 || len(raw.PricingRulesV2) > 0) {
			models = append(models, raw)
		}
		i = end
	}
	return models
}

// matchQiniuBrace 返回与 s[start] 的 '{' 配对的 '}' 下标，字符串内部跳过。
func matchQiniuBrace(s string, start int) int {
	depth := 0
	inStr := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			if c == '\\' {
				i++
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func parseQiniuModel(raw qiniuRawModel) QiniuModelInfo {
	info := QiniuModelInfo{
		ContextLen: raw.ModelConstraints.ContextLength,
		MaxOut:     raw.ModelConstraints.MaxCompletionTokens,
		Tool:       raw.Architecture.FunctionCalling.Supported,
		Reasoning:  raw.Architecture.Reasoning.Supported,
	}
	if info.MaxOut == 0 {
		info.MaxOut = raw.ModelConstraints.MaxTokens
	}
	for _, modality := range raw.Architecture.InputModalities {
		if strings.Contains(modality, "image") {
			info.Vision = true
			break
		}
	}
	for _, rule := range raw.PricingRulesV2 {
		if info.InPrice == 0 {
			info.InPrice = qiniuPickPrice(rule.DetailsV2, "input", "ncache", "ncache_peak")
		}
		if info.OutPrice == 0 {
			info.OutPrice = qiniuPickPrice(rule.DetailsV2, "output", "output_peak")
		}
		if info.CachePrice == 0 {
			info.CachePrice = qiniuPickPrice(rule.DetailsV2, "cache", "cache_peak")
		}
	}
	return info
}

// qiniuPickPrice 按候选键顺序取第一个价格，换算为 元/1M tokens。
// unit_price 是 unit_size（通常 1000）个 token 的价格；0 视为无数据返回 0。
func qiniuPickPrice(details map[string]qiniuPriceUnit, keys ...string) float64 {
	for _, k := range keys {
		if p, ok := details[k]; ok && p.UnitPrice > 0 && p.UnitSize > 0 {
			return p.UnitPrice * 1_000_000 / p.UnitSize
		}
	}
	return 0
}
