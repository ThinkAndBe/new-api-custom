package router

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

// SetApiOnlyRouter 只挂载对外 API relay 路由，不挂载 Web 界面和管理接口。
// 用于 API 端口与 Web 端口分离场景（WEB_PORT 环境变量设置时）。
func SetApiOnlyRouter(router *gin.Engine) {
	// 全局限流，防止未认证请求洪泛
	router.Use(middleware.GlobalAPIRateLimit())
	SetRelayRouter(router)
	SetVideoRouter(router)
	SetDashboardRouter(router)

	// 对未知路由返回 JSON 404，不返回 Web 页面
	router.NoRoute(func(c *gin.Context) {
		controller.RelayNotFound(c)
	})
}

func SetRouter(router *gin.Engine, assets ThemeAssets) {
	SetApiRouter(router)
	SetDashboardRouter(router)
	SetRelayRouter(router)
	SetVideoRouter(router)
	frontendBaseUrl := os.Getenv("FRONTEND_BASE_URL")
	if common.IsMasterNode && frontendBaseUrl != "" {
		frontendBaseUrl = ""
		common.SysLog("FRONTEND_BASE_URL is ignored on master node")
	}
	if frontendBaseUrl == "" {
		SetWebRouter(router, assets)
	} else {
		frontendBaseUrl = strings.TrimSuffix(frontendBaseUrl, "/")
		router.NoRoute(func(c *gin.Context) {
			c.Set(middleware.RouteTagKey, "web")
			c.Redirect(http.StatusMovedPermanently, fmt.Sprintf("%s%s", frontendBaseUrl, c.Request.RequestURI))
		})
	}
}
