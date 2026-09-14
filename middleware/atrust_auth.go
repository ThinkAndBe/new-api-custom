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
		atrustUser, err := service.ATrustLookupByIP(clientIP)
		if err != nil {
			c.Next() // 不在 aTrust 在线列表，走正常认证
			return
		}

		// 匹配优先级：中文姓名（与 CSV 导入一致）> 工号 > displayName
		u := matchATrustUser(atrustUser)
		if u == nil {
			c.Next()
			return
		}

		// 注入登录态
		c.Set("id", u.Id)
		c.Set("username", u.Username)
		c.Set("role", u.Role)
		c.Set("group", u.Group)
		common.SysLog("atrust: auto login " + u.Username + " from IP " + clientIP)
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

// matchATrustUser 按 aTrust 身份匹配/创建用户。
// 优先级：中文姓名（displayName，与 CSV 导入一致）→ 工号（name）→ 自动创建。
// 这样已有的 119 个用户（苏长辉 等）能直接匹配，不会产生重复账号。
func matchATrustUser(atrustUser *service.ATrustOnlineUser) *model.User {
	displayName := strings.TrimSpace(atrustUser.DisplayName)
	employeeId := strings.TrimSpace(atrustUser.Name)

	// 1. 按中文姓名匹配（大多数已有用户的 username 就是中文名）
	if displayName != "" {
		var u model.User
		if err := model.DB.Where("username = ? OR display_name = ?", displayName, displayName).First(&u).Error; err == nil {
			return &u
		}
	}

	// 2. 按工号匹配（以防有人用工号注册过）
	if employeeId != "" {
		var u model.User
		if err := model.DB.Where("username = ?", employeeId).First(&u).Error; err == nil {
			return &u
		}
	}

	// 3. 都没匹配到 → 自动创建（用中文姓名做 username，保持一致性）
	username := displayName
	if username == "" {
		username = employeeId
	}
	if username == "" {
		return nil
	}

	newUser := &model.User{
		Username:    username,
		DisplayName: displayName,
		Password:    common.GetUUID(), // 随机密码，用户永远不需要
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	if err := newUser.Insert(0); err != nil {
		common.SysLog("atrust: auto create user " + username + " failed: " + err.Error())
		return nil
	}
	common.SysLog("atrust: auto created user " + username)
	return newUser
}
