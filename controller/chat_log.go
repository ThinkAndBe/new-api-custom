package controller

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetChatLogs 分页查询对话日志（管理员）
func GetChatLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("p"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	userId, _ := strconv.Atoi(c.Query("user_id"))
	username := c.Query("username")
	modelName := c.Query("model_name")
	tokenName := c.Query("token_name")
	startId, _ := strconv.Atoi(c.Query("start_id"))
	endId, _ := strconv.Atoi(c.Query("end_id"))
	startTime, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTime, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	filter := model.ChatLogFilter{
		UserId:    userId,
		Username:  username,
		ModelName: modelName,
		TokenName: tokenName,
		StartId:   startId,
		EndId:     endId,
		StartTime: startTime,
		EndTime:   endTime,
	}

	logs, total, err := model.GetChatLogs(filter, page, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"items":     logs,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// ExportChatLogs 导出对话日志为 CSV（管理员）
func ExportChatLogs(c *gin.Context) {
	userId, _ := strconv.Atoi(c.Query("user_id"))
	username := c.Query("username")
	modelName := c.Query("model_name")
	tokenName := c.Query("token_name")
	startId, _ := strconv.Atoi(c.Query("start_id"))
	endId, _ := strconv.Atoi(c.Query("end_id"))
	startTime, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTime, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	filter := model.ChatLogFilter{
		UserId:    userId,
		Username:  username,
		ModelName: modelName,
		TokenName: tokenName,
		StartId:   startId,
		EndId:     endId,
		StartTime: startTime,
		EndTime:   endTime,
	}

	// 设置 CSV 响应头
	c.Writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
	c.Writer.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=chat_logs_%s.csv", time.Now().Format("20060102_150405")))

	// UTF-8 BOM
	c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})

	writer := csv.NewWriter(c.Writer)
	if err := writer.Write([]string{"日志ID", "时间", "用户ID", "用户名", "令牌", "渠道ID", "模型", "分组", "请求ID", "流式", "请求内容"}); err != nil {
		common.SysError("write csv header failed: " + err.Error())
		return
	}

	err := model.StreamAllChatLogs(filter, func(log *model.ChatLog) error {
		createdAt := time.Unix(log.CreatedAt, 0).Format("2006-01-02 15:04:05")
		return writer.Write([]string{
			strconv.Itoa(log.Id),
			createdAt,
			strconv.Itoa(log.UserId),
			log.Username,
			log.TokenName,
			strconv.Itoa(log.ChannelId),
			log.ModelName,
			log.Group,
			log.RequestId,
			strconv.FormatBool(log.IsStream),
			log.RequestContent,
		})
	})
	writer.Flush()
	if err != nil {
		common.SysError("export chat logs failed: " + err.Error())
	}
}

// DeleteAllChatLogs 清空所有对话日志（管理员）
func DeleteAllChatLogs(c *gin.Context) {
	err := model.DeleteAllChatLogs()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "已清空所有对话日志",
	})
}

// DeleteExpiredChatLogs 手动触发清理过期对话日志（管理员）
func DeleteExpiredChatLogs(c *gin.Context) {
	deleted, err := model.DeleteChatLogsBefore(common.ChatLogRetentionDays)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("已清理 %d 条过期对话日志", deleted),
	})
}
