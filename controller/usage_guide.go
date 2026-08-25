package controller

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// ---- 配置短码：使用教程「一键配置」的最简交互 ----
// 教程页点按钮生成 6 位短码（5 分钟有效，内存存储），
// 用户在 erke-config-tool.exe 里只输短码 + 点「一键配置」即完成。
// 兑换后立即失效（一次性），码内不落地任何明文密钥。

type guideShortCode struct {
	userId    int
	tokenId   int
	product   string
	expiresAt time.Time
	used      bool
}

const guideShortCodeTTL = 5 * time.Minute

var (
	guideShortCodes   sync.Map // code -> *guideShortCode
	guideShortCodeMu  sync.Mutex
)

func genShortCode() (string, error) {
	const digits = "0123456789"
	const letters = "ABCDEFGHJKMNPQRSTUVWXYZ" // 去掉易混淆的 I/L/O
	out := make([]byte, 6)
	for i := range out {
		pool := digits + letters
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(pool))))
		if err != nil {
			return "", err
		}
		out[i] = pool[n.Int64()]
	}
	return string(out), nil
}

// CreateGuideShortCode POST /api/usage/guide_code （UserAuth）
// 为当前用户选定的令牌生成一次性短码，5 分钟有效。
// token_id 兼容字符串（前端 Select 的 value 是 String(tk.id)）。
func CreateGuideShortCode(c *gin.Context) {
	userId := c.GetInt("id")
	var req struct {
		TokenId json.Number `json:"token_id"`
		Product string      `json:"product"`
	}
	_ = c.ShouldBindJSON(&req)
	tokenId, err := strconv.Atoi(req.TokenId.String())
	if err != nil || tokenId <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "token_id required"})
		return
	}
	if _, err := model.GetTokenByIds(tokenId, userId); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "token not found"})
		return
	}
	product := req.Product
	if product != "workbuddy" && product != "codebuddy" {
		product = "workbuddy"
	}

	// 清理过期码 + 限制单用户未用码数量（防刷）
	now := time.Now()
	count := 0
	guideShortCodes.Range(func(k, v any) bool {
		if sc, ok := v.(*guideShortCode); ok {
			if sc.expiresAt.Before(now) || sc.used {
				guideShortCodes.Delete(k)
			} else if sc.userId == userId {
				count++
			}
		}
		return true
	})
	if count >= 5 {
		c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "message": "too many active codes, wait or use existing one"})
		return
	}

	code, err := genShortCode()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	guideShortCodes.Store(code, &guideShortCode{
		userId:    userId,
		tokenId:   tokenId,
		product:   product,
		expiresAt: now.Add(guideShortCodeTTL),
	})

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"code":       code,
		"expires_in": int(guideShortCodeTTL.Seconds()),
	})
}

// RedeemGuideShortCode GET /api/usage/guide_redeem?code=XXXXXX （公开 + 限流）
// exe 用短码换取完整 models.json 配置；短码一次性，兑换即失效。
func RedeemGuideShortCode(c *gin.Context) {
	code := strings.ToUpper(strings.TrimSpace(c.Query("code")))
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "code required"})
		return
	}
	v, ok := guideShortCodes.Load(code)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "配置码无效或已过期，请回到使用教程页重新生成"})
		return
	}
	sc := v.(*guideShortCode)
	guideShortCodeMu.Lock()
	if sc.used || sc.expiresAt.Before(time.Now()) {
		guideShortCodes.Delete(code)
		guideShortCodeMu.Unlock()
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "配置码无效或已过期，请回到使用教程页重新生成"})
		return
	}
	sc.used = true
	guideShortCodes.Delete(code) // 一次性
	guideShortCodeMu.Unlock()

	// 复用 GetUsageGuideConfig 的生成逻辑（以短码持有者身份）
	c.Set("id", sc.userId)
	q := c.Request.URL.Query()
	q.Set("token_id", strconv.Itoa(sc.tokenId))
	c.Request.URL.RawQuery = q.Encode()
	GetUsageGuideConfig(c)
}

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

// DownloadUsageGuideConfigTool 下发 erke-config-tool.exe 配置工具。
// exe 已随仓库提交在 config-tool/erke-config-tool.exe（git add -f 例外于
// .gitignore 的 *.exe），Docker 构建 COPY . . 自动带入镜像；也可手动放到
// /data/config-tool/ 覆盖。源码见 tools/erke-config-tool。
// GET /api/usage/config_tool （无需登录：工具本身不含任何密钥）
func DownloadUsageGuideConfigTool(c *gin.Context) {
	candidates := []string{
		filepath.Join("config-tool", "erke-config-tool.exe"),
		filepath.Join("/data", "config-tool", "erke-config-tool.exe"),
	}
	exePath := ""
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			exePath = p
			break
		}
	}
	if exePath == "" {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "config tool not deployed",
		})
		return
	}
	c.Header("Content-Disposition", `attachment; filename="erke-config-tool.exe"`)
	// 工具更新较频繁，禁止浏览器/代理缓存旧版本
	c.Header("Cache-Control", "no-store")
	c.File(exePath)
}
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
