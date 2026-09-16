package controller

// open_api_report.go — 数据开放接口的「开箱即用」三个端点（密钥鉴权，见 OpenKeyAuth）。
//
// 面向顾问等外部消费方，目标是「一次调用就能拿到做总结/合规审查所需的全部信息」：
//   GET /api/open/usage     模型调用情况（次数/token/费用，可按用户、模型、用户×模型汇总）
//   GET /api/open/contents  调用内容（请求/回复原文，可截断，format=text 直接喂给 AI）
//   GET /api/open/report    一站式：用量汇总 + 各用户模型明细 + 内容样本（含现成的审查提示词）
//
// 权限沿用密钥 Scope：分组/用户圈定数据范围、include_content 决定是否给出内容、max_days 限制回看天数。

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

var openTimeLayouts = []string{"2006-01-02 15:04:05", "2006-01-02", time.RFC3339}

// openParseRange 解析 start/end（缺省=最近 defDays 天到今天），并按密钥回看天数上限裁剪。
func openParseRange(c *gin.Context, scope *model.OpenKeyScope, defDays int) (time.Time, time.Time, string) {
	now := time.Now()
	parse := func(s string, def time.Time) (time.Time, error) {
		s = strings.TrimSpace(s)
		if s == "" {
			return def, nil
		}
		for _, layout := range openTimeLayouts {
			if t, e := time.ParseInLocation(layout, s, time.Local); e == nil {
				return t, nil
			}
		}
		return time.Time{}, fmt.Errorf("时间格式应为 YYYY-MM-DD 或 YYYY-MM-DD HH:mm:ss")
	}
	start, err := parse(c.Query("start"), now.AddDate(0, 0, -defDays))
	if err != nil {
		return start, start, "start " + err.Error()
	}
	end, err := parse(c.Query("end"), now)
	if err != nil {
		return start, start, "end " + err.Error()
	}
	if end.Before(start) {
		start, end = end, start
	}
	if earliest := now.AddDate(0, 0, -scope.MaxDays); start.Before(earliest) {
		start = earliest
	}
	if end.After(now) {
		end = now
	}
	return start, end, ""
}

// openRequestedUsernames 解析 username（单个，模糊）/ users（逗号分隔，精确）并做权限校验。
// 返回 nil 表示「不做用户过滤」。
func openRequestedUsernames(c *gin.Context, allowed []string) ([]string, string) {
	raw := strings.TrimSpace(c.Query("users"))
	single := strings.TrimSpace(c.Query("username"))
	if raw == "" && single == "" {
		if allowed == nil {
			return nil, ""
		}
		return allowed, "" // 密钥限定了范围：把范围下推成过滤条件
	}
	var names []string
	if raw != "" {
		for _, n := range strings.Split(raw, ",") {
			if n = strings.TrimSpace(n); n != "" {
				names = append(names, n)
			}
		}
	} else {
		names = []string{single}
	}
	if allowed != nil {
		set := make(map[string]bool, len(allowed))
		for _, u := range allowed {
			set[u] = true
		}
		for _, n := range names {
			if !set[n] {
				return nil, fmt.Sprintf("用户「%s」不在该密钥的数据范围内", n)
			}
		}
	}
	return names, ""
}

func openCostCNY(quota int64) float64 {
	if common.QuotaPerUnit <= 0 {
		return 0
	}
	return float64(quota) / common.QuotaPerUnit
}

func openFmtTime(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return time.Unix(ts, 0).Format("2006-01-02 15:04:05")
}

// openTruncate 按 max_chars 截断内容（<=0 表示不截断）
func openTruncate(s string, maxChars int) (string, bool) {
	if maxChars <= 0 || len(s) <= maxChars {
		return s, false
	}
	// 按字符（rune）截断，避免把多字节汉字切坏
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s, false
	}
	return string(runes[:maxChars]), true
}

func openIntParam(c *gin.Context, name string, def, min, max int) int {
	v, _ := strconv.Atoi(c.Query(name))
	if v < min {
		v = def
	}
	if max > 0 && v > max {
		v = max
	}
	return v
}

// ---- 调用情况：/api/open/usage ----

func OpenQueryUsage(c *gin.Context) {
	scope := c.MustGet("open_scope").(*model.OpenKeyScope)

	start, end, msg := openParseRange(c, scope, 7)
	if msg != "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": msg})
		return
	}
	allowed, err := model.ScopeAllowedUsernames(scope)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "权限解析失败: " + err.Error()})
		return
	}
	names, msg := openRequestedUsernames(c, allowed)
	if msg != "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": msg})
		return
	}

	groupBy := strings.TrimSpace(c.Query("group_by"))
	if groupBy != "model" && groupBy != "user_model" {
		groupBy = "user"
	}
	rows, err := model.GetOpenUsageStats(model.OpenUsageFilter{
		StartTime: start.Unix(),
		EndTime:   end.Unix(),
		Usernames: names,
		Model:     strings.TrimSpace(c.Query("model")),
		Group:     strings.TrimSpace(c.Query("group")),
		GroupBy:   groupBy,
		Limit:     openIntParam(c, "limit", 200, 1, 1000),
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	totals := map[string]interface{}{"requests": int64(0), "prompt_tokens": int64(0),
		"completion_tokens": int64(0), "total_tokens": int64(0), "quota": int64(0)}
	type item struct {
		Username         string  `json:"username"`
		ModelName        string  `json:"model_name"`
		Group            string  `json:"group"`
		Requests         int64   `json:"requests"`
		PromptTokens     int64   `json:"prompt_tokens"`
		CompletionTokens int64   `json:"completion_tokens"`
		TotalTokens      int64   `json:"total_tokens"`
		Quota            int64   `json:"quota"`
		CostCNY          float64 `json:"cost_cny"`
		FirstAt          string  `json:"first_at"`
		LastAt           string  `json:"last_at"`
	}
	items := make([]item, 0, len(rows))
	for _, r := range rows {
		items = append(items, item{
			Username: r.Username, ModelName: r.ModelName, Group: r.Group,
			Requests: r.Requests, PromptTokens: r.PromptTokens, CompletionTokens: r.CompletionTokens,
			TotalTokens: r.TotalTokens, Quota: r.Quota, CostCNY: openCostCNY(r.Quota),
			FirstAt: openFmtTime(r.FirstAt), LastAt: openFmtTime(r.LastAt),
		})
		totals["requests"] = totals["requests"].(int64) + r.Requests
		totals["prompt_tokens"] = totals["prompt_tokens"].(int64) + r.PromptTokens
		totals["completion_tokens"] = totals["completion_tokens"].(int64) + r.CompletionTokens
		totals["total_tokens"] = totals["total_tokens"].(int64) + r.TotalTokens
		totals["quota"] = totals["quota"].(int64) + r.Quota
	}
	totals["cost_cny"] = openCostCNY(totals["quota"].(int64))

	rangeInfo := gin.H{
		"start": start.Format("2006-01-02 15:04:05"),
		"end":   end.Format("2006-01-02 15:04:05"),
		"days":  int(end.Sub(start).Hours()/24) + 1,
	}

	if c.Query("format") == "csv" {
		c.Writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
		c.Writer.Header().Set("Content-Disposition",
			fmt.Sprintf("attachment; filename=usage_%s.csv", time.Now().Format("20060102_150405")))
		c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
		w := csv.NewWriter(c.Writer)
		_ = w.Write([]string{"用户名", "模型", "分组", "调用次数", "输入Token", "输出Token", "总Token", "额度", "费用(元)", "首次调用", "最后调用"})
		for _, it := range items {
			_ = w.Write([]string{it.Username, it.ModelName, it.Group,
				strconv.FormatInt(it.Requests, 10), strconv.FormatInt(it.PromptTokens, 10),
				strconv.FormatInt(it.CompletionTokens, 10), strconv.FormatInt(it.TotalTokens, 10),
				strconv.FormatInt(it.Quota, 10), strconv.FormatFloat(it.CostCNY, 'f', 4, 64),
				it.FirstAt, it.LastAt})
		}
		w.Flush()
		return
	}

	if c.Query("format") == "text" {
		var b strings.Builder
		fmt.Fprintf(&b, "【模型调用情况】%s ~ %s\n", rangeInfo["start"], rangeInfo["end"])
		fmt.Fprintf(&b, "合计：%d 次调用，%d tokens，费用 ¥%.4f\n",
			totals["requests"], totals["total_tokens"], totals["cost_cny"])
		for _, it := range items {
			fmt.Fprintf(&b, "- %s | %s | %d 次 | %d tokens | ¥%.4f | %s ~ %s\n",
				it.Username, it.ModelName, it.Requests, it.TotalTokens, it.CostCNY, it.FirstAt, it.LastAt)
		}
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(b.String()))
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"range": rangeInfo, "group_by": groupBy, "totals": totals, "items": items,
	}})
}

// ---- 调用内容：/api/open/contents ----

type openContentItem struct {
	Time             string `json:"time"`
	Username         string `json:"username"`
	ModelName        string `json:"model"`
	Group            string `json:"group"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	Request          string `json:"request"`
	Response         string `json:"response"`
	Truncated        bool   `json:"truncated"`
}

// loadOpenContents 取对话内容（受密钥 include_content 与范围限制）。返回 items/是否有内容权限/错误文本。
func loadOpenContents(c *gin.Context, scope *model.OpenKeyScope, start, end time.Time, names []string, limit int, maxChars int) ([]openContentItem, string) {
	if !scope.IncludeContent {
		return nil, "该密钥未开放对话内容（include_content=false），只能获取用量类数据"
	}
	filter := model.ChatLogFilter{
		StartTime: start.Unix(),
		EndTime:   end.Unix(),
		ModelName: strings.TrimSpace(c.Query("model")),
		Group:     strings.TrimSpace(c.Query("group")),
	}
	if names != nil {
		filter.Usernames = names
	}
	logs, _, err := model.GetChatLogs(filter, 1, limit)
	if err != nil {
		return nil, err.Error()
	}
	items := make([]openContentItem, 0, len(logs))
	for _, l := range logs {
		req, t1 := openTruncate(l.RequestContent, maxChars)
		resp, t2 := openTruncate(l.ResponseContent, maxChars)
		items = append(items, openContentItem{
			Time: openFmtTime(l.CreatedAt), Username: l.Username, ModelName: l.ModelName, Group: l.Group,
			PromptTokens: l.PromptTokens, CompletionTokens: l.CompletionTokens,
			Request: req, Response: resp, Truncated: t1 || t2,
		})
	}
	return items, ""
}

func OpenQueryContents(c *gin.Context) {
	scope := c.MustGet("open_scope").(*model.OpenKeyScope)

	start, end, msg := openParseRange(c, scope, 7)
	if msg != "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": msg})
		return
	}
	allowed, err := model.ScopeAllowedUsernames(scope)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "权限解析失败: " + err.Error()})
		return
	}
	names, msg := openRequestedUsernames(c, allowed)
	if msg != "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": msg})
		return
	}
	limit := openIntParam(c, "limit", 50, 1, 200)
	maxChars := openIntParam(c, "max_chars", 0, 0, 100000)

	items, errMsg := loadOpenContents(c, scope, start, end, names, limit, maxChars)
	if errMsg != "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": errMsg})
		return
	}

	if c.Query("format") == "text" {
		var b strings.Builder
		fmt.Fprintf(&b, "【调用内容】%s ~ %s，共 %d 条\n\n",
			start.Format("2006-01-02 15:04:05"), end.Format("2006-01-02 15:04:05"), len(items))
		for i, it := range items {
			fmt.Fprintf(&b, "── 记录 %d ──\n时间：%s  用户：%s  模型：%s  tokens：%d/%d\n【提问】\n%s\n【回复】\n%s\n\n",
				i+1, it.Time, it.Username, it.ModelName, it.PromptTokens, it.CompletionTokens, it.Request, it.Response)
		}
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(b.String()))
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"range": gin.H{"start": start.Format("2006-01-02 15:04:05"), "end": end.Format("2006-01-02 15:04:05")},
		"count": len(items), "items": items,
	}})
}

// ---- 一站式报告：/api/open/report ----

type openReportUser struct {
	Username         string            `json:"username"`
	Requests         int64             `json:"requests"`
	TotalTokens      int64             `json:"total_tokens"`
	PromptTokens     int64             `json:"prompt_tokens"`
	CompletionTokens int64             `json:"completion_tokens"`
	Quota            int64             `json:"quota"`
	CostCNY          float64           `json:"cost_cny"`
	FirstAt          string            `json:"first_at"`
	LastAt           string            `json:"last_at"`
	Models           []map[string]any  `json:"models"`
	Samples          []openContentItem `json:"samples,omitempty"`
}

func OpenQueryReport(c *gin.Context) {
	scope := c.MustGet("open_scope").(*model.OpenKeyScope)

	start, end, msg := openParseRange(c, scope, 7)
	if msg != "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": msg})
		return
	}
	allowed, err := model.ScopeAllowedUsernames(scope)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "权限解析失败: " + err.Error()})
		return
	}
	names, msg := openRequestedUsernames(c, allowed)
	if msg != "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": msg})
		return
	}
	topUsers := openIntParam(c, "top_users", 20, 1, 50)
	samples := openIntParam(c, "samples", 3, 0, 20)
	maxChars := openIntParam(c, "max_chars", 1200, 0, 100000)

	rows, err := model.GetOpenUsageStats(model.OpenUsageFilter{
		StartTime: start.Unix(), EndTime: end.Unix(), Usernames: names,
		Model: strings.TrimSpace(c.Query("model")), Group: strings.TrimSpace(c.Query("group")),
		GroupBy: "user_model", Limit: 1000,
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	order := []string{}
	byUser := map[string]*openReportUser{}
	for _, r := range rows {
		u, ok := byUser[r.Username]
		if !ok {
			u = &openReportUser{Username: r.Username, FirstAt: openFmtTime(r.FirstAt), LastAt: openFmtTime(r.LastAt)}
			byUser[r.Username] = u
			order = append(order, r.Username)
		}
		u.Requests += r.Requests
		u.Quota += r.Quota
		u.TotalTokens += r.TotalTokens
		u.PromptTokens += r.PromptTokens
		u.CompletionTokens += r.CompletionTokens
		u.CostCNY += openCostCNY(r.Quota)
		if r.FirstAt > 0 && (u.FirstAt == "" || openFmtTime(r.FirstAt) < u.FirstAt) {
			u.FirstAt = openFmtTime(r.FirstAt)
		}
		if openFmtTime(r.LastAt) > u.LastAt {
			u.LastAt = openFmtTime(r.LastAt)
		}
		u.Models = append(u.Models, map[string]any{
			"model": r.ModelName, "requests": r.Requests, "total_tokens": r.TotalTokens,
			"quota": r.Quota, "cost_cny": openCostCNY(r.Quota),
		})
	}

	users := make([]*openReportUser, 0, len(order))
	for _, name := range order {
		users = append(users, byUser[name])
	}
	sort.SliceStable(users, func(i, j int) bool { return users[i].CostCNY > users[j].CostCNY })
	if len(users) > topUsers {
		users = users[:topUsers]
	}

	// 内容样本：仅在有内容权限且 samples>0 时取（每个用户取最近 N 条）
	contentNote := ""
	if samples > 0 {
		if !scope.IncludeContent {
			contentNote = "该密钥未开放对话内容（include_content=false），未附带内容样本"
		} else {
			for _, u := range users {
				c2 := c.Copy()
				c2.Request.URL.RawQuery = fmt.Sprintf("limit=%d", samples)
				items, errMsg := loadOpenContents(c2, scope, start, end, []string{u.Username}, samples, maxChars)
				if errMsg != "" {
					contentNote = errMsg
					break
				}
				u.Samples = items
			}
		}
	}

	var totalQuota, totalTokens, totalReq int64
	for _, u := range users {
		totalReq += u.Requests
		totalTokens += u.TotalTokens
		totalQuota += u.Quota
	}
	totals := gin.H{
		"requests": totalReq, "total_tokens": totalTokens,
		"quota": totalQuota, "cost_cny": openCostCNY(totalQuota),
	}

	rangeInfo := gin.H{
		"start": start.Format("2006-01-02 15:04:05"),
		"end":   end.Format("2006-01-02 15:04:05"),
		"days":  int(end.Sub(start).Hours()/24) + 1,
	}

	if c.Query("format") == "text" {
		var b strings.Builder
		fmt.Fprintf(&b, "【用户模型使用报告】%s ~ %s\n", rangeInfo["start"], rangeInfo["end"])
		fmt.Fprintf(&b, "合计：%d 次调用，%d tokens，费用 ¥%.4f（按费用降序，最多 %d 人）\n\n",
			totalReq, totalTokens, totals["cost_cny"], topUsers)
		for i, u := range users {
			fmt.Fprintf(&b, "=== %d. %s ===\n调用 %d 次，%d tokens（输入 %d / 输出 %d），费用 ¥%.4f，活跃时段 %s ~ %s\n模型明细：\n",
				i+1, u.Username, u.Requests, u.TotalTokens, u.PromptTokens, u.CompletionTokens, u.CostCNY, u.FirstAt, u.LastAt)
			for _, m := range u.Models {
				fmt.Fprintf(&b, "  - %v：%v 次，%v tokens，¥%.4f\n", m["model"], m["requests"], m["total_tokens"], m["cost_cny"])
			}
			for j, s := range u.Samples {
				fmt.Fprintf(&b, "  [样本 %d] %s %s\n  【提问】%s\n  【回复】%s\n", j+1, s.Time, s.ModelName, s.Request, s.Response)
			}
			b.WriteString("\n")
		}
		if contentNote != "" {
			fmt.Fprintf(&b, "（%s）\n", contentNote)
		}
		b.WriteString("---\n审查提示词（可直接连同以上内容发给 AI）：\n")
		b.WriteString("请按用户逐条评估：①是否存在与工作无关的用途；②是否有明显浪费（超长上下文、重复提问、模型选择不当）；③是否触碰敏感信息（客户数据、代码密钥、个人信息）；④给出每个用户「合规/需关注/不建议继续授权」的结论与理由，并列出证据（引用上面的样本）。\n")
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(b.String()))
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"range": rangeInfo, "totals": totals, "users": users,
		"content_available": scope.IncludeContent, "note": contentNote,
	}})
}
