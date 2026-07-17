package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

var chatLogCleanupOnce sync.Once

const chatLogCleanupLogPrefix = "[ChatLog Cleanup]"

// StartChatLogCleanupTask 启动对话日志定时清理任务
// 每小时检查一次，删除超过保留期的对话日志
func StartChatLogCleanupTask() {
	if !common.IsMasterNode {
		return
	}
	chatLogCleanupOnce.Do(func() {
		common.SysLog(chatLogCleanupLogPrefix + " 定时清理任务已启动")
		go func() {
			defer func() {
				if r := recover(); r != nil {
					common.SysError(chatLogCleanupLogPrefix + " panic: " + fmt.Sprintf("%v", r))
				}
			}()

			ticker := time.NewTicker(1 * time.Hour)
			defer ticker.Stop()

			// 启动时先执行一次
			cleanupExpiredChatLogs()

			for range ticker.C {
				cleanupExpiredChatLogs()
			}
		}()
	})
}

// cleanupExpiredChatLogs 清理过期的对话日志
func cleanupExpiredChatLogs() {
	if common.ChatLogRetentionDays <= 0 {
		return
	}

	deleted, err := model.DeleteChatLogsBefore(common.ChatLogRetentionDays)
	if err != nil {
		common.SysError(chatLogCleanupLogPrefix + fmt.Sprintf(" 清理失败: %v", err))
		return
	}
	if deleted > 0 {
		common.SysLog(chatLogCleanupLogPrefix + fmt.Sprintf(" 已清理 %d 条过期对话日志 (保留%d天)", deleted, common.ChatLogRetentionDays))
	}
}
