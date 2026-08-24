package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// TokenAuthAllowQueryKey 允许通过 ?key=sk-xxx 查询参数携带令牌。
// 仅用于用户主动复制/传递配置链接的场景（如使用教程的配置链接给
// erke-config-tool.exe 拉取）：exe 无法方便地设置请求头。
// 挂载顺序：必须在 TokenAuthReadOnly **之前**——把 key 写入请求头后再
// 让后续的 TokenAuthReadOnly 统一校验。优先级：Authorization 头 > ?key=。
func TokenAuthAllowQueryKey() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("Authorization") != "" {
			c.Next()
			return
		}
		k := strings.TrimSpace(c.Query("key"))
		if k == "" {
			c.Next()
			return
		}
		if !strings.HasPrefix(k, "Bearer ") {
			k = "Bearer " + k
		}
		c.Request.Header.Set("Authorization", k)
		c.Next()
	}
}
