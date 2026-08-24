package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// usageGuideConfig 使用教程「复制命令」模式的数据下发。
// 前端生成 PowerShell 单行命令：irm {url} | iex，脚本从这里取配置并写入
// ~/.workbuddy/models.json 或 ~/.codebuddy/models.json。
type usageGuideConfig struct {
	Models []usageGuideModel `json:"models"`
}

type usageGuideModel struct {
	Id                string `json:"id"`
	Name              string `json:"name"`
	Provider          string `json:"provider"`
	URL               string `json:"url"`
	APIKey            string `json:"apiKey"`
	MaxInputTokens    int    `json:"maxInputTokens"`
	MaxOutputTokens   int    `json:"maxOutputTokens"`
	SupportsToolCall  bool   `json:"supportsToolCall"`
	SupportsImages    bool   `json:"supportsImages"`
	SupportsReasoning bool   `json:"supportsReasoning"`
}

// GetUsageGuideConfig 下发当前用户的 models.json 配置内容。
// 鉴权：Authorization Bearer <sk-token>（TokenAuthReadOnly，用户自己的令牌）。
// GET /api/usage/guide_config?token_id=
//
// 与前端逻辑对齐（web/classic/src/pages/UsageGuide/index.jsx）：
//   - 模型清单用完整清单（all=1，含临时禁用渠道），保证配置长期稳定
//   - 管理员模板不在此端点应用（模板影响展示与 bat 导出；命令模式始终用
//     实时清单生成，内容与自动生成路径一致）
//   - token_id 指定用哪个令牌的 key（默认该用户最新的启用令牌）
func GetUsageGuideConfig(c *gin.Context) {
	userId := c.GetInt("id")

	// 1. 令牌 key：优先 token_id 对应令牌（校验归属），否则取最新启用令牌
	tokenId, _ := strconv.Atoi(c.Query("token_id"))
	var key string
	if tokenId > 0 {
		tk, err := model.GetTokenByIds(tokenId, userId)
		if err != nil {
			common.ApiError(c, fmt.Errorf("token not found"))
			return
		}
		key = tk.GetFullKey()
	} else {
		// 默认取该用户最新的启用令牌
		var tokens []*model.Token
		if err := model.DB.Where("user_id = ? AND status = ?", userId, common.TokenStatusEnabled).
			Order("id desc").Limit(1).Find(&tokens).Error; err != nil || len(tokens) == 0 {
			common.ApiError(c, fmt.Errorf("no active token"))
			return
		}
		key = tokens[0].GetFullKey()
	}

	// 2. baseUrl：优先管理台配置的 server_address，否则按请求 Host 推导
	baseUrl := system_setting.ServerAddress
	if baseUrl == "" {
		host := c.Request.Host
		scheme := "https"
		if strings.HasPrefix(host, "localhost") || strings.HasPrefix(host, "127.0.0.1") {
			scheme = "http"
		}
		baseUrl = scheme + "://" + host
	}
	baseUrl = strings.TrimSuffix(baseUrl, "/")

	// 3. 模型清单 + 参数（与 GetUserModelsMeta 相同口径：all=1 + 白名单过滤）
	models, metas := collectUsageGuideModels(userId)

	cfg := buildUsageGuideConfig(models, metas, key, baseUrl)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    cfg,
	})
}

func collectUsageGuideModels(userId int) ([]string, map[string]model.Model) {
	user, err := model.GetUserCache(userId)
	if err != nil {
		return nil, nil
	}
	var groups map[string]string
	if model.IsAdmin(userId) {
		groups = service.GetUserUsableGroups(user.Group)
	} else {
		groups = service.GetUserOwnedGroups(user.Group)
	}
	modelSet := make(map[string]struct{})
	for group := range groups {
		for _, m := range model.GetGroupModels(group) {
			modelSet[m] = struct{}{}
		}
	}
	for name := range modelSet {
		if !model.IsModelRegistered(name) {
			delete(modelSet, name)
		}
	}
	names := make([]string, 0, len(modelSet))
	for n := range modelSet {
		names = append(names, n)
	}
	var dbModels []model.Model
	if len(names) > 0 {
		model.DB.Unscoped().
			Select("model_name", "max_input_tokens", "max_output_tokens", "supports_tool_call", "supports_images", "supports_reasoning").
			Where("model_name IN ?", names).Find(&dbModels)
	}
	metaMap := make(map[string]model.Model, len(dbModels))
	for _, m := range dbModels {
		metaMap[strings.ToLower(m.ModelName)] = m
	}
	return names, metaMap
}

func buildUsageGuideConfig(names []string, metas map[string]model.Model, key, baseUrl string) usageGuideConfig {
	models := make([]usageGuideModel, 0, len(names))
	for _, name := range names {
		m := metas[strings.ToLower(name)]
		models = append(models, usageGuideModel{
			Id:                name,
			Name:              "ERKE " + name,
			Provider:          "openai",
			URL:               baseUrl + "/v1",
			APIKey:            key,
			MaxInputTokens:    m.MaxInputTokens,
			MaxOutputTokens:   m.MaxOutputTokens,
			SupportsToolCall:  m.SupportsToolCall,
			SupportsImages:    m.SupportsImages,
			SupportsReasoning: m.SupportsReasoning,
		})
	}
	return usageGuideConfig{Models: models}
}
