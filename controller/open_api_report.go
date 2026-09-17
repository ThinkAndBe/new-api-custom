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

// isDateOnly 判断参数是否只写了日期（YYYY-MM-DD），没有时分秒
func isDateOnly(raw string) bool {
	if len(raw) != 10 {
		return false
	}
	if _, err := time.ParseInLocation("2006-01-02", raw, time.Local); err != nil {
		return false
	}
	return true
}

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
	startRaw := strings.TrimSpace(c.Query("start"))
	endRaw := strings.TrimSpace(c.Query("end"))
	start, err := parse(startRaw, now.AddDate(0, 0, -defDays))
	if err != nil {
		return start, start, "start " + err.Error()
	}
	end, err := parse(endRaw, now)
	if err != nil {
		return start, start, "end " + err.Error()
	}
	// 只写日期（YYYY-MM-DD）时，语义是"那一天"：end 补到当天 23:59:59。
	// 否则 start=end=2026-09-16 会变成零点到零点的空区间——顾问最容易踩的坑。
	if isDateOnly(endRaw) {
		end = end.Add(24*time.Hour - time.Second)
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

// openScopeFilter 解析并校验数据范围：密钥范围（权威）+ 请求侧收窄条件。
//
// 安全契约（务必保持）：
//   - Restricted=true 且 Users 为空 => DenyAll，任何查询都必须返回空，不得退化成"不过滤"；
//   - Users 非 nil 时，SQL 必须按 Users 过滤（即密钥能看的人是白名单）；
//   - Groups 只允许在密钥已按分组授权时收窄，且必须是密钥分组的子集，绝不允许扩大。
type openScopeFilter struct {
	Users      []string // nil = 不限制用户
	Groups     []string // 请求侧分组收窄（已校验 ⊆ 密钥分组）
	Restricted bool
	DenyAll    bool
	ErrMsg     string
}

func resolveOpenScope(c *gin.Context, keyScope *model.OpenKeyScope) openScopeFilter {
	out := openScopeFilter{}
	allowed, err := model.ScopeAllowedUsernames(keyScope)
	if err != nil {
		out.ErrMsg = "权限解析失败: " + err.Error()
		return out
	}
	out.Restricted = allowed != nil

	raw := strings.TrimSpace(c.Query("users"))
	single := strings.TrimSpace(c.Query("username"))
	var requested []string
	if raw != "" {
		for _, n := range strings.Split(raw, ",") {
			if n = strings.TrimSpace(n); n != "" {
				requested = append(requested, n)
			}
		}
	} else if single != "" {
		requested = []string{single}
	}
	if len(requested) > 0 {
		if out.Restricted {
			inScope := make(map[string]bool, len(allowed))
			for _, u := range allowed {
				inScope[u] = true
			}
			for _, u := range requested {
				if !inScope[u] {
					out.ErrMsg = fmt.Sprintf("用户「%s」不在该密钥的数据范围内", u)
					return out
				}
			}
		}
		out.Users = requested
	} else {
		out.Users = allowed // nil=不限；受限时可能是空集合（此时 DenyAll）
	}

	if gRaw := strings.TrimSpace(c.Query("groups")); gRaw != "" {
		var reqGroups []string
		for _, g := range strings.Split(gRaw, ",") {
			if g = strings.TrimSpace(g); g != "" {
				reqGroups = append(reqGroups, g)
			}
		}
		if len(reqGroups) > 0 {
			if len(keyScope.Groups) == 0 {
				out.ErrMsg = "该密钥未按分组授权，不能使用 groups 参数"
				return out
			}
			valid := make(map[string]bool, len(keyScope.Groups))
			for _, g := range keyScope.Groups {
				valid[g] = true
			}
			for _, g := range reqGroups {
				if !valid[g] {
					out.ErrMsg = fmt.Sprintf("分组「%s」不在该密钥的数据范围内", g)
					return out
				}
			}
			out.Groups = reqGroups
		}
	}

	out.DenyAll = out.Restricted && len(out.Users) == 0
	return out
}

// openScopeEmpty 统一的"范围内无数据"响应（受限密钥解析为空时使用）
func openScopeEmpty(c *gin.Context, what string) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"count": 0, "items": []any{},
		"message": "该密钥的数据范围内当前没有" + what,
	}})
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

// openBoolParam 解析 0/1、true/false 形式的布尔查询参数
func openBoolParam(c *gin.Context, name string, def bool) bool {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return def
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
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
	sf := resolveOpenScope(c, scope)
	if sf.ErrMsg != "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": sf.ErrMsg})
		return
	}
	if sf.DenyAll {
		openScopeEmpty(c, "用户")
		return
	}

	groupBy := strings.TrimSpace(c.Query("group_by"))
	switch groupBy {
	case "model", "user_model", "group", "all":
	default:
		groupBy = "user"
	}
	granularity := strings.TrimSpace(c.Query("granularity"))
	if granularity != "day" && granularity != "hour" {
		granularity = ""
	}
	rows, err := model.GetOpenUsageStats(model.OpenUsageFilter{
		StartTime:   start.Unix(),
		EndTime:     end.Unix(),
		Usernames:   sf.Users,
		Restricted:  sf.Restricted,
		Groups:      sf.Groups,
		Model:       strings.TrimSpace(c.Query("model")),
		Group:       strings.TrimSpace(c.Query("group")),
		GroupBy:     groupBy,
		Granularity: granularity,
		Limit:       openIntParam(c, "limit", 200, 1, 5000),
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
		Bucket           string  `json:"bucket,omitempty"`       // 粒度分桶标签（东八区）
		BucketStart      int64   `json:"bucket_start,omitempty"` // 分桶起点（unix 秒）
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
		bucketLabel := ""
		if r.BucketStart > 0 {
			// bucket_start 是「东八区分桶序号」，换算回真实时间戳再格式化
			unit := int64(86400)
			if granularity == "hour" {
				unit = 3600
			}
			bucketLabel = time.Unix(r.BucketStart*unit-28800, 0).In(time.FixedZone("CST", 8*3600)).
				Format("2006-01-02")
			if granularity == "hour" {
				bucketLabel = time.Unix(r.BucketStart*unit-28800, 0).In(time.FixedZone("CST", 8*3600)).
					Format("2006-01-02 15:00")
			}
		}
		items = append(items, item{
			Username: r.Username, ModelName: r.ModelName, Group: r.Group,
			Bucket: bucketLabel, BucketStart: r.BucketStart,
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
			row := []string{it.Username, it.ModelName, it.Group, it.Bucket,
				strconv.FormatInt(it.Requests, 10), strconv.FormatInt(it.PromptTokens, 10),
				strconv.FormatInt(it.CompletionTokens, 10), strconv.FormatInt(it.TotalTokens, 10),
				strconv.FormatInt(it.Quota, 10), strconv.FormatFloat(it.CostCNY, 'f', 4, 64),
				it.FirstAt, it.LastAt}
			if granularity == "" {
				row = append(row[:3], row[4:]...) // 无粒度时不输出空列
			}
			_ = w.Write(row)
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
			prefix := ""
			if it.Bucket != "" {
				prefix = it.Bucket + " | "
			}
			fmt.Fprintf(&b, "- %s%s | %s | %d 次 | %d tokens | ¥%.4f\n",
				prefix, it.Username, it.ModelName, it.Requests, it.TotalTokens, it.CostCNY)
		}
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(b.String()))
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"range": rangeInfo, "group_by": groupBy, "granularity": granularity,
		"totals": totals, "items": items,
	}})
}

// ---- 用户额度概览：/api/open/users ----

// OpenQueryUsers 每个用户的额度与用量概览（总额度/已用/余额 + 调用次数 + 最近登录）。
// 额度类字段是「账户累计值」（不随时间窗变化）；区间用量请看 /api/open/usage。
func OpenQueryUsers(c *gin.Context) {
	scope := c.MustGet("open_scope").(*model.OpenKeyScope)

	sf := resolveOpenScope(c, scope)
	if sf.ErrMsg != "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": sf.ErrMsg})
		return
	}
	if sf.DenyAll {
		openScopeEmpty(c, "用户")
		return
	}
	// 密钥按分组授权且未显式指定用户时，按分组下推（比逐一列举用户名更准确）
	groups := sf.Groups
	if sf.Users == nil && len(scope.Groups) > 0 {
		groups = scope.Groups
	}

	users, err := model.GetOpenUsers(groups, sf.Users, sf.Restricted)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	type item struct {
		UserId         int     `json:"user_id"`
		Username       string  `json:"username"`
		DisplayName    string  `json:"display_name"`
		EmployeeId     string  `json:"employee_id"`
		Group          string  `json:"group"`
		Status         string  `json:"status"`
		Quota          int     `json:"quota"`
		UsedQuota      int     `json:"used_quota"`
		RemainingQuota int     `json:"remaining_quota"`
		QuotaCNY       float64 `json:"quota_cny"`
		UsedCNY        float64 `json:"used_cny"`
		RemainingCNY   float64 `json:"remaining_cny"`
		RequestCount   int     `json:"request_count"`
		CreatedAt      string  `json:"created_at"`
		LastLoginAt    string  `json:"last_login_at"`
	}
	statusText := func(st int) string {
		if st == common.UserStatusEnabled {
			return "启用"
		}
		return "禁用"
	}
	items := make([]item, 0, len(users))
	var totalQuota, totalUsed int64
	for _, u := range users {
		remaining := u.Quota - u.UsedQuota
		if remaining < 0 {
			remaining = 0
		}
		items = append(items, item{
			UserId: u.Id, Username: u.Username, DisplayName: u.DisplayName, EmployeeId: u.EmployeeId,
			Group: u.Group, Status: statusText(u.Status),
			Quota: u.Quota, UsedQuota: u.UsedQuota, RemainingQuota: remaining,
			QuotaCNY: openCostCNY(int64(u.Quota)), UsedCNY: openCostCNY(int64(u.UsedQuota)),
			RemainingCNY: openCostCNY(int64(remaining)),
			RequestCount: u.RequestCount,
			CreatedAt:    openFmtTime(u.CreatedAt), LastLoginAt: openFmtTime(u.LastLoginAt),
		})
		totalQuota += int64(u.Quota)
		totalUsed += int64(u.UsedQuota)
	}

	if c.Query("format") == "csv" {
		c.Writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
		c.Writer.Header().Set("Content-Disposition",
			fmt.Sprintf("attachment; filename=users_%s.csv", time.Now().Format("20060102_150405")))
		c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
		w := csv.NewWriter(c.Writer)
		_ = w.Write([]string{"用户ID", "用户名", "显示名", "工号", "分组", "状态", "总额度(元)", "已用额度(元)", "剩余额度(元)", "调用次数", "注册时间", "最后登录"})
		for _, it := range items {
			_ = w.Write([]string{
				strconv.Itoa(it.UserId), it.Username, it.DisplayName, it.EmployeeId, it.Group, it.Status,
				strconv.FormatFloat(it.QuotaCNY, 'f', 4, 64), strconv.FormatFloat(it.UsedCNY, 'f', 4, 64),
				strconv.FormatFloat(it.RemainingCNY, 'f', 4, 64), strconv.Itoa(it.RequestCount),
				it.CreatedAt, it.LastLoginAt,
			})
		}
		w.Flush()
		return
	}

	if c.Query("format") == "text" {
		var b strings.Builder
		fmt.Fprintf(&b, "【用户额度概览】共 %d 人（累计值，不随时间窗变化）\n", len(items))
		fmt.Fprintf(&b, "合计：总额度 ¥%.2f，已用 ¥%.2f\n", openCostCNY(totalQuota), openCostCNY(totalUsed))
		for _, it := range items {
			fmt.Fprintf(&b, "- %s（%s）| %s | %s | 额度 ¥%.2f | 已用 ¥%.2f | 剩余 ¥%.2f | 累计调用 %d 次 | 最后登录 %s\n",
				it.Username, it.DisplayName, it.Group, it.Status,
				it.QuotaCNY, it.UsedCNY, it.RemainingCNY, it.RequestCount, it.LastLoginAt)
		}
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(b.String()))
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"count": len(items),
		"totals": gin.H{
			"quota": totalQuota, "used_quota": totalUsed,
			"quota_cny": openCostCNY(totalQuota), "used_cny": openCostCNY(totalUsed),
		},
		"items": items,
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
// loadOpenContents 取对话内容。默认只给「用户输入」，模型回复需显式 with_response=1。
func loadOpenContents(c *gin.Context, scope *model.OpenKeyScope, sf openScopeFilter, start, end time.Time, limit, maxChars int, withResponse bool) ([]openContentItem, string) {
	if !scope.IncludeContent {
		return nil, "该密钥未开放对话内容（include_content=false），只能获取用量类数据"
	}
	filter := model.ChatLogFilter{
		StartTime:           start.Unix(),
		EndTime:             end.Unix(),
		ModelName:           strings.TrimSpace(c.Query("model")),
		Group:               strings.TrimSpace(c.Query("group")),
		Groups:              sf.Groups,
		Usernames:           sf.Users,
		UsernamesRestricted: sf.Restricted,
	}
	logs, _, err := model.GetChatLogs(filter, 1, limit)
	if err != nil {
		return nil, err.Error()
	}
	items := make([]openContentItem, 0, len(logs))
	for _, l := range logs {
		if l.Username != "" && sf.Restricted && len(sf.Users) > 0 && !openInSet(sf.Users, l.Username) {
			continue
		}
		req, t1 := openTruncate(l.RequestContent, maxChars)
		item := openContentItem{
			Time: openFmtTime(l.CreatedAt), Username: l.Username, ModelName: l.ModelName, Group: l.Group,
			PromptTokens: l.PromptTokens, CompletionTokens: l.CompletionTokens,
			Request: req, Truncated: t1,
		}
		if withResponse {
			resp, t2 := openTruncate(l.ResponseContent, maxChars)
			item.Response = resp
			item.Truncated = t1 || t2
		}
		items = append(items, item)
	}
	return items, ""
}

// openInSet 判断用户名是否在白名单内（二次校验，防上游过滤遗漏）
func openInSet(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func OpenQueryContents(c *gin.Context) {
	scope := c.MustGet("open_scope").(*model.OpenKeyScope)

	start, end, msg := openParseRange(c, scope, 7)
	if msg != "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": msg})
		return
	}
	sf := resolveOpenScope(c, scope)
	if sf.ErrMsg != "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": sf.ErrMsg})
		return
	}
	if sf.DenyAll {
		openScopeEmpty(c, "对话内容")
		return
	}
	limit := openIntParam(c, "limit", 50, 1, 200)
	maxChars := openIntParam(c, "max_chars", 0, 0, 100000)
	withResponse := openBoolParam(c, "with_response", false)

	items, errMsg := loadOpenContents(c, scope, sf, start, end, limit, maxChars, withResponse)
	if errMsg != "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": errMsg})
		return
	}

	if c.Query("format") == "text" {
		var b strings.Builder
		fmt.Fprintf(&b, "【调用内容】%s ~ %s，共 %d 条\n\n",
			start.Format("2006-01-02 15:04:05"), end.Format("2006-01-02 15:04:05"), len(items))
		for i, it := range items {
			fmt.Fprintf(&b, "── 记录 %d ──\n时间：%s  用户：%s  模型：%s  tokens：%d/%d\n【提问】\n%s\n",
				i+1, it.Time, it.Username, it.ModelName, it.PromptTokens, it.CompletionTokens, it.Request)
			if it.Response != "" {
				fmt.Fprintf(&b, "【回复】\n%s\n", it.Response)
			}
			b.WriteString("\n")
		}
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(b.String()))
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"range": gin.H{"start": start.Format("2006-01-02 15:04:05"), "end": end.Format("2006-01-02 15:04:05")},
		"count": len(items), "items": items, "with_response": withResponse,
	}})
}

// ---- 批量导出对话内容：/api/open/contents/export ----
//
// 面向「所有相关分组的所有用户对话内容」场景：按 id 升序游标翻页，逐条写出，内存占用恒定。
// 客户端循环：after_id 从 0 开始，每次取响应头 X-Next-After-Id，直到 X-Has-More=0。
// 默认只返回用户输入（with_response=1 才带模型回复）。
func OpenExportContents(c *gin.Context) {
	scope := c.MustGet("open_scope").(*model.OpenKeyScope)

	start, end, msg := openParseRange(c, scope, 7)
	if msg != "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": msg})
		return
	}
	if !scope.IncludeContent {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "该密钥未开放对话内容（include_content=false），只能获取用量类数据"})
		return
	}
	sf := resolveOpenScope(c, scope)
	if sf.ErrMsg != "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": sf.ErrMsg})
		return
	}
	afterId, _ := strconv.Atoi(c.Query("after_id"))
	limit := openIntParam(c, "limit", 2000, 1, 5000)
	maxChars := openIntParam(c, "max_chars", 0, 0, 100000)
	withResponse := openBoolParam(c, "with_response", false)

	filter := model.ChatLogFilter{
		StartTime:           start.Unix(),
		EndTime:             end.Unix(),
		ModelName:           strings.TrimSpace(c.Query("model")),
		Group:               strings.TrimSpace(c.Query("group")),
		Groups:              sf.Groups,
		Usernames:           sf.Users,
		UsernamesRestricted: sf.Restricted,
	}

	format := strings.TrimSpace(c.Query("format"))
	if format == "" {
		format = "jsonl"
	}

	// 先探一次拿游标与是否还有更多（数据量小，直接全部取出后写出，保证响应头准确）
	logs, hasMore, err := openFetchContentPage(filter, afterId, limit)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	nextAfter := afterId
	if len(logs) > 0 {
		nextAfter = logs[len(logs)-1].Id
	}
	c.Writer.Header().Set("X-Count", strconv.Itoa(len(logs)))
	c.Writer.Header().Set("X-Next-After-Id", strconv.Itoa(nextAfter))
	if hasMore {
		c.Writer.Header().Set("X-Has-More", "1")
	} else {
		c.Writer.Header().Set("X-Has-More", "0")
	}
	c.Writer.Header().Set("Access-Control-Expose-Headers", "X-Count, X-Next-After-Id, X-Has-More")

	switch format {
	case "csv":
		c.Writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
		c.Writer.Header().Set("Content-Disposition",
			fmt.Sprintf("attachment; filename=contents_%s.csv", time.Now().Format("20060102_150405")))
		c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
		w := csv.NewWriter(c.Writer)
		header := []string{"日志ID", "时间", "用户ID", "用户名", "令牌", "渠道ID", "模型", "分组", "请求ID", "流式", "请求内容"}
		if withResponse {
			header = append(header, "回复内容")
		}
		_ = w.Write(header)
		for _, l := range logs {
			req, _ := openTruncate(l.RequestContent, maxChars)
			row := []string{strconv.Itoa(l.Id), openFmtTime(l.CreatedAt), strconv.Itoa(l.UserId), l.Username,
				l.TokenName, strconv.Itoa(l.ChannelId), l.ModelName, l.Group, l.RequestId,
				strconv.FormatBool(l.IsStream), req}
			if withResponse {
				resp, _ := openTruncate(l.ResponseContent, maxChars)
				row = append(row, resp)
			}
			_ = w.Write(row)
		}
		w.Flush()
	case "text":
		c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		var b strings.Builder
		fmt.Fprintf(&b, "【对话内容】%s ~ %s，本页 %d 条，本页游标 %d~%d，还有更多：%v\n",
			start.Format("2006-01-02 15:04:05"), end.Format("2006-01-02 15:04:05"),
			len(logs), afterId, nextAfter, hasMore)
		for _, l := range logs {
			req, _ := openTruncate(l.RequestContent, maxChars)
			fmt.Fprintf(&b, "── #%d %s | %s | %s\n%s\n", l.Id, openFmtTime(l.CreatedAt), l.Username, l.ModelName, req)
			if withResponse {
				resp, _ := openTruncate(l.ResponseContent, maxChars)
				fmt.Fprintf(&b, "【回复】\n%s\n", resp)
			}
		}
		_, _ = c.Writer.WriteString(b.String())
	default: // jsonl：一行一条，便于直接喂给 AI
		c.Writer.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		for _, l := range logs {
			req, truncated := openTruncate(l.RequestContent, maxChars)
			item := gin.H{
				"id": l.Id, "time": openFmtTime(l.CreatedAt), "user_id": l.UserId, "username": l.Username,
				"token_name": l.TokenName, "channel_id": l.ChannelId, "model": l.ModelName, "group": l.Group,
				"request_id": l.RequestId, "is_stream": l.IsStream,
				"prompt_tokens": l.PromptTokens, "completion_tokens": l.CompletionTokens,
				"request": req, "truncated": truncated,
			}
			if withResponse {
				resp, t2 := openTruncate(l.ResponseContent, maxChars)
				item["response"] = resp
				item["truncated"] = truncated || t2
			}
			line, err := common.Marshal(item)
			if err != nil {
				continue
			}
			c.Writer.Write(line)
			c.Writer.Write([]byte("\n"))
		}
	}
}

// openFetchContentPage 按游标取一页对话内容（升序，id > afterId）
func openFetchContentPage(filter model.ChatLogFilter, afterId, limit int) ([]*model.ChatLog, bool, error) {
	filter.AfterId = afterId
	var logs []*model.ChatLog
	tx := model.ApplyChatLogFilterForExport(model.LOG_DB.Model(&model.ChatLog{}), filter).
		Order("id ASC").Limit(limit + 1)
	if err := tx.Find(&logs).Error; err != nil {
		return nil, false, err
	}
	hasMore := len(logs) > limit
	if hasMore {
		logs = logs[:limit]
	}
	return logs, hasMore, nil
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
	LogCount         int64             `json:"log_count"`
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
	sf := resolveOpenScope(c, scope)
	if sf.ErrMsg != "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": sf.ErrMsg})
		return
	}
	if sf.DenyAll {
		openScopeEmpty(c, "用户")
		return
	}
	withResponse := openBoolParam(c, "with_response", false)
	topUsers := openIntParam(c, "top_users", 20, 1, 50)
	samples := openIntParam(c, "samples", 3, 0, 200)
	maxChars := openIntParam(c, "max_chars", 1200, 0, 100000)

	rows, err := model.GetOpenUsageStats(model.OpenUsageFilter{
		StartTime: start.Unix(), EndTime: end.Unix(), Usernames: sf.Users,
		Restricted: sf.Restricted, Groups: sf.Groups,
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

	// 对话日志总数（按用户聚合，一次查询）：让调用方知道「共多少条、本报告附了多少条」
	if counts, err := model.GetChatLogUserCounts(start.Unix(), end.Unix(), sf.Users, sf.Restricted); err == nil {
		for _, u := range users {
			u.LogCount = counts[u.Username]
		}
	}

	// 内容：按 samples 取每个用户的对话日志（最近 N 条）。总量设上限，避免一次拉爆响应体。
	contentNote := ""
	if samples > 0 {
		if !scope.IncludeContent {
			contentNote = "该密钥未开放对话内容（include_content=false），未附带对话日志内容"
		} else {
			const maxTotalLogs = 500
			fetched := 0
			for _, u := range users {
				if fetched >= maxTotalLogs {
					contentNote = fmt.Sprintf("对话日志条数超过单次上限 %d 条，已截断；完整日志请用 /api/open/contents 按用户分页获取", maxTotalLogs)
					break
				}
				want := samples
				if remaining := maxTotalLogs - fetched; want > remaining {
					want = remaining
				}
				c2 := c.Copy()
				c2.Request.URL.RawQuery = fmt.Sprintf("limit=%d", want)
				items, errMsg := loadOpenContents(c2, scope, openScopeFilter{
					Users: []string{u.Username}, Restricted: true, Groups: sf.Groups,
				}, start, end, want, maxChars, withResponse)
				if errMsg != "" {
					contentNote = errMsg
					break
				}
				u.Samples = items
				fetched += len(items)
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
			if u.LogCount > 0 {
				fmt.Fprintf(&b, "对话日志：区间内共 %d 条，以下为最近 %d 条\n", u.LogCount, len(u.Samples))
			}
			for j, s := range u.Samples {
				fmt.Fprintf(&b, "  ── 日志 %d ── %s | %s | tokens %d/%d\n  【提问】%s\n  【回复】%s\n",
					j+1, s.Time, s.ModelName, s.PromptTokens, s.CompletionTokens, s.Request, s.Response)
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
