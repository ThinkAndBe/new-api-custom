package middleware

import (
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func CORS() gin.HandlerFunc {
	config := cors.DefaultConfig()
	// 从环境变量读取允许的来源，逗号分隔；不设置则允许所有来源但不带凭证
	allowedOriginsStr := os.Getenv("CORS_ALLOWED_ORIGINS")
	if allowedOriginsStr != "" {
		config.AllowOrigins = strings.Split(allowedOriginsStr, ",")
		for i := range config.AllowOrigins {
			config.AllowOrigins[i] = strings.TrimSpace(config.AllowOrigins[i])
		}
		config.AllowCredentials = true
	} else {
		// 默认：允许所有来源但不带凭证（安全的默认行为）
		config.AllowAllOrigins = true
		config.AllowCredentials = false
	}
	config.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"*"}
	return cors.New(config)
}

func Version() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-New-Api-Version", common.Version)
		c.Next()
	}
}
