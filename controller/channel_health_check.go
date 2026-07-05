package controller

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// CheckChannelHealth 手动触发单个渠道的健康检测（测活）。
// POST /api/channel/health/check/:id
// 复用 testChannel 做真实探活，返回检测结果。
func CheckChannelHealth(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的渠道 ID",
		})
		return
	}

	channel, err := model.GetChannelById(id, false)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "渠道不存在",
		})
		return
	}

	testUserID, err := resolveChannelTestUserID(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	result := testChannel(channel, testUserID, "", "", false)

	success := result.newAPIError == nil && result.localErr == nil
	msg := "测活成功"
	if !success {
		if result.newAPIError != nil {
			msg = result.newAPIError.Error()
		} else if result.localErr != nil {
			msg = result.localErr.Error()
		} else {
			msg = "测活失败"
		}
	}

	// 如果渠道当前是自动禁用状态且测活成功，则恢复
	if success && channel.Status == common.ChannelStatusAutoDisabled {
		// 使用 service.EnableChannel 但它在 service 包⋯
		// 直接调 model.UpdateChannelStatus
		model.UpdateChannelStatus(channel.Id, "", common.ChannelStatusEnabled, "手动测活成功，自动恢复")
		common.SysLog(fmt.Sprintf("[手动测活] 渠道「%s」(#%d) 测活成功，已自动恢复", channel.Name, channel.Id))
	}

	c.JSON(http.StatusOK, gin.H{
		"success": success,
		"message": msg,
		"data": gin.H{
			"channel_id":   channel.Id,
			"channel_name": channel.Name,
		},
	})
}