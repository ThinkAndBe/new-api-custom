package controller

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// model_params_sync.go — 模型参数/价格同步，唯一数据源：七牛模型目录
// https://www.qiniu.com/ai/models（参数 + 价格，见 qiniu_models.go）。
// 不再使用 litellm 等其他来源。目录里查不到的模型按默认值处理：
// 输入上限 100 万 / 输出上限 12.8 万。

// applyQiniuInfo 计算某个模型应更新的字段。
// found=false 表示目录里没有该模型 → 参数取默认值（价格不动）。
func applyQiniuInfo(m *model.Model, info QiniuModelInfo, found bool) (paramUpdates, priceUpdates map[string]interface{}) {
	paramUpdates = map[string]interface{}{}
	priceUpdates = map[string]interface{}{}
	if found {
		if info.ContextLen > 0 {
			paramUpdates["max_input_tokens"] = info.ContextLen
		}
		if info.MaxOut > 0 {
			paramUpdates["max_output_tokens"] = info.MaxOut
		}
		paramUpdates["supports_tool_call"] = info.Tool
		paramUpdates["supports_images"] = info.Vision
		paramUpdates["supports_reasoning"] = info.Reasoning
		if info.InPrice > 0 {
			priceUpdates["input_price"] = info.InPrice
		}
		if info.OutPrice > 0 {
			priceUpdates["output_price"] = info.OutPrice
		}
		if info.CachePrice > 0 {
			priceUpdates["cache_hit_price"] = info.CachePrice
		}
	} else {
		paramUpdates["max_input_tokens"] = model.DefaultMaxInputTokens
		paramUpdates["max_output_tokens"] = model.DefaultMaxOutputTokens
	}
	return paramUpdates, priceUpdates
}

// SyncModelParamsFromQiniu 管理员接口：从七牛模型目录拉取参数与价格，更新 models 表。
// - 参数：仅更新 ParamsLocked=false 的行（人工编辑过的不覆盖）；目录没有的给默认值。
// - 价格：仅更新 PricingLocked=false 的行；目录没有价格的不动。
// POST /api/models/sync_params
func SyncModelParamsFromQiniu(c *gin.Context) {
	catalog, err := FetchQiniuModels()
	if err != nil {
		common.ApiErrorMsg(c, "拉取七牛模型目录失败: "+err.Error())
		return
	}

	var models []model.Model
	if err := model.DB.Find(&models).Error; err != nil {
		common.ApiErrorMsg(c, "查询本地模型失败: "+err.Error())
		return
	}

	updated := 0
	priceUpdated := 0
	skippedLocked := 0
	notFoundInQiniu := 0

	for _, m := range models {
		info, found := LookupQiniuModel(catalog, m.ModelName)
		if !found {
			notFoundInQiniu++
		}
		paramUpdates, priceUpdates := applyQiniuInfo(&m, info, found)

		if !m.ParamsLocked && len(paramUpdates) > 0 {
			if err := model.DB.Model(&model.Model{}).Where("id = ?", m.Id).Updates(paramUpdates).Error; err != nil {
				common.SysError(fmt.Sprintf("七牛参数同步更新模型 %s (id=%d) 失败: %s", m.ModelName, m.Id, err.Error()))
			} else {
				updated++
			}
		} else if m.ParamsLocked {
			skippedLocked++
		}

		if !m.PricingLocked && len(priceUpdates) > 0 {
			if err := model.DB.Model(&model.Model{}).Where("id = ?", m.Id).Updates(priceUpdates).Error; err != nil {
				common.SysError(fmt.Sprintf("七牛价格同步更新模型 %s (id=%d) 失败: %s", m.ModelName, m.Id, err.Error()))
			} else {
				priceUpdated++
			}
		}
	}

	common.SysLog(fmt.Sprintf("七牛模型同步完成: 参数更新 %d, 价格更新 %d, 跳过锁定 %d, 目录未覆盖 %d(默认值), 总计 %d, 目录模型数 %d",
		updated, priceUpdated, skippedLocked, notFoundInQiniu, len(models), len(catalog)))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("已更新 %d 个模型参数、%d 个模型价格", updated, priceUpdated),
		"data": gin.H{
			"updated":           updated,
			"price_updated":     priceUpdated,
			"skipped_locked":    skippedLocked,
			"not_found":         notFoundInQiniu,
			"total":             len(models),
			"qiniu_model_count": len(catalog),
		},
	})
}

// PreviewModelParamsDiff 管理员接口：预览七牛同步将带来的变更（不写库）。
// GET /api/models/sync_params/preview
func PreviewModelParamsDiff(c *gin.Context) {
	catalog, err := FetchQiniuModels()
	if err != nil {
		common.ApiErrorMsg(c, "拉取七牛模型目录失败: "+err.Error())
		return
	}

	var models []model.Model
	if err := model.DB.Find(&models).Error; err != nil {
		common.ApiErrorMsg(c, "查询本地模型失败: "+err.Error())
		return
	}

	type diffItem struct {
		ModelId          int     `json:"model_id"`
		ModelName        string  `json:"model_name"`
		ParamsLocked     bool    `json:"params_locked"`
		FoundInQiniu     bool    `json:"found_in_qiniu"`
		WillUpdate       bool    `json:"will_update"`
		CurrentMaxIn     int     `json:"current_max_input_tokens"`
		CurrentMaxOut    int     `json:"current_max_output_tokens"`
		NewMaxIn         int     `json:"new_max_input_tokens"`
		NewMaxOut        int     `json:"new_max_output_tokens"`
		NewTools         bool    `json:"new_supports_tool_call"`
		NewVision        bool    `json:"new_supports_images"`
		NewReasoning     bool    `json:"new_supports_reasoning"`
		NewInputPrice    float64 `json:"new_input_price"`
		NewOutputPrice   float64 `json:"new_output_price"`
		NewCacheHitPrice float64 `json:"new_cache_hit_price"`
	}
	items := make([]diffItem, 0, len(models))
	for _, m := range models {
		info, found := LookupQiniuModel(catalog, m.ModelName)
		paramUpdates, priceUpdates := applyQiniuInfo(&m, info, found)
		item := diffItem{
			ModelId:       m.Id,
			ModelName:     m.ModelName,
			ParamsLocked:  m.ParamsLocked,
			FoundInQiniu:  found,
			WillUpdate:    (!m.ParamsLocked && len(paramUpdates) > 0) || (!m.PricingLocked && len(priceUpdates) > 0),
			CurrentMaxIn:  m.MaxInputTokens,
			CurrentMaxOut: m.MaxOutputTokens,
		}
		if v, ok := paramUpdates["max_input_tokens"].(int); ok {
			item.NewMaxIn = v
		}
		if v, ok := paramUpdates["max_output_tokens"].(int); ok {
			item.NewMaxOut = v
		}
		item.NewTools, _ = paramUpdates["supports_tool_call"].(bool)
		item.NewVision, _ = paramUpdates["supports_images"].(bool)
		item.NewReasoning, _ = paramUpdates["supports_reasoning"].(bool)
		item.NewInputPrice, _ = priceUpdates["input_price"].(float64)
		item.NewOutputPrice, _ = priceUpdates["output_price"].(float64)
		item.NewCacheHitPrice, _ = priceUpdates["cache_hit_price"].(float64)
		items = append(items, item)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    items,
	})
}
