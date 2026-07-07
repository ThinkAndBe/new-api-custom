package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// logTokenSummary 令牌汇总行的 JSON 结构。
type logTokenSummary struct {
	TokenName         string `json:"token_name"`
	Count             int64  `json:"count"`
	Quota             int64  `json:"quota"`
	PromptTokens      int64  `json:"prompt_tokens"`
	CompletionTokens  int64  `json:"completion_tokens"`
	TotalTokens       int64  `json:"total_tokens"`
}

// buildTokenSummary 复用 SumQuotaGroupByToken 构造汇总结果。
func buildTokenSummary(filter model.LogFilter) ([]logTokenSummary, int64, error) {
	rows, err := model.SumQuotaGroupByToken(&filter)
	if err != nil {
		return nil, 0, err
	}
	result := make([]logTokenSummary, 0, len(rows))
	var totalCount int64
	for _, r := range rows {
		key := r.KeyName
		if key == "" {
			key = "(空)"
		}
		result = append(result, logTokenSummary{
			TokenName:        key,
			Count:            r.Count,
			Quota:            r.Quota,
			PromptTokens:     r.PromptTokens,
			CompletionTokens: r.CompletionTokens,
			TotalTokens:      r.PromptTokens + r.CompletionTokens,
		})
		totalCount += r.Count
	}
	return result, totalCount, nil
}

// GetLogsTokenSummary 管理员获取令牌维度汇总（JSON，用于页面内展示）。
// 管理员视角下，使用日志和用户报表的令牌维度是重叠的；这里只统计超管（role>=10）用户下的令牌，
// 避免与用户报表重复，让管理员聚焦于"系统级/共享"令牌的用量。
func GetLogsTokenSummary(c *gin.Context) {
	filter, err := parseExportFilter(c, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	// 限定只统计管理员（role>=10）用户的令牌
	adminUserIds, err := model.GetAllAdminUserIds()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(adminUserIds) == 0 {
		// 没有管理员用户，直接返回空
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				"rows":  []logTokenSummary{},
				"total": 0,
				"count": "0",
			},
		})
		return
	}
	filter.AdminUserIds = adminUserIds
	rows, totalCount, err := buildTokenSummary(filter)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"rows":  rows,
			"total": len(rows),
			"count": strconv.FormatInt(totalCount, 10),
		},
	})
}

// GetLogsSelfTokenSummary 普通用户获取自己令牌维度汇总。
func GetLogsSelfTokenSummary(c *gin.Context) {
	filter, err := parseExportFilter(c, true)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	rows, totalCount, err := buildTokenSummary(filter)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"rows":       rows,
			"total":      len(rows),
			"count":      strconv.FormatInt(totalCount, 10),
		},
	})
}
