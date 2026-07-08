package controller

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

type HeadroomLogRow struct {
	CreatedAt     int64   `json:"created_at"`
	Username      string  `json:"username"`
	ModelName     string  `json:"model_name"`
	TokenName     string  `json:"token_name"`
	ChannelId     int     `json:"channel_id"`
	ChannelName   string  `json:"channel_name"`
	RequestID     string  `json:"request_id"`
	HeadroomSaved int     `json:"headroom_tokens_saved"`
	HeadroomInput int     `json:"headroom_tokens_input"`
	HeadroomRatio float64 `json:"headroom_ratio"`
	// PromptTokens 是使用日志里记录的实际输入 token（压缩后），与使用日志一致
	PromptTokens      int `json:"prompt_tokens"`
	CompletionTokens  int `json:"completion_tokens"`
}

type HeadroomAggRow struct {
	Name         string  `json:"name"`
	TokensSaved  int     `json:"tokens_saved"`
	TokensInput  int     `json:"tokens_input"`
	RequestCount int     `json:"request_count"`
	AverageRatio float64 `json:"average_ratio"`
}

// getHeadroomRows 读取 headroom 明细行，应用 HeadroomRetentionDays 留存窗口限制。
func getHeadroomRows(startTs, endTs int64) ([]HeadroomLogRow, error) {
	return getHeadroomRowsImpl(startTs, endTs, true)
}

// getAllHeadroomRows 读取 headroom 明细行，不应用留存窗口限制，用于历史趋势报表。
func getAllHeadroomRows(startTs, endTs int64) ([]HeadroomLogRow, error) {
	return getHeadroomRowsImpl(startTs, endTs, false)
}

func getHeadroomRowsImpl(startTs, endTs int64, applyRetention bool) ([]HeadroomLogRow, error) {
	// 应用留存天数过滤（仅常规看板，趋势报表绕过）
	if applyRetention {
		retentionDays := operation_setting.HeadroomRetentionDays
		if retentionDays > 0 {
			retentionStart := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).Unix()
			if startTs == 0 || startTs < retentionStart {
				startTs = retentionStart
			}
		}
	}

	var logs []model.Log
	query := model.LOG_DB.Model(&model.Log{}).
		Where("type = ?", model.LogTypeConsume).
		Where("other <> ''")
	if startTs > 0 {
		query = query.Where("created_at >= ?", startTs)
	}
	if endTs > 0 {
		query = query.Where("created_at <= ?", endTs)
	}
	if err := query.Order("created_at desc").Limit(50000).Find(&logs).Error; err != nil {
		return nil, err
	}

	// 批量解析渠道名
	channelIds := make(map[int]bool)
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIds[log.ChannelId] = true
		}
	}
	channelNames := make(map[int]string)
	if len(channelIds) > 0 {
		for id := range channelIds {
			if ch, err := model.GetChannelById(id, false); err == nil {
				channelNames[id] = ch.Name
			}
		}
	}

	rows := make([]HeadroomLogRow, 0)
	for _, log := range logs {
		var other map[string]interface{}
		if err := common.UnmarshalJsonStr(log.Other, &other); err != nil {
			continue
		}
		saved := intFromAny(other["headroom_tokens_saved"])
		if saved <= 0 {
			continue
		}
		input := intFromAny(other["headroom_tokens_input"])
		ratio := floatFromAny(other["headroom_ratio"])
		chName := channelNames[log.ChannelId]
		rows = append(rows, HeadroomLogRow{
			CreatedAt:        log.CreatedAt,
			Username:         log.Username,
			ModelName:        log.ModelName,
			TokenName:        log.TokenName,
			ChannelId:        log.ChannelId,
			ChannelName:      chName,
			RequestID:        log.RequestId,
			HeadroomSaved:    saved,
			HeadroomInput:    input,
			HeadroomRatio:    ratio,
			PromptTokens:     log.PromptTokens,
			CompletionTokens: log.CompletionTokens,
		})
	}
	return rows, nil
}

func intFromAny(v interface{}) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	default:
		return 0
	}
}

func floatFromAny(v interface{}) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	default:
		return 0.0
	}
}

func aggregateHeadroom(rows []HeadroomLogRow, keyFn func(HeadroomLogRow) string) []HeadroomAggRow {
	m := map[string]*HeadroomAggRow{}
	ratioSum := map[string]float64{}
	for _, r := range rows {
		key := keyFn(r)
		if key == "" {
			key = "(空)"
		}
		if m[key] == nil {
			m[key] = &HeadroomAggRow{Name: key}
		}
		m[key].TokensSaved += r.HeadroomSaved
		m[key].TokensInput += r.HeadroomInput
		m[key].RequestCount++
		ratioSum[key] += r.HeadroomRatio
	}
	out := make([]HeadroomAggRow, 0, len(m))
	for k, v := range m {
		if v.RequestCount > 0 {
			v.AverageRatio = ratioSum[k] / float64(v.RequestCount)
		}
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TokensSaved > out[j].TokensSaved })
	return out
}

func parseHeadroomTime(c *gin.Context) (int64, int64) {
	startTs, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTs, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if startTs == 0 {
		startTs = time.Now().Add(-30 * 24 * time.Hour).Unix()
	}
	if endTs == 0 {
		endTs = time.Now().Unix()
	}
	return startTs, endTs
}

func GetHeadroomSummary(c *gin.Context) {
	startTs, endTs := parseHeadroomTime(c)
	rows, err := getHeadroomRows(startTs, endTs)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	totalSaved, totalInput, totalCompressed := 0, 0, 0
	for _, r := range rows {
		totalSaved += r.HeadroomSaved
		totalInput += r.HeadroomInput
		totalCompressed += r.HeadroomInput - r.HeadroomSaved // 实际发送给上游的 token
	}
	ratio := 0.0
	if totalInput > 0 {
		ratio = float64(totalSaved) / float64(totalInput)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"request_count":      len(rows),
		"tokens_saved":       totalSaved,
		"tokens_input":       totalInput,       // 原始输入（压缩前）
		"tokens_compressed":  totalCompressed,  // 实际发送（压缩后）
		"average_ratio":      ratio,
	}})
}

func GetHeadroomByModel(c *gin.Context) {
	startTs, endTs := parseHeadroomTime(c)
	rows, err := getHeadroomRows(startTs, endTs)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": aggregateHeadroom(rows, func(r HeadroomLogRow) string { return r.ModelName })})
}

func GetHeadroomByUser(c *gin.Context) {
	startTs, endTs := parseHeadroomTime(c)
	rows, err := getHeadroomRows(startTs, endTs)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": aggregateHeadroom(rows, func(r HeadroomLogRow) string { return r.Username })})
}

func GetHeadroomByChannel(c *gin.Context) {
	startTs, endTs := parseHeadroomTime(c)
	rows, err := getHeadroomRows(startTs, endTs)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": aggregateHeadroom(rows, func(r HeadroomLogRow) string {
		if r.ChannelName != "" {
			return r.ChannelName
		}
		if r.ChannelId > 0 {
			return fmt.Sprintf("渠道 #%d", r.ChannelId)
		}
		return "(未知渠道)"
	})})
}

func GetHeadroomRecent(c *gin.Context) {
	startTs, endTs := parseHeadroomTime(c)
	rows, err := getHeadroomRows(startTs, endTs)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 支持分页：page 从 1 开始，默认每页 100 条
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "100"))
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 100
	}
	total := len(rows)
	start := (page - 1) * pageSize
	if start >= total {
		rows = []HeadroomLogRow{}
	} else {
		end := start + pageSize
		if end > total {
			end = total
		}
		rows = rows[start:end]
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows, "total": total, "page": page, "page_size": pageSize})
}

// ExportHeadroom 导出 Headroom 压缩统计 CSV
// view: summary(汇总) / model(按模型) / user(按用户) / channel(按渠道) / detail(明细)
func ExportHeadroom(c *gin.Context) {
	startTs, endTs := parseHeadroomTime(c)
	rows, err := getHeadroomRows(startTs, endTs)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	view := c.DefaultQuery("view", "detail")
	filename := fmt.Sprintf("headroom_%s_%s.csv", view, time.Now().Format("20060102_150405"))
	setCSVHeaders(c, filename)
	w := csv.NewWriter(c.Writer)

	switch view {
	case "model":
		w.Write([]string{"模型", "压缩次数", "节省 Tokens", "原输入 Tokens", "平均压缩率"})
		for _, r := range aggregateHeadroom(rows, func(r HeadroomLogRow) string { return r.ModelName }) {
			w.Write([]string{r.Name, strconv.Itoa(r.RequestCount), strconv.Itoa(r.TokensSaved),
				strconv.Itoa(r.TokensInput), fmt.Sprintf("%.1f%%", r.AverageRatio*100)})
		}
	case "user":
		w.Write([]string{"用户", "压缩次数", "节省 Tokens", "原输入 Tokens", "平均压缩率"})
		for _, r := range aggregateHeadroom(rows, func(r HeadroomLogRow) string { return r.Username }) {
			w.Write([]string{r.Name, strconv.Itoa(r.RequestCount), strconv.Itoa(r.TokensSaved),
				strconv.Itoa(r.TokensInput), fmt.Sprintf("%.1f%%", r.AverageRatio*100)})
		}
	case "channel":
		w.Write([]string{"渠道", "压缩次数", "节省 Tokens", "原输入 Tokens", "平均压缩率"})
		for _, r := range aggregateHeadroom(rows, func(r HeadroomLogRow) string {
			if r.ChannelName != "" {
				return r.ChannelName
			}
			return fmt.Sprintf("渠道 #%d", r.ChannelId)
		}) {
			w.Write([]string{r.Name, strconv.Itoa(r.RequestCount), strconv.Itoa(r.TokensSaved),
				strconv.Itoa(r.TokensInput), fmt.Sprintf("%.1f%%", r.AverageRatio*100)})
		}
	case "monthly":
		data, _ := aggregateHeadroomByGranularity("month")
		w.Write([]string{"月份", "节省 Tokens", "原输入 Tokens", "实际发送 Tokens", "请求数", "节省率"})
		for _, r := range data {
			w.Write([]string{r.Time, strconv.Itoa(r.TokensSaved), strconv.Itoa(r.TokensInput),
				strconv.Itoa(r.TokensCompressed), strconv.Itoa(r.RequestCount),
				fmt.Sprintf("%.1f%%", r.AverageRatio*100)})
		}
	case "yearly":
		data, _ := aggregateHeadroomByGranularity("year")
		w.Write([]string{"年份", "节省 Tokens", "原输入 Tokens", "实际发送 Tokens", "请求数", "节省率"})
		for _, r := range data {
			w.Write([]string{r.Time, strconv.Itoa(r.TokensSaved), strconv.Itoa(r.TokensInput),
				strconv.Itoa(r.TokensCompressed), strconv.Itoa(r.RequestCount),
				fmt.Sprintf("%.1f%%", r.AverageRatio*100)})
		}
	default: // detail
		w.Write([]string{"时间", "用户", "令牌", "模型", "渠道", "原输入 Tokens", "节省 Tokens", "实际输入 Tokens", "输出 Tokens", "压缩率", "请求ID"})
		for _, r := range rows {
			chName := r.ChannelName
			if chName == "" {
				chName = fmt.Sprintf("渠道 #%d", r.ChannelId)
			}
			w.Write([]string{
				time.Unix(r.CreatedAt, 0).Format("2006-01-02 15:04:05"),
				r.Username, r.TokenName, r.ModelName, chName,
				strconv.Itoa(r.HeadroomInput), strconv.Itoa(r.HeadroomSaved),
				strconv.Itoa(r.PromptTokens), strconv.Itoa(r.CompletionTokens),
				fmt.Sprintf("%.1f%%", r.HeadroomRatio*100), r.RequestID,
			})
		}
	}
	w.Flush()
}

// HeadroomTrendRow 历史趋势聚合行，按时间桶（日/周/月）聚合。
type HeadroomTrendRow struct {
	Time             string  `json:"time"`
	Granularity      string  `json:"granularity"`
	TokensSaved      int     `json:"tokens_saved"`
	TokensInput      int     `json:"tokens_input"`
	TokensCompressed int     `json:"tokens_compressed"`
	RequestCount     int     `json:"request_count"`
	AverageRatio     float64 `json:"average_ratio"`
}

// formatHeadroomBucket 把 Unix 时间戳按指定粒度格式化为桶标签。
//   - day:   "2006-01-02"
//   - week:  "2006-W__"（ISO 周编号）
//   - month: "2006-01"
//   - year:  "2006"
func formatHeadroomBucket(ts int64, granularity string) string {
	t := time.Unix(ts, 0)
	switch granularity {
	case "day":
		return t.Format("2006-01-02")
	case "week":
		year, week := t.ISOWeek()
		return fmt.Sprintf("%d-W%02d", year, week)
	case "month":
		return t.Format("2006-01")
	case "year":
		return t.Format("2006")
	default:
		return t.Format("2006-01-02")
	}
}

// pickHeadroomGranularity 根据时间跨度自动选择合适的粒度。
func pickHeadroomGranularity(startTs, endTs int64) string {
	spanDays := (endTs - startTs) / 86400
	if spanDays < 0 {
		spanDays = 0
	}
	switch {
	case spanDays <= 60:
		return "day"
	case spanDays <= 365:
		return "week"
	default:
		return "month"
	}
}

// GetHeadroomTrend 历史趋势报表：按时间桶聚合，绕过留存窗口限制。
func GetHeadroomTrend(c *gin.Context) {
	startTs, endTs := parseHeadroomTime(c)
	granularity := pickHeadroomGranularity(startTs, endTs)

	// 趋势报表绕过 HeadroomRetentionDays，读取全部历史 headroom 数据
	rows, err := getAllHeadroomRows(startTs, endTs)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 内存中按桶聚合（不使用 SQL date 函数，确保三种 DB 行为一致）
	type bucket struct {
		Saved      int
		Input      int
		Count      int
		RatioSum   float64
		FirstTs    int64 // 用于排序
	}
	buckets := map[string]*bucket{}
	for _, r := range rows {
		key := formatHeadroomBucket(r.CreatedAt, granularity)
		b := buckets[key]
		if b == nil {
			b = &bucket{FirstTs: r.CreatedAt}
			buckets[key] = b
		}
		b.Saved += r.HeadroomSaved
		b.Input += r.HeadroomInput
		b.Count++
		b.RatioSum += r.HeadroomRatio
		if r.CreatedAt < b.FirstTs {
			b.FirstTs = r.CreatedAt
		}
	}

	out := make([]HeadroomTrendRow, 0, len(buckets))
	for key, b := range buckets {
		ratio := 0.0
		if b.Input > 0 {
			ratio = float64(b.Saved) / float64(b.Input)
		}
		out = append(out, HeadroomTrendRow{
			Time:             key,
			Granularity:      granularity,
			TokensSaved:      b.Saved,
			TokensInput:      b.Input,
			TokensCompressed: b.Input - b.Saved,
			RequestCount:     b.Count,
			AverageRatio:     ratio,
		})
	}

	// 按 time 升序输出（桶标签按字典序正好等于时间序）
	sort.Slice(out, func(i, j int) bool { return out[i].Time < out[j].Time })

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"data":       out,
		"granularity": granularity,
	})
}

// aggregateHeadroomByGranularity 按指定粒度聚合全部历史 headroom 数据，绕过留存窗口。
func aggregateHeadroomByGranularity(granularity string) ([]HeadroomTrendRow, error) {
	rows, err := getAllHeadroomRows(0, 0)
	if err != nil {
		return nil, err
	}
	type bucket struct {
		Saved    int
		Input    int
		Count    int
	}
	buckets := map[string]*bucket{}
	for _, r := range rows {
		key := formatHeadroomBucket(r.CreatedAt, granularity)
		b := buckets[key]
		if b == nil {
			b = &bucket{}
			buckets[key] = b
		}
		b.Saved += r.HeadroomSaved
		b.Input += r.HeadroomInput
		b.Count++
	}
	out := make([]HeadroomTrendRow, 0, len(buckets))
	for key, b := range buckets {
		ratio := 0.0
		if b.Input > 0 {
			ratio = float64(b.Saved) / float64(b.Input)
		}
		out = append(out, HeadroomTrendRow{
			Time:             key,
			Granularity:      granularity,
			TokensSaved:      b.Saved,
			TokensInput:      b.Input,
			TokensCompressed: b.Input - b.Saved,
			RequestCount:     b.Count,
			AverageRatio:     ratio,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time < out[j].Time })
	return out, nil
}

// GetHeadroomMonthly 月度汇总报表：按月聚合全部历史 headroom 数据。
func GetHeadroomMonthly(c *gin.Context) {
	data, err := aggregateHeadroomByGranularity("month")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

// GetHeadroomYearly 年度汇总报表：按年聚合全部历史 headroom 数据。
func GetHeadroomYearly(c *gin.Context) {
	data, err := aggregateHeadroomByGranularity("year")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}
