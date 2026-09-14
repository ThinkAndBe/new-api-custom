package controller

// provider_quota.go — 套餐额度监控管理端 API（仅超管）。

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// ListQuotaAccounts GET /api/quota/accounts
func ListQuotaAccounts(c *gin.Context) {
	accounts, err := model.GetProviderQuotaAccounts()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": accounts})
}

// CreateQuotaAccount POST /api/quota/accounts
func CreateQuotaAccount(c *gin.Context) {
	var req struct {
		Provider     string `json:"provider" binding:"required"`
		AccountName  string `json:"account_name" binding:"required"`
		SessionToken string `json:"session_token"`
		OrgId        string `json:"org_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	a := &model.ProviderQuotaAccount{
		Provider:     req.Provider,
		AccountName:  req.AccountName,
		SessionToken: req.SessionToken,
		OrgId:        req.OrgId,
		Status:       1,
	}
	if err := model.CreateProviderQuotaAccount(a); err != nil {
		common.ApiError(c, err)
		return
	}
	// 有 token 就立即抓一次
	if req.SessionToken != "" {
		go service.RefreshProviderQuota(a.Id)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": a})
}

// UpdateQuotaAccount PUT /api/quota/accounts
func UpdateQuotaAccount(c *gin.Context) {
	var req struct {
		Id           int    `json:"id" binding:"required"`
		AccountName  string `json:"account_name"`
		SessionToken string `json:"session_token"`
		OrgId        string `json:"org_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	a, err := model.GetProviderQuotaAccount(req.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if req.AccountName != "" {
		a.AccountName = req.AccountName
	}
	if req.SessionToken != "" {
		a.SessionToken = req.SessionToken
	}
	if req.OrgId != "" {
		a.OrgId = req.OrgId
	}
	if err := model.UpdateProviderQuotaAccount(a); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.SessionToken != "" {
		go service.RefreshProviderQuota(a.Id)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": a})
}

// DeleteQuotaAccount DELETE /api/quota/accounts?id=
func DeleteQuotaAccount(c *gin.Context) {
	id, _ := strconv.Atoi(c.Query("id"))
	if id <= 0 {
		common.ApiErrorMsg(c, "id required")
		return
	}
	if err := model.DeleteProviderQuotaAccount(id); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// RefreshQuota POST /api/quota/refresh?id=（空=全部）
func RefreshQuota(c *gin.Context) {
	idStr := c.Query("id")
	if idStr == "" {
		go service.RefreshAllProviderQuotas()
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "已触发全部刷新"})
		return
	}
	id, _ := strconv.Atoi(idStr)
	if err := service.RefreshProviderQuota(id); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "刷新完成"})
}
