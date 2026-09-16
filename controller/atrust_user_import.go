package controller

// atrust_user_import.go — 从零信任目录预建用户（解决「未登录无法配置」）。
//
// 用户首次零信任登录前账号不存在，管理员无法预配分组和额度。本模块：
//   GET  /api/user/atrust_directory?keyword=&only_new=true
//        拉取零信任目录（复用 queryAll），标记哪些已存在于本地
//        （按工号/用户名/显示名匹配）
//   POST /api/user/import_atrust
//        批量预建：写入 employee_id（工号）+ 显示名 + 分组 + 额度；
//        这些用户后续 SSO 登录时按工号精确命中预建账号（分组额度即生效），
//        不再走自动建号的 default 分组。
//
// 密码随机生成（用户经零信任登录永远用不到，也满足密码字段约束）。

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// ATrustDirectoryUser 目录条目 + 本地存在性标记
type ATrustDirectoryUser struct {
	EmployeeId string `json:"employee_id"`
	DisplayName string `json:"display_name"`
	GroupPath   string `json:"group_path"`
	ExistsLocal bool   `json:"exists_local"` // 工号/姓名已存在于本地
	MatchedUser string `json:"matched_user"` // 命中的本地用户名
}

// GetATrustDirectory GET /api/user/atrust_directory
//   scope=role（默认）：仅拉「AI用户」角色成员（tokenhub 访问白名单，约数百人）
//   scope=all：全量目录（3万+，慎用）
func GetATrustDirectory(c *gin.Context) {
	var dirUsers []service.ATrustDirectoryUser
	var err error
	if c.Query("scope") == "all" {
		dirUsers, err = service.ATrustQueryDirectoryUsers()
	} else {
		dirUsers, err = service.ATrustQueryRoleMembers(system_setting.ATrustSyncRole)
	}
	if err != nil {
		common.ApiErrorMsg(c, "拉取零信任目录失败: "+err.Error())
		return
	}
	keyword := strings.TrimSpace(c.Query("keyword"))
	onlyNew := c.Query("only_new") == "true"

	// 本地已有用户索引：employee_id、username、display_name
	type existRow struct{ EmployeeId, Username, DisplayName string }
	var exists []existRow
	if err := model.DB.Model(&model.User{}).
		Select("employee_id, username, display_name").Find(&exists).Error; err == nil {
		// ignore 查询失败：只影响标记
	}
	byEmp := map[string]string{}
	byName := map[string]string{}
	for _, e := range exists {
		if e.EmployeeId != "" {
			byEmp[e.EmployeeId] = e.Username
		}
		if e.DisplayName != "" {
			byName[e.DisplayName] = e.Username
		}
	}

	out := make([]ATrustDirectoryUser, 0, len(dirUsers))
	for _, u := range dirUsers {
		if keyword != "" &&
			!strings.Contains(u.DisplayName, keyword) &&
			!strings.Contains(u.Name, keyword) {
			continue
		}
		item := ATrustDirectoryUser{
			EmployeeId:  u.Name,
			DisplayName: u.DisplayName,
			GroupPath:   "",
		}
		if un, ok := byEmp[u.Name]; ok {
			item.ExistsLocal, item.MatchedUser = true, un
		} else if un, ok := byName[u.DisplayName]; ok {
			item.ExistsLocal, item.MatchedUser = true, un
		}
		if onlyNew && item.ExistsLocal {
			continue
		}
		out = append(out, item)
	}
	common.ApiSuccess(c, gin.H{"users": out, "total": len(out)})
}

type atrustImportItem struct {
	EmployeeId  string  `json:"employee_id"`
	DisplayName string  `json:"display_name"`
	Group       string  `json:"group"`
	QuotaCNY    float64 `json:"quota_cny"`
}

type atrustImportReq struct {
	Users []atrustImportItem `json:"users"`
}

// ImportATrustUsers POST /api/user/import_atrust
func ImportATrustUsers(c *gin.Context) {
	var req atrustImportReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if len(req.Users) == 0 {
		common.ApiErrorMsg(c, "导入列表为空")
		return
	}
	if len(req.Users) > 500 {
		common.ApiErrorMsg(c, "单次最多导入 500 个用户")
		return
	}
	validGroups := ratio_setting.GetGroupRatioCopy()

	type rowResult struct {
		DisplayName string `json:"display_name"`
		EmployeeId  string `json:"employee_id"`
		Status      string `json:"status"`
		Message     string `json:"message"`
	}
	results := make([]rowResult, 0, len(req.Users))
	success, skip, fail := 0, 0, 0

	for _, row := range req.Users {
		emp := strings.TrimSpace(row.EmployeeId)
		name := strings.TrimSpace(row.DisplayName)
		res := rowResult{DisplayName: name, EmployeeId: emp}
		if emp == "" || name == "" {
			res.Status, res.Message = "error", "工号或姓名为空"
			fail++
			results = append(results, res)
			continue
		}
		// 工号已绑定 → 跳过（该用户登录即命中既有账号，配置走用户管理）
		var exist model.User
		if err := model.DB.Where("employee_id = ?", emp).First(&exist).Error; err == nil {
			res.Status, res.Message = "duplicate", "工号已绑定用户 "+exist.Username
			skip++
			results = append(results, res)
			continue
		}
		// 用户名冲突检查：以显示名作用户名（与 CSV 导入惯例一致），冲突则用工号
		username := name
		if len(username) > model.UserNameMaxLength {
			username = emp
		}
		if exists, err := model.CheckUserExistOrDeleted(username, ""); err == nil && exists {
			username = emp
		}
		if exists, err := model.CheckUserExistOrDeleted(username, ""); err == nil && exists {
			res.Status, res.Message = "duplicate", "用户名与工号均冲突，请手动处理"
			skip++
			results = append(results, res)
			continue
		}
		group := strings.TrimSpace(row.Group)
		if group == "" {
			group = "default"
		}
		if _, ok := validGroups[group]; !ok {
			res.Status, res.Message = "error", "分组不存在: "+group
			fail++
			results = append(results, res)
			continue
		}

		user := model.User{
			Username:    username,
			DisplayName: name,
			EmployeeId:  emp,
			Password:    common.GetUUID(), // 随机：零信任登录用不到
			Group:       group,
			Role:        common.RoleCommonUser,
			Status:      common.UserStatusEnabled,
		}
		if err := user.Insert(0); err != nil {
			res.Status, res.Message = "error", "创建失败: "+err.Error()
			fail++
			results = append(results, res)
			continue
		}
		if row.QuotaCNY > 0 {
			quota := int(row.QuotaCNY * common.QuotaPerUnit)
			if err := model.DB.Model(&model.User{}).Where("id = ?", user.Id).
				Update("quota", quota).Error; err != nil {
				res.Status, res.Message = "error", "已创建但额度设置失败"
				fail++
				results = append(results, res)
				continue
			}
			_ = model.InvalidateUserCache(user.Id)
		}
		res.Status, res.Message = "success", "已创建（SSO 登录按工号命中）"
		success++
		results = append(results, res)
	}

	model.RecordLog(c.GetInt("id"), model.LogTypeSystem,
		fmt.Sprintf("零信任预建用户：导入 %d（成功 %d 跳过 %d 失败 %d）",
			len(req.Users), success, skip, fail))
	common.ApiSuccess(c, gin.H{
		"results": results, "total": len(req.Users),
		"success_count": success, "duplicate_count": skip, "error_count": fail,
	})
}
