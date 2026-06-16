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

// 导出明细行数上限，防止内存与带宽滥用
const exportDetailMaxRows = 500000

// 预定义四种聚合视图
const (
	exportViewDetail = "detail"
	exportViewModel  = "model"
	exportViewUser   = "user"
	exportViewToken  = "token"
)

// parseExportFilter 从 gin.Context 解析导出公共过滤参数为 LogFilter。
func parseExportFilter(c *gin.Context, forUser bool) (model.LogFilter, error) {
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	modelName := c.Query("model_name")
	username := c.Query("username")
	tokenName := c.Query("token_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	requestId := c.Query("request_id")
	upstreamRequestId := c.Query("upstream_request_id")

	filter := model.LogFilter{
		LogType:           logType,
		StartTimestamp:    startTimestamp,
		EndTimestamp:      endTimestamp,
		ModelName:         modelName,
		Username:          username,
		TokenName:         tokenName,
		Channel:           channel,
		Group:             group,
		RequestID:         requestId,
		UpstreamRequestID: upstreamRequestId,
	}
	if forUser {
		filter.UserID = c.GetInt("id")
		// 普通用户不能按 username 跨用户过滤
		filter.Username = ""
	}
	return filter, nil
}

// setCSVHeaders 设置 CSV 下载响应头（带 UTF-8 BOM 兼容 Excel 中文）。
func setCSVHeaders(c *gin.Context, filename string) {
	c.Writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
	c.Writer.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	// UTF-8 BOM，确保 Excel 正确识别中文编码
	_, _ = c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
}

// quotaToUSD 将内部 quota 值换算为 USD 字符串。
func quotaToUSD(quota int64) string {
	return fmt.Sprintf("%.4f", float64(quota)/common.QuotaPerUnit)
}

// exportAggregated 导出聚合视图（model/user/token）。
func exportAggregated(c *gin.Context, view string, filter model.LogFilter) {
	var (
		rows   []model.LogAggRow
		err    error
		keyCol string
	)
	switch view {
	case exportViewModel:
		rows, err = model.SumQuotaGroupByModel(&filter)
		keyCol = "模型"
	case exportViewUser:
		rows, err = model.SumQuotaGroupByUser(&filter)
		keyCol = "用户"
	case exportViewToken:
		rows, err = model.SumQuotaGroupByToken(&filter)
		keyCol = "令牌"
	default:
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的视图"})
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}

	filename := fmt.Sprintf("logs_%s_%s.csv", view, time.Now().Format("20060102_150405"))
	setCSVHeaders(c, filename)

	w := csv.NewWriter(c.Writer)
	if err := w.Write([]string{keyCol, "调用次数", "Prompt Tokens", "Completion Tokens", "总 Tokens", "消耗 Quota", "消耗(USD)"}); err != nil {
		return
	}
	for _, r := range rows {
		key := r.KeyName
		if key == "" {
			key = "(空)"
		}
		if err := w.Write([]string{
			key,
			strconv.FormatInt(r.Count, 10),
			strconv.FormatInt(r.PromptTokens, 10),
			strconv.FormatInt(r.CompletionTokens, 10),
			strconv.FormatInt(r.PromptTokens+r.CompletionTokens, 10),
			strconv.FormatInt(r.Quota, 10),
			quotaToUSD(r.Quota),
		}); err != nil {
			return
		}
	}
	w.Flush()
}

// exportDetail 导出明细视图（流式）。
func exportDetail(c *gin.Context, filter model.LogFilter, forUser bool) {
	filename := fmt.Sprintf("logs_detail_%s.csv", time.Now().Format("20060102_150405"))
	setCSVHeaders(c, filename)

	w := csv.NewWriter(c.Writer)
	header := []string{"时间", "用户", "令牌名", "模型", "类型", "Prompt Tokens", "Completion Tokens", "消耗 Quota", "消耗(USD)", "耗时(ms)", "渠道", "分组", "请求ID"}
	if !forUser {
		// 管理员额外输出渠道 ID
		header = append(header, "渠道ID")
	}
	if err := w.Write(header); err != nil {
		return
	}

	totalRows := 0
	_, err := model.StreamAllLogs(&filter, func(logs []*model.Log) error {
		if forUser {
			model.SanitizeUserLogs(logs)
		}
		for _, log := range logs {
			row := []string{
				time.Unix(log.CreatedAt, 0).Format("2006-01-02 15:04:05"),
				log.Username,
				log.TokenName,
				log.ModelName,
				logTypeName(log.Type),
				strconv.Itoa(log.PromptTokens),
				strconv.Itoa(log.CompletionTokens),
				strconv.Itoa(log.Quota),
				quotaToUSD(int64(log.Quota)),
				strconv.Itoa(log.UseTime),
				log.ChannelName,
				log.Group,
				log.RequestId,
			}
			if !forUser {
				row = append(row, strconv.Itoa(log.ChannelId))
			}
			if err := w.Write(row); err != nil {
				return err
			}
		}
		w.Flush()
		totalRows += len(logs)
		if totalRows >= exportDetailMaxRows {
			return model.ErrExportRowsLimitReached
		}
		return nil
	})

	// 行数上限属于预期行为，flush 后正常结束
	if err != nil && err != model.ErrExportRowsLimitReached {
		// 响应已开始流式写入，只能记录错误日志
		common.SysError("export detail logs error: " + err.Error())
	}
	w.Flush()
}

// logTypeName 将日志类型枚举转为中文名称。
func logTypeName(t int) string {
	switch t {
	case model.LogTypeConsume:
		return "消费"
	case model.LogTypeTopup:
		return "充值"
	case model.LogTypeManage:
		return "管理"
	case model.LogTypeSystem:
		return "系统"
	case model.LogTypeError:
		return "错误"
	case model.LogTypeRefund:
		return "退款"
	case model.LogTypeLogin:
		return "登录"
	default:
		return "未知"
	}
}

// ExportLogs 管理员导出日志（全量）。
func ExportLogs(c *gin.Context) {
	filter, err := parseExportFilter(c, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	view := c.DefaultQuery("view", exportViewDetail)
	doExport(c, view, filter, false)
}

// ExportSelfLogs 普通用户导出自己的日志。
func ExportSelfLogs(c *gin.Context) {
	filter, err := parseExportFilter(c, true)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	view := c.DefaultQuery("view", exportViewDetail)
	doExport(c, view, filter, true)
}

func doExport(c *gin.Context, view string, filter model.LogFilter, forUser bool) {
	switch view {
	case exportViewModel, exportViewUser, exportViewToken:
		exportAggregated(c, view, filter)
	case exportViewDetail:
		exportDetail(c, filter, forUser)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的导出视图，支持: detail/model/user/token"})
	}
}
