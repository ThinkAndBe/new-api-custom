package controller

import (
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

const userImportMaxRows = 500

// importUserRow 导入请求中的单行用户
type importUserRow struct {
	Username    string  `json:"username"`
	Password    string  `json:"password"`
	DisplayName string  `json:"display_name"`
	Group       string  `json:"group"`
	QuotaCNY    float64 `json:"quota_cny"`
}

type importUsersRequest struct {
	Users []importUserRow `json:"users"`
}

// importRowResult 单行导入结果
type importRowResult struct {
	Row      int    `json:"row"`
	Username string `json:"username"`
	Status   string `json:"status"` // success | duplicate | error
	Message  string `json:"message,omitempty"`
}

// ImportUsers POST /api/user/import
// 批量导入用户。前端解析 CSV 后提交 JSON。
// 每行独立处理：某行失败不影响其他行，返回逐行结果报告。
func ImportUsers(c *gin.Context) {
	var req importUsersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if len(req.Users) == 0 {
		common.ApiErrorMsg(c, "导入列表为空")
		return
	}
	if len(req.Users) > userImportMaxRows {
		common.ApiErrorMsg(c, fmt.Sprintf("单次最多导入 %d 个用户", userImportMaxRows))
		return
	}
	// 分组合法性预检（分组配置来自 GroupRatio 表）
	validGroups := ratio_setting.GetGroupRatioCopy()

	results := make([]importRowResult, 0, len(req.Users))
	successCount, duplicateCount, errorCount := 0, 0, 0
	for i, row := range req.Users {
		res := importRowResult{Row: i + 1, Username: strings.TrimSpace(row.Username)}

		// 校验
		if res.Username == "" || row.Password == "" {
			res.Status = "error"
			res.Message = "用户名或密码为空"
			errorCount++
			results = append(results, res)
			continue
		}
		if len(res.Username) > 20 {
			res.Status = "error"
			res.Message = "用户名超过 20 字符"
			errorCount++
			results = append(results, res)
			continue
		}
		if len(row.Password) < 8 || len(row.Password) > 20 {
			res.Status = "error"
			res.Message = "密码长度需在 8-20 之间"
			errorCount++
			results = append(results, res)
			continue
		}
		group := strings.TrimSpace(row.Group)
		if group == "" {
			group = "default"
		}
		if _, ok := validGroups[group]; !ok {
			res.Status = "error"
			res.Message = "分组不存在: " + group
			errorCount++
			results = append(results, res)
			continue
		}
		displayName := strings.TrimSpace(row.DisplayName)
		if displayName == "" {
			displayName = res.Username
		}
		if len(displayName) > 20 {
			res.Status = "error"
			res.Message = "显示名超过 20 字符"
			errorCount++
			results = append(results, res)
			continue
		}

		// 查重（用户名或邮箱占用，含已注销）
		exist, err := model.CheckUserExistOrDeleted(res.Username, "")
		if err != nil {
			res.Status = "error"
			res.Message = "查询失败: " + err.Error()
			errorCount++
			results = append(results, res)
			continue
		}
		if exist {
			res.Status = "duplicate"
			res.Message = "用户名已存在（或已注销）"
			duplicateCount++
			results = append(results, res)
			continue
		}

		// 插入（固定普通用户角色、首登改密）
		user := model.User{
			Username:           res.Username,
			Password:           row.Password,
			DisplayName:        displayName,
			Group:              group,
			Role:               common.RoleCommonUser,
			MustChangePassword: true,
		}
		if err := user.Insert(0); err != nil {
			res.Status = "error"
			res.Message = "创建失败: " + err.Error()
			errorCount++
			results = append(results, res)
			continue
		}

		// 额度覆盖：Insert 会重置为 QuotaForNewUser，指定了额度则覆盖
		if row.QuotaCNY > 0 {
			quota := int(row.QuotaCNY * common.QuotaPerUnit)
			if err := model.DB.Model(&model.User{}).Where("id = ?", user.Id).Update("quota", quota).Error; err != nil {
				res.Status = "error"
				res.Message = "用户已创建但额度设置失败: " + err.Error()
				errorCount++
				results = append(results, res)
				continue
			}
			if err := model.InvalidateUserCache(user.Id); err != nil {
				common.SysLog(fmt.Sprintf("import: invalidate user cache %d failed: %s", user.Id, err.Error()))
			}
		}

		res.Status = "success"
		successCount++
		results = append(results, res)
	}

	recordManageAudit(c, "user.import", map[string]interface{}{
		"count":     len(req.Users),
		"success":   successCount,
		"duplicate": duplicateCount,
		"error":     errorCount,
	})
	common.ApiSuccess(c, gin.H{
		"results":        results,
		"total":          len(req.Users),
		"success_count":  successCount,
		"duplicate": duplicateCount,
		"error_count":    errorCount,
	})
}

// ExportUsers GET /api/user/export
// 按当前筛选条件流式导出用户 CSV（不含密码/令牌等敏感字段）。
func ExportUsers(c *gin.Context) {
	keyword := c.Query("keyword")
	group := c.Query("group")
	var rolePtr *int
	if v := c.Query("role"); v != "" {
		if r, err := strconv.Atoi(v); err == nil {
			rolePtr = &r
		}
	}
	var statusPtr *int
	if v := c.Query("status"); v != "" {
		if s, err := strconv.Atoi(v); err == nil {
			statusPtr = &s
		}
	}
	users, _, err := model.SearchUsers(keyword, group, rolePtr, statusPtr, 0, 100000)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	setCSVHeaders(c, "users")
	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{"ID", "用户名", "显示名", "分组", "角色", "状态", "额度", "已用额度", "请求数", "邮箱", "备注", "注册时间", "最后登录"})
	roleName := map[int]string{1: "普通用户", 10: "管理员", 100: "Root"}
	statusName := map[int]string{1: "启用", 2: "禁用", 3: "封禁"}
	for _, u := range users {
		st := statusName[u.Status]
		if st == "" {
			st = fmt.Sprintf("状态%d", u.Status)
		}
		_ = w.Write([]string{
			strconv.Itoa(u.Id),
			u.Username,
			u.DisplayName,
			u.Group,
			roleName[u.Role],
			st,
			logger.FormatQuota(u.Quota),
			logger.FormatQuota(u.UsedQuota),
			strconv.Itoa(u.RequestCount),
			u.Email,
			u.Remark,
			formatUnixTime(u.CreatedAt),
			formatUnixTime(u.LastLoginAt),
		})
	}
	w.Flush()
}

func formatUnixTime(ts int64) string {
	if ts == 0 {
		return ""
	}
	return time.Unix(ts, 0).Format("2006-01-02 15:04:05")
}

// ManageUserBatchRequest 批量管理请求
type ManageUserBatchRequest struct {
	Ids    []int  `json:"ids"`
	Action string `json:"action"` // disable | enable | delete
	Value  int    `json:"value"`  // add_quota 时的额度（quota 单位）
	Mode   string `json:"mode"`   // add_quota 时的模式
}

// ManageUserBatch POST /api/user/manage_batch
// 批量启用/禁用/注销/调额度。逐个执行，返回逐个结果。
// 支持的 action 与单个管理一致（不含 promote/demote——批量提权降级风险高，走单个操作）。
func ManageUserBatch(c *gin.Context) {
	var req ManageUserBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if len(req.Ids) == 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if len(req.Ids) > userImportMaxRows {
		common.ApiErrorMsg(c, fmt.Sprintf("单次最多操作 %d 个用户", userImportMaxRows))
		return
	}
	myRole := c.GetInt("role")
	type rowResult struct {
		Id       int    `json:"id"`
		Username string `json:"username"`
		Status   string `json:"status"` // success | error
		Message  string `json:"message,omitempty"`
	}
	results := make([]rowResult, 0, len(req.Ids))
	successCount := 0
	for _, id := range req.Ids {
		var user model.User
		model.DB.Unscoped().Where("id = ?", id).First(&user)
		if user.Id == 0 {
			results = append(results, rowResult{Id: id, Status: "error", Message: "用户不存在"})
			continue
		}
		if !canManageTargetRole(myRole, user.Role) {
			results = append(results, rowResult{Id: id, Username: user.Username, Status: "error", Message: "无权操作该用户"})
			continue
		}
		var err error
		switch req.Action {
		case "disable":
			if user.Role == common.RoleRootUser {
				err = fmt.Errorf("不能禁用 Root 用户")
			} else {
				user.Status = common.UserStatusDisabled
				err = user.Update(false)
			}
		case "enable":
			user.Status = common.UserStatusEnabled
			err = user.Update(false)
		case "delete":
			if user.Role == common.RoleRootUser {
				err = fmt.Errorf("不能注销 Root 用户")
			} else {
				err = user.Delete()
			}
		case "add_quota":
			switch req.Mode {
			case "add":
				if req.Value <= 0 {
					err = fmt.Errorf("额度必须大于 0")
				} else {
					err = model.IncreaseUserQuota(user.Id, req.Value, true)
				}
			case "subtract":
				if req.Value <= 0 {
					err = fmt.Errorf("额度必须大于 0")
				} else {
					err = model.DecreaseUserQuota(user.Id, req.Value, true)
				}
			case "override":
				err = model.DB.Model(&model.User{}).Where("id = ?", user.Id).Update("quota", req.Value).Error
			default:
				err = fmt.Errorf("无效的额度模式")
			}
		default:
			err = fmt.Errorf("无效的操作: %s", req.Action)
		}
		if err != nil {
			results = append(results, rowResult{Id: id, Username: user.Username, Status: "error", Message: err.Error()})
			continue
		}
		// 禁用/注销后失效缓存，避免 TTL 内仍可用
		if req.Action == "disable" || req.Action == "delete" {
			if err := model.InvalidateUserCache(user.Id); err != nil {
				common.SysLog(fmt.Sprintf("batch: invalidate user cache %d failed: %s", user.Id, err.Error()))
			}
			if err := model.InvalidateUserTokensCache(user.Id); err != nil {
				common.SysLog(fmt.Sprintf("batch: invalidate tokens cache %d failed: %s", user.Id, err.Error()))
			}
		}
		results = append(results, rowResult{Id: id, Username: user.Username, Status: "success"})
		successCount++
	}
	recordManageAudit(c, "user.manage_batch", map[string]interface{}{
		"action":  req.Action,
		"count":   len(req.Ids),
		"success": successCount,
	})
	common.ApiSuccess(c, gin.H{
		"results":       results,
		"total":         len(req.Ids),
		"success_count": successCount,
	})
}
