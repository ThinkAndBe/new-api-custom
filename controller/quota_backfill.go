package controller

// quota_backfill.go — quota_data 事故窗口回填（一次性管理操作）。
//
// 背景：734dfa51（09-14 14:06）误删 go model.UpdateQuotaData()，至 e0515c02
// 部署期间用量只进内存未落库，quota_data 出现空档。logs 表明细完整，
// 可按小时×用户×模型×渠道聚合回填空档，恢复看板排行与曲线。
//
// POST /api/shadow/quota_backfill?start=&end=  （RootAuth，幂等可重复执行：
// 回填前删除窗口内既有 quota_data 行再插入，避免重复累计）

import (
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// BackfillQuotaData POST /api/quota_backfill?start=2026-09-14&end=2026-09-16
// 从 logs 表(type=2 消费)聚合用量回填 quota_data 指定日期窗口
func BackfillQuotaData(c *gin.Context) {
	parseDay := func(s string) (time.Time, error) {
		return time.ParseInLocation("2006-01-02", s, time.Local)
	}
	start, err := parseDay(c.Query("start"))
	if err != nil {
		common.ApiErrorMsg(c, "start 日期格式应为 YYYY-MM-DD")
		return
	}
	end, err := parseDay(c.Query("end"))
	if err != nil {
		common.ApiErrorMsg(c, "end 日期格式应为 YYYY-MM-DD")
		return
	}
	if !end.After(start) {
		common.ApiErrorMsg(c, "end 必须晚于 start")
	}
	if end.Sub(start) > 31*24*time.Hour {
		common.ApiErrorMsg(c, "单次回填窗口不得超过 31 天")
		return
	}
	startTs, endTs := start.Unix(), end.Unix()

	// 幂等：清掉窗口内已落库的 quota_data（事故窗口内通常只有零星缓存落盘）
	delRes := model.DB.Where("created_at >= ? AND created_at < ?", startTs, endTs).
		Delete(&model.QuotaData{})

	// 从 logs 聚合：按 小时×用户×模型×渠道 分组
	// channel 列在 logs 表名为 channel（struct tag gorm:"column:channel"→映射 channel_id? 检查）
	type aggRow struct {
		Hour      int64  `gorm:"column:hour_ts"`
		UserId    int    `gorm:"column:user_id"`
		Username  string `gorm:"column:username"`
		ModelName string `gorm:"column:model_name"`
		ChannelId int    `gorm:"column:channel_id"`
		Cnt       int64  `gorm:"column:cnt"`
		Quota     int64  `gorm:"column:quota_sum"`
		Tokens    int64  `gorm:"column:tokens_sum"`
	}
	var rows []aggRow
	err = model.LOG_DB.Table("logs").
		Select(fmt.Sprintf(
			"((created_at / 3600) * 3600) AS hour_ts, user_id, MAX(username) AS username, "+
				"model_name, channel_id, COUNT(*) AS cnt, SUM(quota) AS quota_sum, "+
				"SUM(prompt_tokens + completion_tokens) AS tokens_sum")).
		Where("created_at >= ? AND created_at < ? AND type = ?", startTs, endTs, model.LogTypeConsume).
		Group("hour_ts, user_id, model_name, channel_id").
		Order("hour_ts").Find(&rows).Error
	if err != nil {
		common.ApiError(c, err)
		return
	}

	inserts := make([]model.QuotaData, 0, len(rows))
	for _, r := range rows {
		inserts = append(inserts, model.QuotaData{
			UserID:    r.UserId,
			Username:  r.Username,
			ModelName: r.ModelName,
			CreatedAt: r.Hour,
			Count:     int(r.Cnt),
			Quota:     int(r.Quota),
			TokenUsed: int(r.Tokens),
			ChannelId: r.ChannelId,
		})
	}
	inserted := 0
	if len(inserts) > 0 {
		if err := model.DB.CreateInBatches(inserts, 200).Error; err != nil {
			common.ApiError(c, err)
			return
		}
		inserted = len(inserts)
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeSystem,
		fmt.Sprintf("quota_data 回填 %s~%s：删 %d 行旧数据，插入 %d 行聚合",
			start.Format("2006-01-02"), end.Format("2006-01-02"), delRes.RowsAffected, inserted))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("回填完成：清除窗口内旧数据 %d 行，从 logs 聚合插入 %d 行（覆盖 %s 至 %s）",
			delRes.RowsAffected, inserted, start.Format("01-02 15:04"), end.Format("01-02")),
		"data": gin.H{"rows_inserted": inserted, "rows_deleted": delRes.RowsAffected},
	})
}
