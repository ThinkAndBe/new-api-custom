package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// litellm 模型元数据 JSON URL（社区维护，每周更新）
const litellmModelParamsURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

// litellmModelEntry 对应 litellm 的 model_prices_and_context_window.json 中每个模型的子对象。
// 只取我们关心的字段，其余忽略。
// 注意：litellm 部分条目（如 sample_spec）的字段值可能是字符串而非数字/布尔，
// 用 *json.Number/*bool 会直接报错；因此这里用 json.RawMessage 延迟解析并按需宽容处理。
type litellmModelEntry struct {
	MaxInputTokens          json.RawMessage `json:"max_input_tokens,omitempty"`
	MaxOutputTokens         json.RawMessage `json:"max_output_tokens,omitempty"`
	MaxTokens               json.RawMessage `json:"max_tokens,omitempty"`
	SupportsFunctionCalling json.RawMessage `json:"supports_function_calling,omitempty"` // litellm 实际字段名
	SupportsVision          json.RawMessage `json:"supports_vision,omitempty"`
	SupportsReasoning       json.RawMessage `json:"supports_reasoning,omitempty"`        // litellm 直接有 reasoning
	SupportsResponseSchema  json.RawMessage `json:"supports_response_schema,omitempty"`  // 兜底推理能力
}

// rawToInt 安全地把任何 JSON 原始值（数字或字符串数字）解析为 int。
// 无法解析（如纯说明文字）返回 (0, false)。
func rawToInt(raw json.RawMessage) (int, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	// 去掉引号（字符串数字）
	s := string(raw)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	// 尝试 int
	var i int64
	if err := common.Unmarshal([]byte(s), &i); err == nil {
		return int(i), true
	}
	// 尝试 float
	var f float64
	if err := common.Unmarshal([]byte(s), &f); err == nil {
		return int(f), true
	}
	return 0, false
}

// rawToBool 安全地把 JSON 原始值解析为 bool。
// 接受 true/false。无法解析返回 (false, false)。
func rawToBool(raw json.RawMessage) (bool, bool) {
	if len(raw) == 0 {
		return false, false
	}
	s := strings.ToLower(strings.Trim(string(raw), `"`))
	switch s {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

// fetchLitellmModelParams 拉取并解析 litellm 模型参数 JSON。
// 返回的 map 键是模型名（如 "gpt-4o"），值是对应参数。
func fetchLitellmModelParams() (map[string]litellmModelEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", litellmModelParamsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}
	req.Header.Set("User-Agent", "new-api/model-params-sync")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 litellm 失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("litellm 返回状态 %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	// litellm JSON 是 {"model_name": {...}, "sample_spec": {...}, ...}
	// 用 map 直接反序列化，忽略非对象条目
	var raw map[string]litellmModelEntry
	if err := common.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析 litellm JSON 失败: %v", err)
	}
	// 过滤掉 sample_spec 等文档条目（所有字段均为空/nil）
	for k, v := range raw {
		if v.MaxInputTokens == nil && v.MaxOutputTokens == nil && v.MaxTokens == nil &&
			v.SupportsFunctionCalling == nil && v.SupportsVision == nil &&
			v.SupportsReasoning == nil && v.SupportsResponseSchema == nil {
			delete(raw, k)
		}
	}
	return raw, nil
}

// lookupLitellmEntry 在 litellm 参数表里查找模型对应的条目。
// 匹配优先级：
//  1. 精确匹配模型名（如 "gpt-4o"）
//  2. 厂商前缀匹配（如 "zhipu/glm-5.2" 以 "/glm-5.2" 结尾）
//  3. 版本号规范化匹配（litellm 常把 5.2 写作 5p2，如 "glm-5p2"）
// 返回命中的条目；多个候选时优先精确，再取第一个前缀命中。
func lookupLitellmEntry(litellmParams map[string]litellmModelEntry, modelName string) (litellmModelEntry, bool) {
	if entry, ok := litellmParams[modelName]; ok {
		return entry, true
	}
	lowerName := strings.ToLower(modelName)
	// 版本号归一：把小数点换成 p（litellm 习惯写法：glm-5p2 / kimi-k2p5）
	pName := strings.ReplaceAll(lowerName, ".", "p")
	suffixes := []string{"/" + lowerName}
	if pName != lowerName {
		suffixes = append(suffixes, "/"+pName)
	}
	for key, entry := range litellmParams {
		lk := strings.ToLower(key)
		for _, suf := range suffixes {
			if strings.HasSuffix(lk, suf) {
				return entry, true
			}
		}
	}
	return litellmModelEntry{}, false
}

// SyncModelParamsFromLitellm 管理员接口：从 litellm 拉取模型参数，更新本库 models 表。
// 模型白名单语义：不再自动登记未注册模型——模型管理由「同步官方模型」+手动添加控制，
// 未注册的模型用「缺失模型」工具（GET /api/models/missing）发现后手动补。
// - 仅更新 ParamsLocked=false 的行（已人工编辑的模型跳过，避免覆盖）。
// POST /api/models/sync_params
func SyncModelParamsFromLitellm(c *gin.Context) {
	litellmParams, err := fetchLitellmModelParams()
	if err != nil {
		common.ApiErrorMsg(c, "拉取 litellm 参数失败: "+err.Error())
		return
	}

	// 取本库所有模型（含未禁用，软删的排除）
	var models []model.Model
	if err := model.DB.Find(&models).Error; err != nil {
		common.ApiErrorMsg(c, "查询本地模型失败: "+err.Error())
		return
	}

	updated := 0
	skippedLocked := 0
	notFoundInLitellm := 0

	for _, m := range models {
		if m.ParamsLocked {
			skippedLocked++
			continue
		}
		entry, ok := lookupLitellmEntry(litellmParams, m.ModelName)
		if !ok {
			notFoundInLitellm++
			continue
		}
		updates := map[string]interface{}{}
		if v, ok := rawToInt(entry.MaxInputTokens); ok {
			updates["max_input_tokens"] = v
		}
		if v, ok := rawToInt(entry.MaxOutputTokens); ok {
			updates["max_output_tokens"] = v
		} else if v, ok2 := rawToInt(entry.MaxTokens); ok2 && len(entry.MaxOutputTokens) == 0 {
			// 部分模型只有 max_tokens（输入+输出共享上限），用它作为输出上限的近似值
			updates["max_output_tokens"] = v
		}
		if v, ok := rawToBool(entry.SupportsFunctionCalling); ok {
			updates["supports_tool_call"] = v
		}
		if v, ok := rawToBool(entry.SupportsVision); ok {
			updates["supports_images"] = v
		}
		if v, ok := rawToBool(entry.SupportsReasoning); ok {
			updates["supports_reasoning"] = v
		} else if v, ok2 := rawToBool(entry.SupportsResponseSchema); ok2 {
			// 没有 reasoning 字段时退而求其次用 response_schema 近似
			updates["supports_reasoning"] = v
		}
		if len(updates) == 0 {
			notFoundInLitellm++
			continue
		}
		if err := model.DB.Model(&model.Model{}).Where("id = ?", m.Id).Updates(updates).Error; err != nil {
			common.SysError(fmt.Sprintf("SyncModelParams 更新模型 %s (id=%d) 失败: %s", m.ModelName, m.Id, err.Error()))
			continue
		}
		updated++
	}

	common.SysLog(fmt.Sprintf("模型参数同步完成: 更新 %d, 跳过锁定 %d, litellm 未覆盖 %d, 总计 %d",
		updated, skippedLocked, notFoundInLitellm, len(models)))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("已更新 %d 个模型参数", updated),
		"data": gin.H{
			"updated":            updated,
			"skipped_locked":     skippedLocked,
			"not_found":          notFoundInLitellm,
			"total":              len(models),
			"litellm_model_count": len(litellmParams),
		},
	})
}

// modelParamsDiffItem 预览单个模型的参数变更
type modelParamsDiffItem struct {
	ModelId          int    `json:"model_id"`
	ModelName        string `json:"model_name"`
	ParamsLocked     bool   `json:"params_locked"`
	WillUpdate       bool   `json:"will_update"`
	CurrentMaxIn     int    `json:"current_max_input_tokens"`
	CurrentMaxOut    int    `json:"current_max_output_tokens"`
	NewMaxIn         int    `json:"new_max_input_tokens"`
	NewMaxOut        int    `json:"new_max_output_tokens"`
	NewTools         bool   `json:"new_supports_tool_call"`
	NewVision        bool   `json:"new_supports_images"`
	NewReasoning     bool   `json:"new_supports_reasoning"`
}

// PreviewModelParamsDiff 管理员接口：预览 litellm 同步将带来的变更（不写库）。
// GET /api/models/sync_params/preview
func PreviewModelParamsDiff(c *gin.Context) {
	litellmParams, err := fetchLitellmModelParams()
	if err != nil {
		common.ApiErrorMsg(c, "拉取 litellm 参数失败: "+err.Error())
		return
	}

	var models []model.Model
	if err := model.DB.Find(&models).Error; err != nil {
		common.ApiErrorMsg(c, "查询本地模型失败: "+err.Error())
		return
	}

	willUpdate := make([]modelParamsDiffItem, 0)
	skipped := make([]modelParamsDiffItem, 0)
	notFound := make([]modelParamsDiffItem, 0)

	for _, m := range models {
		item := modelParamsDiffItem{
			ModelId:      m.Id,
			ModelName:    m.ModelName,
			ParamsLocked: m.ParamsLocked,
			CurrentMaxIn: m.MaxInputTokens,
			CurrentMaxOut: m.MaxOutputTokens,
		}
		if m.ParamsLocked {
			skipped = append(skipped, item)
			continue
		}
		entry, ok := lookupLitellmEntry(litellmParams, m.ModelName)
		if !ok {
			item.WillUpdate = false
			notFound = append(notFound, item)
			continue
		}
		hasChange := false
		if v, ok := rawToInt(entry.MaxInputTokens); ok {
			item.NewMaxIn = v
			if v != m.MaxInputTokens {
				hasChange = true
			}
		} else {
			item.NewMaxIn = m.MaxInputTokens
		}
		if v, ok := rawToInt(entry.MaxOutputTokens); ok {
			item.NewMaxOut = v
		} else if v, ok2 := rawToInt(entry.MaxTokens); ok2 && len(entry.MaxOutputTokens) == 0 {
			item.NewMaxOut = v
		} else {
			item.NewMaxOut = m.MaxOutputTokens
		}
		if item.NewMaxOut != m.MaxOutputTokens {
			hasChange = true
		}
		if v, ok := rawToBool(entry.SupportsFunctionCalling); ok {
			item.NewTools = v
			if v != m.SupportsToolCall {
				hasChange = true
			}
		}
		if v, ok := rawToBool(entry.SupportsVision); ok {
			item.NewVision = v
			if v != m.SupportsImages {
				hasChange = true
			}
		}
		if v, ok := rawToBool(entry.SupportsReasoning); ok {
			item.NewReasoning = v
			if v != m.SupportsReasoning {
				hasChange = true
			}
		} else if v, ok2 := rawToBool(entry.SupportsResponseSchema); ok2 {
			item.NewReasoning = v
			if v != m.SupportsReasoning {
				hasChange = true
			}
		}
		item.WillUpdate = hasChange
		willUpdate = append(willUpdate, item)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"will_update":    willUpdate,
			"skipped_locked": skipped,
			"not_found":      notFound,
			"summary": gin.H{
				"will_update_count":    len(willUpdate),
				"skipped_locked_count": len(skipped),
				"not_found_count":      len(notFound),
				"total":                len(models),
			},
		},
	})
}
