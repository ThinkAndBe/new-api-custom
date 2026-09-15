package middleware

// atrust_auth.go — aTrust 零信任自动单点登录中间件（反向 OAuth2 方案）。
//
// 历史：最初按「来源 IP 反查 aTrust 在线用户」实现，但 aTrust 反代模式下
// 所有请求来源 IP 相同且不注入身份头，该方案不可行。现改为官方
// 「票据共享-反向oauth2」方案：
//
//	来源 IP 命中 aTrust 反代出口（ATrustProxyIPs）且未登录
//	→ 302 到 /api/oauth/atrust/start 发起 SSO → 回调换用户信息建会话。
//
// 防死循环：start 会写 atrust_sso_tried Cookie，存在时不再自动跳转，
// 用户落在正常登录页（可手动点零信任登录按钮重试）。

import (
	"strings"

	"github.com/QuantumNous/new-api/service"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// ATrustAutoAuth 来自零信任反代的未登录页面请求自动走 SSO
func ATrustAutoAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isATrustCandidate(c) || !service.ATrustSSOConfigured() {
			c.Next()
			return
		}

		// 已登录不动
		session := sessions.Default(c)
		if session.Get("username") != nil {
			c.Next()
			return
		}

		// 仅对来自 aTrust 反代出口的请求自动跳转
		if !service.IsATrustProxyIP(c.ClientIP()) {
			c.Next()
			return
		}

		// 防重定向死循环
		if _, err := c.Cookie("atrust_sso_tried"); err == nil {
			c.Next()
			return
		}

		c.Redirect(302, "/api/oauth/atrust/start")
		c.Abort()
	}
}

// isATrustCandidate 仅对前端页面路由做自动 SSO（排除 API/静态资源/回调页）
func isATrustCandidate(c *gin.Context) bool {
	path := c.Request.URL.Path
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/v1/") {
		return false
	}
	if strings.HasPrefix(path, "/static/") || strings.HasPrefix(path, "/logo") {
		return false
	}
	// OAuth 回调页（/oauth/*）不参与，避免与其他登录方式互踢
	if strings.HasPrefix(path, "/oauth/") {
		return false
	}
	// 登录/注册页保持手动（管理员密码登录的退路，不能被 SSO 抢跳）；
	// 正常导航（首页、/console/*）仍自动免登录
	if path == "/login" || path == "/register" {
		return false
	}
	return true
}
