package model

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// ChatLog 对话日志，存储用户请求的文本内容（不含图片/文件等二进制数据）
type ChatLog struct {
	Id             int    `gorm:"primaryKey;autoIncrement" json:"id"`
	UserId         int    `gorm:"index" json:"user_id"`
	Username       string `json:"username"`
	TokenId        int    `json:"token_id"`
	TokenName      string `json:"token_name"`
	ChannelId      int    `json:"channel_id"`
	RequestId      string `gorm:"index" json:"request_id"`
	ModelName      string `json:"model_name"`
	Group          string `json:"group"`
	RequestContent string `gorm:"type:text" json:"request_content"`
	IsStream       bool   `json:"is_stream"`
	CreatedAt      int64  `gorm:"index;bigint" json:"created_at"`
}

// ChatLogFilter 对话日志查询过滤条件
type ChatLogFilter struct {
	UserId    int
	Username  string
	ModelName string
	TokenName string
	Group     string
	StartId   int
	EndId     int
	StartTime int64
	EndTime   int64
}

// RecordChatLog 写入一条对话日志
// 同一个 request_id 或同一用户短时间内相同内容只记录一次，避免重试导致重复
func RecordChatLog(info *relaycommon.RelayInfo, content string) {
	if content == "" {
		return
	}
	// 去重1：同一个 request_id 只记录一次
	if info.RequestId != "" {
		var count int64
		LOG_DB.Model(&ChatLog{}).Where("request_id = ?", info.RequestId).Count(&count)
		if count > 0 {
			return
		}
	}
	// 去重2：同一用户 10 分钟内相同内容只记录一次（防止重试/多渠道产生不同 request_id 的重复）
	if info.UserId > 0 && content != "" {
		tenMinutesAgo := time.Now().Unix() - 600
		var count int64
		LOG_DB.Model(&ChatLog{}).Where("user_id = ? AND request_content = ? AND created_at > ?", info.UserId, content, tenMinutesAgo).Count(&count)
		if count > 0 {
			return
		}
	}
	// 优先使用 Username，其次 UserEmail
	username := info.Username
	if username == "" {
		username = info.UserEmail
	}
	// TokenKey 是完整 key，截取缩略形式
	tokenDisplay := info.TokenKey
	if len(tokenDisplay) > 12 {
		tokenDisplay = tokenDisplay[:8] + "..." + tokenDisplay[len(tokenDisplay)-4:]
	}
	// 优先使用 UsingGroup（本次请求实际使用的分组），其次 UserGroup
	group := info.UsingGroup
	if group == "" {
		group = info.UserGroup
	}
	log := &ChatLog{
		UserId:         info.UserId,
		Username:       username,
		TokenId:        info.TokenId,
		TokenName:      tokenDisplay,
		ChannelId:      info.ChannelId,
		RequestId:      info.RequestId,
		ModelName:      info.OriginModelName,
		Group:          group,
		RequestContent: content,
		IsStream:       info.IsStream,
		CreatedAt:      time.Now().Unix(),
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		common.SysLog("failed to record chat log: " + err.Error())
	}
}

// GetChatLogs 分页查询对话日志
func GetChatLogs(filter ChatLogFilter, page, pageSize int) ([]*ChatLog, int64, error) {
	var logs []*ChatLog
	var total int64

	tx := LOG_DB.Model(&ChatLog{})

	if filter.UserId != 0 {
		tx = tx.Where("user_id = ?", filter.UserId)
	}
	if filter.Username != "" {
		tx = tx.Where("username LIKE ?", "%"+filter.Username+"%")
	}
	if filter.ModelName != "" {
		tx = tx.Where("model_name = ?", filter.ModelName)
	}
	if filter.TokenName != "" {
		tx = tx.Where("token_name = ?", filter.TokenName)
	}
	if filter.Group != "" {
		tx = tx.Where(logGroupCol+" = ?", filter.Group)
	}
	if filter.StartId != 0 {
		tx = tx.Where("id >= ?", filter.StartId)
	}
	if filter.EndId != 0 {
		tx = tx.Where("id <= ?", filter.EndId)
	}
	if filter.StartTime != 0 {
		tx = tx.Where("created_at >= ?", filter.StartTime)
	}
	if filter.EndTime != 0 {
		tx = tx.Where("created_at <= ?", filter.EndTime)
	}

	err := tx.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	err = tx.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&logs).Error
	return logs, total, err
}

// DeleteChatLogsBefore 删除指定天数前的对话日志
func DeleteChatLogsBefore(retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, fmt.Errorf("invalid retention days: %d", retentionDays)
	}
	threshold := time.Now().Unix() - int64(retentionDays)*24*3600
	var totalDeleted int64
	for {
		result := LOG_DB.Where("created_at < ?", threshold).Limit(1000).Delete(&ChatLog{})
		if result.Error != nil {
			return totalDeleted, result.Error
		}
		totalDeleted += result.RowsAffected
		if result.RowsAffected < 1000 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return totalDeleted, nil
}

// DeleteAllChatLogs 清空所有对话日志
func DeleteAllChatLogs() error {
	return LOG_DB.Where("1 = 1").Delete(&ChatLog{}).Error
}

// StreamAllChatLogs 流式查询对话日志，用于导出
func StreamAllChatLogs(filter ChatLogFilter, callback func(*ChatLog) error) error {
	tx := LOG_DB.Model(&ChatLog{})

	if filter.UserId != 0 {
		tx = tx.Where("user_id = ?", filter.UserId)
	}
	if filter.Username != "" {
		tx = tx.Where("username LIKE ?", "%"+filter.Username+"%")
	}
	if filter.ModelName != "" {
		tx = tx.Where("model_name = ?", filter.ModelName)
	}
	if filter.TokenName != "" {
		tx = tx.Where("token_name = ?", filter.TokenName)
	}
	if filter.Group != "" {
		tx = tx.Where(logGroupCol+" = ?", filter.Group)
	}
	if filter.StartId != 0 {
		tx = tx.Where("id >= ?", filter.StartId)
	}
	if filter.EndId != 0 {
		tx = tx.Where("id <= ?", filter.EndId)
	}
	if filter.StartTime != 0 {
		tx = tx.Where("created_at >= ?", filter.StartTime)
	}
	if filter.EndTime != 0 {
		tx = tx.Where("created_at <= ?", filter.EndTime)
	}

	rows, err := tx.Order("id DESC").Rows()
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var log ChatLog
		if err := LOG_DB.ScanRows(rows, &log); err != nil {
			return err
		}
		if err := callback(&log); err != nil {
			return err
		}
	}
	return rows.Err()
}
