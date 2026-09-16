package service

// atrust_role_user_sync.go — AI用户角色成员自动建号。
//
// 用户决定：加入「AI用户」零信任角色即自动在 tokenhub 创建账号（含工号，
// SSO 登录按工号命中），管理员无需手动预建。原「工号回填同步」的定时
// 循环改为执行本同步（工号回填逻辑保留在 SyncEmployeeIdsFromATrust，
// 由本同步先执行），新建自动建号在其后运行：
//
//   1. 工号回填（原有能力：给已存在用户补 employee_id）
//   2. 角色成员自动建号：角色里还没有本地账号的（工号/姓名均未命中）→
//      自动创建（用户名=姓名、工号、default 分组、随机密码），幂等跳过已存在

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// ATrustRoleUserSyncResult 角色成员自动建号结果
type ATrustRoleUserSyncResult struct {
	RoleMembers int      `json:"role_members"`
	Created     int      `json:"created"`
	Skipped     int      `json:"skipped"`  // 工号或姓名已命中本地账号
	Failed      int      `json:"failed"`
	Errors      []string `json:"errors"`
}

// SyncATrustRoleUsers 同步「AI用户」角色成员：没有本地账号的自动创建
func SyncATrustRoleUsers() (*ATrustRoleUserSyncResult, error) {
	result := &ATrustRoleUserSyncResult{Errors: []string{}}

	members, err := ATrustQueryRoleMembers(system_setting.ATrustSyncRole)
	if err != nil {
		return nil, fmt.Errorf("拉取角色成员失败: %v", err)
	}
	result.RoleMembers = len(members)

	for _, m := range members {
		emp := strings.TrimSpace(m.Name)
		name := strings.TrimSpace(m.DisplayName)
		if emp == "" || name == "" {
			continue
		}

		// 工号已绑定 → 已有账号，跳过
		var exist model.User
		if err := model.DB.Where("employee_id = ?", emp).First(&exist).Error; err == nil {
			result.Skipped++
			continue
		}
		// 姓名已存在（username 或 display_name）→ 认为是同人，补绑工号
		var byName model.User
		if err := model.DB.Where(
			"username = ? OR display_name = ?", name, name,
		).First(&byName).Error; err == nil {
			if err := model.DB.Model(&byName).Update("employee_id", emp).Error; err == nil {
				common.SysLog("[角色同步] 补绑工号: " + byName.Username + " ← " + name + "(" + emp + ")")
			}
			result.Skipped++
			continue
		}

		// 自动建号：用户名=姓名（超长/冲突用工号），default 分组，随机密码
		username := name
		if len(username) > model.UserNameMaxLength {
			username = emp
		}
		if exists, _ := model.CheckUserExistOrDeleted(username, ""); exists {
			username = emp
		}
		if exists, _ := model.CheckUserExistOrDeleted(username, ""); exists {
			result.Failed++
			result.Errors = append(result.Errors, name+"("+emp+"): 用户名冲突")
			continue
		}
		newUser := model.User{
			Username:    username,
			DisplayName: name,
			EmployeeId:  emp,
			Password:    common.GetUUID(),
			Role:        common.RoleCommonUser,
			Status:      common.UserStatusEnabled,
			Group:       "default",
		}
		if err := newUser.Insert(0); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, name+"("+emp+"): "+err.Error())
			continue
		}
		result.Created++
	}
	return result, nil
}

// ListATrustOrphanUsers 列出孤儿账号：已绑工号但工号不在当前角色成员中。
// 早期用 queryAll 的 roleIdList 判成员导致把非成员也建了号（532 vs 90），
// 本接口用于找出并清理这些账号。
type ATrustOrphanUser struct {
	Id          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	EmployeeId  string `json:"employee_id"`
	CreatedAt   int64  `json:"created_at"`
}

func ListATrustOrphanUsers() ([]ATrustOrphanUser, error) {
	members, err := ATrustQueryRoleMembers(system_setting.ATrustSyncRole)
	if err != nil {
		return nil, fmt.Errorf("拉取角色成员失败: %v", err)
	}
	valid := make(map[string]bool, len(members))
	for _, m := range members {
		valid[strings.TrimSpace(m.Name)] = true
	}

	var users []ATrustOrphanUser
	err = model.DB.Model(&model.User{}).
		Select("id, username, display_name, employee_id, created_at").
		Where("employee_id != ''").
		Find(&users).Error
	if err != nil {
		return nil, err
	}
	out := make([]ATrustOrphanUser, 0)
	for _, u := range users {
		if !valid[strings.TrimSpace(u.EmployeeId)] {
			out = append(out, u)
		}
	}
	return out, nil
}

// DeleteUsersByIds 软删除指定用户（与用户管理删除行为一致）
func DeleteUsersByIds(ids []int) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res := model.DB.Where("id IN ?", ids).Delete(&model.User{})
	if res.Error != nil {
		return 0, res.Error
	}
	for _, id := range ids {
		_ = model.InvalidateUserCache(id)
	}
	return int(res.RowsAffected), nil
}
