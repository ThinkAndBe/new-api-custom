package middleware

// atrust_auth.go — aTrust 零信任免认证中间件。
//
// 挂在公开路由之前：如果请求来源 IP 在 aTrust 在线用户列表中，
// 自动创建/查找用户并注入 session（等于已登录）。未匹配则正常放行，
// 走后续的登录/token 认证。

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// ATrustAutoAuth 尝试通过 aTrust 在线用户自动登录
func ATrustAutoAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isATrustCandidate(c) {
			c.Next()
			return
		}

		clientIP := c.ClientIP()
		user, err := service.ATrustLookupByIP(clientIP)
		if err != nil {
			c.Next() // 不在 aTrust 在线列表，走正常认证
			return
		}

		// 找到 aTrust 用户，自动登录
		username := normalizeATrustUsername(user.Name, user.DisplayName)
		if username == "" {
			c.Next()
			return
		}

		u, err := findOrCreateATrustUser(username, user.DisplayName)
		if err != nil {
			common.SysLog("atrust: auto login failed for " + username + ": " + err.Error())
			c.Next()
			return
		}

		// 注入登录态
		c.Set("id", u.Id)
		c.Set("username", u.Username)
		c.Set("role", u.Role)
		c.Set("group", u.Group)
		c.Set(" atoftrust", true) // 标记来源（供日志区分）
		common.SysLog("atrust: auto login " + username + " from IP " + clientIP)
		c.Next()
	}
}

// isATrustCandidate 判断是否尝试 aTrust 免认证（仅页面路径，排除 /api 和 /v1）
func isATrustCandidate(c *gin.Context) bool {
	path := c.Request.URL.Path
	// 只对前端页面路由做免认证（控制台、登录页等）
	// API 调用（/v1/*）仍走 token 认证，不受影响
	if strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/api/") {
		return false
	}
	// 静态资源不需要
	if strings.HasPrefix(path, "/static/") || strings.HasPrefix(path, "/logo") {
		return false
	}
	return true
}

// normalizeATrustUsername 从 aTrust 用户信息提取用户名
func normalizeATrustUsername(name, displayName string) string {
	if name != "" {
		return strings.TrimSpace(name)
	}
	if displayName != "" {
		return strings.TrimSpace(displayName)
	}
	return ""
}

// findOrCreateATrustUser 查找或自动创建用户
func findOrCreateATrustUser(username, displayName string) (*model.User, error) {
	// 先按用户名查
	var existing model.User
	if err := model.DB.Where("username = ?", username).First(&existing).Error; err == nil {
		return &existing, nil
	}

	// 不存在 → 自动创建
	newUser := &model.User{
		Username:    username,
		DisplayName: displayName,
		Password:    common.GetUUID(), // 随机密码，用户永远不需要知道
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	if err := newUser.Insert(0); err != nil {
		return nil, err
	}
	common.SysLog("atrust: auto created user " + username)
	return newUser, nil
}
