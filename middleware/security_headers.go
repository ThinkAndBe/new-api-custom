package middleware

import (
	"strings"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
)

// SecurityHeaders 为所有响应添加基础安全响应头。
//
//   - X-Content-Type-Options: nosniff  阻止 MIME 嗅探
//   - X-Frame-Options: DENY            防点击劫持（管理后台不可被 iframe 嵌套）
//   - Referrer-Policy: strict-origin-when-cross-origin  不向外站泄漏完整 URL
//   - Strict-Transport-Security        强制 HTTPS（仅在确认走 HTTPS 入口时下发）
//
// HSTS 仅在请求已是 HTTPS（TLS 终止在可信代理，X-Forwarded-Proto=https）
// 或本机测试显式设置 SECURITY_HEADERS_FORCE_HSTS=true 时下发，
// 避免纯 HTTP 内网部署被浏览器强制升级导致无法访问。
func SecurityHeaders() gin.HandlerFunc {
	forceHSTS := common.GetEnvOrDefaultBool("SECURITY_HEADERS_FORCE_HSTS", false)

	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")

		if forceHSTS || c.Request.TLS != nil ||
			strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
			c.Header("Strict-Transport-Security", "max-age=31536000")
		}

		c.Next()
	}
}
