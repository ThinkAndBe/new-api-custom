package controller

import (
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// AutoSyncOfficialRatiosNow 手动触发一次官方价格同步（Root 权限）。
// 复用 service.SyncOfficialRatios，与定时任务相同的逻辑。
func AutoSyncOfficialRatiosNow(c *gin.Context) {
	count, err := service.SyncOfficialRatios()
	if err != nil {
		common.SysError("manual official ratio sync failed: " + err.Error())
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "同步失败: " + err.Error(),
		})
		return
	}
	lastTime, _ := service.GetLastAutoSyncInfo()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "同步成功",
		"data": gin.H{
			"count":          count,
			"last_sync_time": lastTime.Unix(),
		},
	})
}

// GetOfficialRatioSyncStatus 返回自动同步的当前状态（管理员可见）。
func GetOfficialRatioSyncStatus(c *gin.Context) {
	lastTime, lastCount := service.GetLastAutoSyncInfo()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"enabled":          operation_setting.AutoSyncOfficialRatioEnabled,
			"interval_hours":   operation_setting.OfficialRatioSyncIntervalHours,
			"sources":          operation_setting.OfficialRatioSyncSources,
			"last_sync_time":   lastTime.Unix(),
			"last_sync_count":  lastCount,
			"self_use_mode":    operation_setting.SelfUseModeEnabled,
			"next_sync_time":   nextSyncTime(lastTime, operation_setting.OfficialRatioSyncIntervalHours).Unix(),
		},
	})
}

func nextSyncTime(last time.Time, intervalHours int) time.Time {
	if intervalHours <= 0 {
		intervalHours = 24
	}
	if last.IsZero() {
		return time.Now()
	}
	return last.Add(time.Duration(intervalHours) * time.Hour)
}
