package service

// zhipu_quota.go — 智谱 coding plan 额度抓取。
//
// 智谱 bigmodel.cn 的 coding plan 页面是前端渲染的 SPA，登录后有 session
// token，所有 API 请求带 Authorization header。我们用管理员导入的 token
// 定期调用以下端点：
//   GET https://bigmodel.cn/api/coding-plan/v1/team/my-plan
//   GET https://bigmodel.cn/api/coding-plan/v1/team/usage
//
// Token 获取方式：浏览器 F12 → Network → 选 coding-plan 页面的任一请求
// → 复制 Request Headers 里的 Authorization 值（Bearer xxx 或 JWT）。

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// ZhipuQuotaData 智谱 coding plan 额度结构
type ZhipuQuotaData struct {
	PlanName    string  `json:"plan_name"`    // 套餐名
	UsedTokens  int64   `json:"used_tokens"`  // 已用 tokens
	TotalTokens int64   `json:"total_tokens"` // 总额度 tokens
	UsedPercent float64 `json:"used_percent"` // 使用百分比
	ExpireAt    string  `json:"expire_at"`    // 过期时间
	Raw         string  `json:"raw"`          // 原始 JSON
}

// FetchZhipuQuota 用 session token 抓智谱 coding plan 额度
func FetchZhipuQuota(sessionToken, orgId string) (*ZhipuQuotaData, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	// 智谱 coding plan 的 API 端点（前端 SPA 调用的内部接口）
	urls := []string{
		"https://bigmodel.cn/api/coding-plan/v1/team/my-plan",
		"https://bigmodel.cn/api/coding-plan/v1/team/usage",
		"https://bigmodel.cn/api/coding-plan/team/my-plan",
	}

	var body []byte
	var statusCode int
	for _, u := range urls {
		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", sessionToken)
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Referer", "https://bigmodel.cn/coding-plan/team/my-plan")
		if orgId != "" {
			req.Header.Set("X-Org-Id", orgId)
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		statusCode = resp.StatusCode
		if statusCode == 200 && strings.Contains(string(body), "code\":200") {
			break // 找到可用端点
		}
	}

	if len(body) == 0 {
		return nil, fmt.Errorf("所有端点均不可达")
	}
	bodyStr := string(body)

	// 401/403 = token 过期
	if statusCode == 401 || statusCode == 403 || strings.Contains(bodyStr, "令牌已过期") || strings.Contains(bodyStr, "401") {
		return nil, fmt.Errorf("TOKEN_EXPIRED")
	}
	if statusCode != 200 {
		return nil, fmt.Errorf("status %d: %s", statusCode, truncate(bodyStr, 100))
	}

	// 解析（结构可能是 {code:200, data:{...}} 或直接 {...}）
	data := &ZhipuQuotaData{Raw: truncate(bodyStr, 2000)}
	var m map[string]interface{}
	if err := common.Unmarshal(body, &m); err != nil {
		return data, nil // 返回原始数据，前端展示 raw
	}
	// 递归找关键字段
	extractZhipuFields(m, data)
	return data, nil
}

// extractZhipuFields 递归提取套餐字段（智谱 API 层级不固定）
func extractZhipuFields(m map[string]interface{}, d *ZhipuQuotaData) {
	var walk func(obj interface{}, depth int)
	walk = func(obj interface{}, depth int) {
		if depth > 5 {
			return
		}
		switch v := obj.(type) {
		case map[string]interface{}:
			for key, val := range v {
				lk := strings.ToLower(key)
				switch {
				case strings.Contains(lk, "plan") && strings.Contains(lk, "name"):
					if s, ok := val.(string); ok {
						d.PlanName = s
					}
				case strings.Contains(lk, "used") && (strings.Contains(lk, "token") || strings.Contains(lk, "quota")):
					if f, ok := val.(float64); ok {
						d.UsedTokens = int64(f)
					}
				case strings.Contains(lk, "total") && (strings.Contains(lk, "token") || strings.Contains(lk, "quota")):
					if f, ok := val.(float64); ok {
						d.TotalTokens = int64(f)
					}
				case strings.Contains(lk, "expire"):
					if s, ok := val.(string); ok {
						d.ExpireAt = s
					} else if f, ok := val.(float64); ok {
						d.ExpireAt = fmt.Sprintf("%.0f", f)
					}
				}
				walk(val, depth+1)
			}
		case []interface{}:
			for _, item := range v {
				walk(item, depth+1)
			}
		}
	}
	walk(m, 0)
	if d.TotalTokens > 0 {
		d.UsedPercent = float64(d.UsedTokens) / float64(d.TotalTokens) * 100
	}
}

// RefreshProviderQuota 刷新单个账号的额度
func RefreshProviderQuota(accountId int) error {
	a, err := model.GetProviderQuotaAccount(accountId)
	if err != nil {
		return err
	}
	if a.SessionToken == "" {
		model.SaveQuotaResult(accountId, "", "error", "未设置 Session Token")
		return fmt.Errorf("未设置 Session Token")
	}

	switch a.Provider {
	case "zhipu":
		data, err := FetchZhipuQuota(a.SessionToken, a.OrgId)
		if err != nil {
			if err.Error() == "TOKEN_EXPIRED" {
				model.SaveQuotaResult(accountId, "", "expired", "Token 已过期，请重新导入")
				return fmt.Errorf("Token 已过期")
			}
			model.SaveQuotaResult(accountId, "", "error", err.Error())
			return err
		}
		jsonStr, _ := common.Marshal(data)
		model.SaveQuotaResult(accountId, string(jsonStr), "ok", "")
		return nil
	default:
		return fmt.Errorf("不支持的服务商: %s", a.Provider)
	}
}

// RefreshAllProviderQuotas 刷新全部账号
func RefreshAllProviderQuotas() {
	accounts, err := model.GetProviderQuotaAccounts()
	if err != nil {
		return
	}
	for _, a := range accounts {
		if a.SessionToken == "" {
			continue
		}
		if err := RefreshProviderQuota(a.Id); err != nil {
			common.SysLog(fmt.Sprintf("quota refresh %s/%s failed: %s", a.Provider, a.AccountName, err.Error()))
		}
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
