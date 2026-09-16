package controller

// open_api.go — 对话日志数据开放：密钥管理（Root）+ 开放查询（密钥鉴权）。
//
// 顾问等外部消费方用开放密钥拉取对话日志做用户使用总结：
//   GET /api/open/chat_logs?start=&end=&username=&model_name=&stats=1&page=&page_size=
//   Header: Authorization: Bearer sk-open-xxxx
// 数据权限由密钥 Scope 决定（分组/用户/是否含内容/回看天数上限）。

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// ---- 开放密钥管理（RootAuth） ----

type openKeyReq struct {
	Id            int      `json:"id"`
	Name          string   `json:"name"`
	Groups        []string `json:"groups"`
	Usernames     []string `json:"usernames"`
	IncludeContent bool    `json:"include_content"`
	MaxDays       int      `json:"max_days"`
	Enabled       bool     `json:"enabled"`
	ExpireDays    int      `json:"expire_days"` // 0=永久
}

func buildScopeJSON(req *openKeyReq) (string, error) {
	scope := model.OpenKeyScope{
		Groups:         req.Groups,
		Usernames:      req.Usernames,
		IncludeContent: req.IncludeContent,
		MaxDays:        req.MaxDays,
	}
	scope.Normalize()
	data, err := common.Marshal(scope)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// GetOpenKeys GET /api/open_key/
func GetOpenKeys(c *gin.Context) {
	keys, err := model.ListOpenAPIKeys()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, keys)
}

// CreateOpenKey POST /api/open_key/
func CreateOpenKey(c *gin.Context) {
	var req openKeyReq
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		common.ApiErrorMsg(c, "名称必填")
		return
	}
	scope, err := buildScopeJSON(&req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	expiresAt := int64(0)
	if req.ExpireDays > 0 {
		expiresAt = time.Now().Unix() + int64(req.ExpireDays)*86400
	}
	k := &model.OpenAPIKey{
		Name: strings.TrimSpace(req.Name),
		Key:  "sk-open-" + common.GetUUID(),
		Scope: scope,
		Enabled: true,
		ExpiresAt: expiresAt,
	}
	if err := model.CreateOpenAPIKey(k); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeSystem, "创建数据开放密钥 "+k.Name)
	common.ApiSuccess(c, k)
}

// UpdateOpenKey PUT /api/open_key/
func UpdateOpenKey(c *gin.Context) {
	var req openKeyReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Id == 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	scope, err := buildScopeJSON(&req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	k := &model.OpenAPIKey{
		Id: req.Id, Name: strings.TrimSpace(req.Name), Scope: scope,
		Enabled: req.Enabled,
	}
	if req.ExpireDays > 0 {
		// 正数=从现在重新计算；0=保持当前有效期（避免编辑时误重置为永久）
		k.ExpiresAt = time.Now().Unix() + int64(req.ExpireDays)*86400
		model.UpdateOpenAPIKeyPreserveExpire(k)
	} else {
		if err := model.UpdateOpenAPIKey(k); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	common.ApiSuccess(c, nil)
}

// DeleteOpenKey DELETE /api/open_key/:id
func DeleteOpenKey(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if err := model.DeleteOpenAPIKey(id); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeSystem, fmt.Sprintf("删除数据开放密钥 #%d", id))
	common.ApiSuccess(c, nil)
}

// ---- 开放查询（密钥鉴权） ----

// OpenKeyAuth 开放密钥鉴权中间件
func OpenKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !common.ChatLogEnabled {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "对话日志功能未启用"})
			c.Abort()
			return
		}
		key := c.Request.Header.Get("Authorization")
		key = strings.TrimPrefix(strings.TrimPrefix(key, "Bearer "), "bearer ")
		key = strings.TrimSpace(key)
		// ?key= 兜底：便于在已登录浏览器直接打开验证（正式调用建议用 Header）
		if key == "" {
			key = strings.TrimSpace(c.Query("key"))
		}
		if key == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "缺少 Authorization: Bearer <开放密钥>"})
			c.Abort()
			return
		}
		k, scope, err := model.GetEnabledOpenAPIKey(key)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": err.Error()})
			c.Abort()
			return
		}
		c.Set("open_key_id", k.Id)
		c.Set("open_scope", scope)
		go model.TouchOpenAPIKey(k.Id)
		c.Next()
	}
}

// OpenQueryChatLogs GET /api/open/chat_logs
func OpenQueryChatLogs(c *gin.Context) {
	scope := c.MustGet("open_scope").(*model.OpenKeyScope)

	// 时间窗：默认最近 7 天；跨度受 max_days 限制；start/end 支持 2006-01-02 或 2006-01-02 15:04:05
	now := time.Now()
	startStr, endStr := c.Query("start"), c.Query("end")
	var start, end time.Time
	var err error
	parse := func(s string, def time.Time) (time.Time, error) {
		s = strings.TrimSpace(s)
		if s == "" {
			return def, nil
		}
		for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02", time.RFC3339} {
			if t, e := time.ParseInLocation(layout, s, time.Local); e == nil {
				return t, nil
			}
		}
		return time.Time{}, fmt.Errorf("时间格式应为 YYYY-MM-DD 或 YYYY-MM-DD HH:mm:ss")
	}
	if start, err = parse(startStr, now.AddDate(0, 0, -7)); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "start " + err.Error()})
		return
	}
	if end, err = parse(endStr, now); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "end " + err.Error()})
		return
	}
	if end.Before(start) {
		start, end = end, start
	}
	// 回看上限
	earliest := now.AddDate(0, 0, -scope.MaxDays)
	if start.Before(earliest) {
		start = earliest
	}
	if end.After(now) {
		end = now
	}

	filter := model.ChatLogFilter{
		StartTime: start.Unix(),
		EndTime:   end.Unix(),
		ModelName: strings.TrimSpace(c.Query("model_name")),
		TokenName: strings.TrimSpace(c.Query("token_name")),
		Group:     strings.TrimSpace(c.Query("group")),
	}

	// 权限：scope 限定用户集合
	allowed, err := model.ScopeAllowedUsernames(scope)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "权限解析失败: " + err.Error()})
		return
	}
	queryUsername := strings.TrimSpace(c.Query("username"))
	if allowed != nil {
		if queryUsername != "" {
			inScope := false
			for _, u := range allowed {
				if u == queryUsername {
					inScope = true
					break
				}
			}
			if !inScope {
				c.JSON(http.StatusOK, gin.H{"success": false, "message": "该用户不在密钥数据范围内"})
				return
			}
			filter.Usernames = []string{queryUsername}
		} else {
			// scope 集合下推 SQL，保证分页/total 正确
			filter.Usernames = allowed
		}
	} else if queryUsername != "" {
		filter.Username = queryUsername
	}

	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}

	// format=csv：与对话日志页「导出CSV」同列同序（顾问可直接拉报表）
	if c.Query("format") == "csv" {
		c.Writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
		c.Writer.Header().Set("Content-Disposition",
			fmt.Sprintf("attachment; filename=chat_logs_%s.csv", time.Now().Format("20060102_150405")))
		c.Writer.Write([]byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM（Excel 兼容）
		csvw := csv.NewWriter(c.Writer)
		header := []string{"日志ID", "时间", "用户ID", "用户名", "令牌", "渠道ID", "模型", "分组", "请求ID", "流式"}
		if scope.IncludeContent {
			header = append(header, "请求内容")
		}
		if err := csvw.Write(header); err != nil {
			return
		}
		_ = model.StreamAllChatLogs(filter, func(l *model.ChatLog) error {
			row := []string{
				strconv.Itoa(l.Id),
				time.Unix(l.CreatedAt, 0).Format("2006-01-02 15:04:05"),
				strconv.Itoa(l.UserId),
				l.Username,
				l.TokenName,
				strconv.Itoa(l.ChannelId),
				l.ModelName,
				l.Group,
				l.RequestId,
				strconv.FormatBool(l.IsStream),
			}
			if scope.IncludeContent {
				row = append(row, l.RequestContent)
			}
			return csvw.Write(row)
		})
		csvw.Flush()
		return
	}

	// stats=1：按用户汇总（顾问做使用总结的主形态）
	if c.Query("stats") == "1" {
		stats, err := model.GetChatLogUserStats(filter)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
			return
		}
		stats = filterStatsByScope(stats, allowed)
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"stats":  stats,
				"start":  start.Format("2006-01-02 15:04:05"),
				"end":    end.Format("2006-01-02 15:04:05"),
			},
		})
		return
	}

	logs, total, err := model.GetChatLogs(filter, page, pageSize)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	// 内容字段按权限裁剪
	if !scope.IncludeContent {
		for _, l := range logs {
			l.RequestContent = ""
			l.ResponseContent = ""
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"items":     logs,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
			"include_content": scope.IncludeContent,
			"start":     start.Format("2006-01-02 15:04:05"),
			"end":       end.Format("2006-01-02 15:04:05"),
		},
	})
}

func filterStatsByScope(stats []*model.ChatLogUserStat, allowed []string) []*model.ChatLogUserStat {
	if allowed == nil {
		return stats
	}
	set := map[string]bool{}
	for _, u := range allowed {
		set[u] = true
	}
	out := make([]*model.ChatLogUserStat, 0, len(stats))
	for _, s := range stats {
		if set[s.Username] {
			out = append(out, s)
		}
	}
	return out
}
