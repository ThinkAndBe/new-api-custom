package controller

// zhipu_browser_controller.go — 智谱无头浏览器登录 API（仅超管）。
//
// POST /api/quota/browser/start       → 启动浏览器会话
// GET  /api/quota/browser/screenshot  → 截图（PNG）
// POST /api/quota/browser/fill        → 填入手机号/密码
// POST /api/quota/browser/submit      → 点击登录
// POST /api/quota/browser/capture     → 等待捕获 token（长轮询）
// POST /api/quota/browser/close       → 关闭会话

import (
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// StartBrowserLogin POST /api/quota/browser/start
func StartBrowserLogin(c *gin.Context) {
	session, err := service.StartZhipuLogin()
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"session_id": session.ID,
			"status":     session.Status,
			"expires_at": session.CreatedAt.Add(5 * time.Minute).Unix(),
		},
	})
}

// BrowserScreenshot GET /api/quota/browser/screenshot?session=
func BrowserScreenshot(c *gin.Context) {
	sessionID := c.Query("session")
	if sessionID == "" {
		common.ApiErrorMsg(c, "session required")
		return
	}
	png, err := service.GetZhipuScreenshot(sessionID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	contentType := "image/png"
	if len(png) > 2 && png[0] == 0xFF && png[1] == 0xD8 {
		contentType = "image/jpeg"
	}
	c.Data(http.StatusOK, contentType, png)
}

// BrowserFillCredentials POST /api/quota/browser/fill
func BrowserFillCredentials(c *gin.Context) {
	var req struct {
		Session  string `json:"session" binding:"required"`
		Phone    string `json:"phone" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := service.ZhipuFillCredentials(req.Session, req.Phone, req.Password); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已填入"})
}

// BrowserSubmitLogin POST /api/quota/browser/submit
func BrowserSubmitLogin(c *gin.Context) {
	var req struct {
		Session string `json:"session" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := service.ZhipuSubmitLogin(req.Session); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已点击登录"})
}

// BrowserCaptureToken POST /api/quota/browser/capture
// 长轮询：等待最多 30 秒捕获 token；成功后可指定 account_id 保存
func BrowserCaptureToken(c *gin.Context) {
	var req struct {
		Session   string `json:"session" binding:"required"`
		AccountID int    `json:"account_id"`
		Timeout   int    `json:"timeout"` // 秒，默认 20
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	timeout := req.Timeout
	if timeout <= 0 || timeout > 60 {
		timeout = 20
	}

	token, url, err := service.ZhipuCaptureToken(req.Session, timeout)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	// 如果指定了账号，直接保存
	if req.AccountID > 0 {
		if saveErr := service.SaveZhipuTokenToAccount(req.AccountID, token); saveErr != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"message": "Token 已捕获但保存失败: " + saveErr.Error(),
				"token":   token[:20] + "...",
			})
			return
		}
	}

	service.CloseZhipuSession(req.Session)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "登录成功，Token 已捕获",
		"data": gin.H{
			"token":       token,
			"capture_url": url,
		},
	})
}

// BrowserClose POST /api/quota/browser/close
func BrowserClose(c *gin.Context) {
	var req struct {
		Session string `json:"session" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	service.CloseZhipuSession(req.Session)
	c.JSON(http.StatusOK, gin.H{"success": true})
}
