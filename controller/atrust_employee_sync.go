package controller

// atrust_employee_sync.go — 管理端：从零信任批量同步用户工号。

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// SyncEmployeeIds 拉取零信任在线用户，按姓名匹配本地账号回填工号
func SyncEmployeeIds(c *gin.Context) {
	report, err := service.SyncEmployeeIdsFromATrust()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeSystem,
		"零信任工号同步：在线 "+itoa(report.OnlineTotal)+"，新绑定 "+itoa(report.Synced)+
			"，更新 "+itoa(report.Overwritten)+"，歧义 "+itoa(len(report.Ambiguous)))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    report,
	})
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
