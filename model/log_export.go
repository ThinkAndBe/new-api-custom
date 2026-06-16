package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"gorm.io/gorm"
)

// ErrExportRowsLimitReached 导出明细达到行数上限，属于预期终止。
var ErrExportRowsLimitReached = errors.New("export rows limit reached")

// LogFilter 封装日志查询的过滤条件，供聚合查询与流式明细导出复用。
type LogFilter struct {
	UserID            int
	LogType           int
	StartTimestamp    int64
	EndTimestamp      int64
	ModelName         string
	Username          string
	TokenName         string
	Channel           int
	Group             string
	RequestID         string
	UpstreamRequestID string
}

// applyWithPrefix 将过滤条件应用到查询上。
// prefix 为 "logs." 时用于 Model(&Log{}) 模式（列名带表前缀），
// 为空时用于 Table("logs") 聚合模式。
func (f *LogFilter) applyWithPrefix(tx *gorm.DB, prefix string) (*gorm.DB, error) {
	var err error
	if f.UserID != 0 {
		tx = tx.Where(prefix+"user_id = ?", f.UserID)
	}
	if f.LogType != LogTypeUnknown {
		tx = tx.Where(prefix+"type = ?", f.LogType)
	}
	if tx, err = applyExplicitLogTextFilter(tx, prefix+"model_name", f.ModelName); err != nil {
		return nil, err
	}
	if tx, err = applyExplicitLogTextFilter(tx, prefix+"username", f.Username); err != nil {
		return nil, err
	}
	if f.TokenName != "" {
		tx = tx.Where(prefix+"token_name = ?", f.TokenName)
	}
	if f.RequestID != "" {
		tx = tx.Where(prefix+"request_id = ?", f.RequestID)
	}
	if f.UpstreamRequestID != "" {
		tx = tx.Where(prefix+"upstream_request_id = ?", f.UpstreamRequestID)
	}
	if f.StartTimestamp != 0 {
		tx = tx.Where(prefix+"created_at >= ?", f.StartTimestamp)
	}
	if f.EndTimestamp != 0 {
		tx = tx.Where(prefix+"created_at <= ?", f.EndTimestamp)
	}
	if f.Channel != 0 {
		tx = tx.Where(prefix+"channel_id = ?", f.Channel)
	}
	if f.Group != "" {
		tx = tx.Where(prefix+logGroupCol+" = ?", f.Group)
	}
	return tx, nil
}

func (f *LogFilter) ApplyToModel(tx *gorm.DB) (*gorm.DB, error) {
	return f.applyWithPrefix(tx, "logs.")
}

func (f *LogFilter) ApplyToTable(tx *gorm.DB) (*gorm.DB, error) {
	return f.applyWithPrefix(tx, "")
}

// LogAggRow 日志聚合查询的结果行（按模型/用户/令牌维度聚合）。
type LogAggRow struct {
	KeyName          string `gorm:"column:key_name" json:"key_name"`
	Count            int64  `gorm:"column:count" json:"count"`
	Quota            int64  `gorm:"column:quota" json:"quota"`
	PromptTokens     int64  `gorm:"column:prompt_tokens" json:"prompt_tokens"`
	CompletionTokens int64  `gorm:"column:completion_tokens" json:"completion_tokens"`
}

// sumQuotaGroupBy 按指定列分组聚合消费日志统计。
// groupCol 不含关键字冲突（model_name / username / token_name 均安全）。
func sumQuotaGroupBy(filter *LogFilter, groupCol string) ([]LogAggRow, error) {
	// 强制只统计消费日志
	f := *filter
	f.LogType = LogTypeConsume

	selectExpr := groupCol + " as key_name, count(*) as count, " +
		"sum(quota) as quota, sum(prompt_tokens) as prompt_tokens, sum(completion_tokens) as completion_tokens"
	tx := LOG_DB.Table("logs").Select(selectExpr)
	tx, err := f.ApplyToTable(tx)
	if err != nil {
		return nil, err
	}
	var rows []LogAggRow
	err = tx.Group(groupCol).Order("quota desc").Find(&rows).Error
	if err != nil {
		common.SysError("failed to query aggregated logs: " + err.Error())
		return nil, errors.New("查询聚合数据失败")
	}
	return rows, nil
}

// SumQuotaGroupByModel 按模型聚合消费统计。
func SumQuotaGroupByModel(filter *LogFilter) ([]LogAggRow, error) {
	return sumQuotaGroupBy(filter, "model_name")
}

// SumQuotaGroupByUser 按用户聚合消费统计。
func SumQuotaGroupByUser(filter *LogFilter) ([]LogAggRow, error) {
	return sumQuotaGroupBy(filter, "username")
}

// SumQuotaGroupByToken 按令牌名聚合消费统计。
func SumQuotaGroupByToken(filter *LogFilter) ([]LogAggRow, error) {
	return sumQuotaGroupBy(filter, "token_name")
}

// resolveChannelNames 为一批日志批量填充 ChannelName，复用内存缓存。
func resolveChannelNames(logs []*Log) {
	channelIds := types.NewSet[int]()
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIds.Add(log.ChannelId)
		}
	}
	if channelIds.Len() == 0 {
		return
	}
	var channels []struct {
		Id   int    `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}
	if common.MemoryCacheEnabled {
		for _, channelId := range channelIds.Items() {
			if cacheChannel, err := CacheGetChannel(channelId); err == nil {
				channels = append(channels, struct {
					Id   int    `gorm:"column:id"`
					Name string `gorm:"column:name"`
				}{
					Id:   channelId,
					Name: cacheChannel.Name,
				})
			}
		}
	} else {
		if err := DB.Table("channels").Select("id, name").Where("id IN ?", channelIds.Items()).Find(&channels).Error; err != nil {
			common.SysError("failed to query channel names: " + err.Error())
			return
		}
	}
	channelMap := make(map[int]string, len(channels))
	for _, ch := range channels {
		channelMap[ch.Id] = ch.Name
	}
	for i := range logs {
		logs[i].ChannelName = channelMap[logs[i].ChannelId]
	}
}

const (
	exportBatchSize = 5000   // 明细导出每批拉取行数
	exportMaxRows   = 500000 // 明细导出最大行数上限，防止 OOM
)

// StreamAllLogs 按 id 升序分批流式获取满足过滤条件的明细日志，每批回调 handle。
// 采用基于 id 的 keyset 分页以避免大 offset 性能问题。
// 注意：handle 回调内不应依赖 Log.Id 做分页（用户脱敏可能改写 Id），
// 内部已使用原始 id 推进游标。
func StreamAllLogs(filter *LogFilter, handle func(logs []*Log) error) (int, error) {
	lastId := 0
	totalRows := 0
	for {
		var batch []*Log
		tx := LOG_DB.Model(&Log{}).Where("logs.id > ?", lastId).
			Order("logs.id asc").Limit(exportBatchSize)
		tx, err := filter.ApplyToModel(tx)
		if err != nil {
			return totalRows, err
		}
		if err := tx.Find(&batch).Error; err != nil {
			common.SysError("failed to stream logs: " + err.Error())
			return totalRows, errors.New("查询日志明细失败")
		}
		if len(batch) == 0 {
			break
		}
		// 先用原始 id 推进游标，再交给回调（回调内可能改写 Id）
		lastId = batch[len(batch)-1].Id
		resolveChannelNames(batch)
		if err := handle(batch); err != nil {
			return totalRows, err
		}
		totalRows += len(batch)
		if totalRows >= exportMaxRows {
			break
		}
	}
	return totalRows, nil
}

// SanitizeUserLogs 清理用户视角下的管理员敏感字段（导出给普通用户时使用）。
// 不改写 Id（与 formatUserLogs 不同，避免破坏流式游标）。
func SanitizeUserLogs(logs []*Log) {
	for i := range logs {
		logs[i].ChannelName = ""
		logs[i].ChannelId = 0
		if logs[i].Other != "" {
			otherMap, _ := common.StrToMap(logs[i].Other)
			if otherMap != nil {
				delete(otherMap, "admin_info")
				delete(otherMap, "audit_info")
				delete(otherMap, "stream_status")
				logs[i].Other = common.MapToJsonStr(otherMap)
			}
		}
	}
}
