package service

// atrust_employee_sync.go — 从零信任用户目录批量回填本地用户工号。
//
// 数据源：OpenAPI V3 /api/v3/user/queryAll 全量目录（不再用在线用户快照，
// 后者是动态集合，覆盖不全）。目录的 name 即工号、displayName 即姓名，
// 与在线接口/SSO getUserInfoByCode 同源一致（已实测对齐）。
//
// 同步范围：只处理 ATrustSyncGroups 指定分组（默认「AI用户」）的本地账号，
// 管理员、测试账号等不受影响。匹配：本地 username/display_name 精确等于
// 目录 displayName；目录同名多人时跳过并报告（以零信任目录为权威，
// 工号变化时以目录为准更新）。

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// ATrustDirectoryUser queryAll 返回的目录用户（仅取同步所需字段）
type ATrustDirectoryUser struct {
	Id          string   `json:"id"`          // 用户 id（角色成员 userIdList 按此匹配）
	Name        string   `json:"name"`        // 工号（账号名）
	DisplayName string   `json:"displayName"` // 姓名
	GroupPath   string   `json:"groupPath"`   // 组织架构路径
	Status      int      `json:"status"`      // 0-禁用 1-启用
	IsDeleted   int      `json:"isDeleted"`
	RoleIdList  []string `json:"roleIdList"` // 关联角色（微信目录存角色的 externalId，本地目录存 id）
}

// CenterFromGroupPath 从零信任组织路径提取「所在中心」。
// 规则（2026-09-20 按 149 名真实角色成员校准）：取第一个以「中心」结尾的段
// （如 /鸿星尔克实业/制造基地/鞋业开发中心/业务组 → 鞋业开发中心，143/149 命中）；
// 路径中没有「中心」段的（大货销售系统、电商事业部这类事业部级组织），取第三段兜底。
func CenterFromGroupPath(groupPath string) string {
	segs := strings.Split(strings.Trim(groupPath, "/"), "/")
	for _, s := range segs {
		if strings.HasSuffix(s, "中心") {
			return s
		}
	}
	if len(segs) >= 3 {
		return segs[2]
	}
	return ""
}

// MatchATrustSyncEmployeeId 工号白名单过滤（ATrustSyncEmployeeIds 空=不过滤）。
// 用于收敛「角色被绑定到组织节点后继承出的大量成员」，只保留控制台
// 直接成员对应的工号名单。
func MatchATrustSyncEmployeeId(employeeId string) bool {
	raw := strings.TrimSpace(system_setting.ATrustSyncEmployeeIds)
	if raw == "" {
		return true
	}
	for _, id := range strings.Split(raw, ",") {
		if strings.TrimSpace(id) == employeeId {
			return true
		}
	}
	return false
}

// MatchATrustSyncPath 组织路径白名单过滤（ATrustSyncPaths 空=不过滤）
func MatchATrustSyncPath(groupPath string) bool {
	raw := strings.TrimSpace(system_setting.ATrustSyncPaths)
	if raw == "" {
		return true
	}
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" && strings.HasPrefix(groupPath, p) {
			return true
		}
	}
	return false
}

type atrustDirectoryResponse struct {
	Code interface{} `json:"code"` // 成功为字符串 "OK"，失败为数字
	Msg  string      `json:"msg"`
	Data struct {
		Count int                   `json:"count"`
		Data  []ATrustDirectoryUser `json:"data"`
	} `json:"data"`
}

// ATrustRole queryAll 返回的角色
type ATrustRole struct {
	Id         string `json:"id"`
	Name       string `json:"name"`
	ExternalId string `json:"externalId"`
	IsDeleted  int    `json:"isDeleted"`
}

type atrustRoleResponse struct {
	Code interface{} `json:"code"`
	Msg  string      `json:"msg"`
	Data struct {
		Count int          `json:"count"`
		Data  []ATrustRole `json:"data"`
	} `json:"data"`
}

// ATrustQueryDirectoryUsers 分页拉取全量目录（仅启用且未删除的成员）
func ATrustQueryDirectoryUsers() ([]ATrustDirectoryUser, error) {
	cfg := GetATrustConfig()
	if cfg.Server == "" || cfg.APIId == "" || cfg.APISecret == "" {
		return nil, fmt.Errorf("aTrust OpenAPI 未配置")
	}
	domain := strings.TrimSpace(system_setting.ATrustDirectoryDomain)
	if domain == "" {
		return nil, fmt.Errorf("aTrust 目录标识未配置（ATrustDirectoryDomain）")
	}

	var all []ATrustDirectoryUser
	const pageSize = 5000
	for pageIndex := 1; ; pageIndex++ {
		// 签名要求与请求体逐字节一致：手拼紧凑 JSON（无空格、中文原文）
		body := fmt.Sprintf(`{"directoryDomain":%q,"pageSize":%d,"pageIndex":%d}`, domain, pageSize, pageIndex)
		raw, err := atrustRequest(cfg, "POST", "/api/v3/user/queryAll", "", body)
		if err != nil {
			return nil, err
		}
		var resp atrustDirectoryResponse
		if err := common.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("解析 aTrust 目录响应失败: %v", err)
		}
		if fmt.Sprintf("%v", resp.Code) != "OK" {
			return nil, fmt.Errorf("aTrust 目录查询失败 code=%v msg=%s", resp.Code, resp.Msg)
		}
		for _, u := range resp.Data.Data {
			if u.IsDeleted == 0 && u.Status == 1 && u.Name != "" && u.DisplayName != "" {
				all = append(all, u)
			}
		}
		if len(all) >= resp.Data.Count || len(resp.Data.Data) == 0 {
			break
		}
	}
	return all, nil
}

// ATrustQueryRoles 拉取目录的角色列表
func ATrustQueryRoles() ([]ATrustRole, error) {
	cfg := GetATrustConfig()
	domain := strings.TrimSpace(system_setting.ATrustDirectoryDomain)
	if cfg.Server == "" || cfg.APIId == "" || cfg.APISecret == "" || domain == "" {
		return nil, fmt.Errorf("aTrust OpenAPI 或目录标识未配置")
	}
	var all []ATrustRole
	const pageSize = 500
	for pageIndex := 1; ; pageIndex++ {
		body := fmt.Sprintf(`{"directoryDomain":%q,"pageSize":%d,"pageIndex":%d}`, domain, pageSize, pageIndex)
		raw, err := atrustRequest(cfg, "POST", "/api/v3/role/queryAll", "", body)
		if err != nil {
			return nil, err
		}
		var resp atrustRoleResponse
		if err := common.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("解析 aTrust 角色响应失败: %v", err)
		}
		if fmt.Sprintf("%v", resp.Code) != "OK" {
			return nil, fmt.Errorf("aTrust 角色查询失败 code=%v msg=%s", resp.Code, resp.Msg)
		}
		for _, r := range resp.Data.Data {
			if r.IsDeleted == 0 {
				all = append(all, r)
			}
		}
		if len(all) >= resp.Data.Count || len(resp.Data.Data) == 0 {
			break
		}
	}
	return all, nil
}

type atrustRoleDetailResponse struct {
	Code interface{} `json:"code"`
	Msg  string      `json:"msg"`
	Data struct {
		Id          string   `json:"id"`
		Name        string   `json:"name"`
		ExternalId  string   `json:"externalId"`
		UserIdList  []string `json:"userIdList"`  // 角色直接关联的用户 id（权威成员列表）
		GroupIdList []string `json:"groupIdList"` // 关联的组织架构 id（有值说明还绑了组织节点）
	} `json:"data"`
}

// ATrustQueryRoleUserIds 查询角色的直接成员用户 id 列表。
// 注意：不要用 queryAll 的 roleIdList 判成员——该字段语义与「关联角色」不一致
// （实测某用户 roleIdList 56 项而控制台仅 1 项），会把非成员误判进来。
// role/queryById 的 userIdList 才是与 aTrust 控制台一致的直接成员。
func ATrustQueryRoleUserIds(roleId string) ([]string, error) {
	cfg := GetATrustConfig()
	domain := strings.TrimSpace(system_setting.ATrustDirectoryDomain)
	if cfg.Server == "" || cfg.APIId == "" || cfg.APISecret == "" || domain == "" {
		return nil, fmt.Errorf("aTrust OpenAPI 或目录标识未配置")
	}
	query := "directoryDomain=" + domain + "&id=" + roleId
	raw, err := atrustRequest(cfg, "GET", "/api/v3/role/queryById", query, "")
	if err != nil {
		return nil, err
	}
	var resp atrustRoleDetailResponse
	if err := common.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("解析角色详情失败: %v", err)
	}
	if fmt.Sprintf("%v", resp.Code) != "OK" {
		return nil, fmt.Errorf("角色详情查询失败 code=%v msg=%s", resp.Code, resp.Msg)
	}
	if len(resp.Data.GroupIdList) > 0 {
		common.SysLog(fmt.Sprintf("[角色同步] 提示：角色 %s 还绑定了 %d 个组织架构节点（成员含继承）",
			resp.Data.Name, len(resp.Data.GroupIdList)))
	}
	return resp.Data.UserIdList, nil
}

// ATrustQueryRoleMembers 拉取目录中指定角色的成员。
// 微信目录用户的 roleIdList 存的是角色 externalId（本地目录则存 id），
// 两种标识都收集匹配。
func ATrustQueryRoleMembers(roleName string) ([]ATrustDirectoryUser, error) {
	roles, err := ATrustQueryRoles()
	if err != nil {
		return nil, err
	}
	var roleId string
	for _, r := range roles {
		if r.Name == roleName {
			roleId = r.Id
			break
		}
	}
	if roleId == "" {
		return nil, fmt.Errorf("零信任目录中不存在角色「%s」", roleName)
	}

	userIds, err := ATrustQueryRoleUserIds(roleId)
	if err != nil {
		return nil, err
	}
	idSet := make(map[string]bool, len(userIds))
	for _, id := range userIds {
		idSet[id] = true
	}

	all, err := ATrustQueryDirectoryUsers()
	if err != nil {
		return nil, err
	}
	var members []ATrustDirectoryUser
	for _, u := range all {
		if idSet[u.Id] && MatchATrustSyncPath(u.GroupPath) && MatchATrustSyncEmployeeId(u.Name) {
			members = append(members, u)
		}
	}
	return members, nil
}

// ATrustSyncReport 工号同步结果报告
type ATrustSyncReport struct {
	RoleMembers int      `json:"role_members"` // 零信任角色成员数（同步数据源）
	TargetUsers int      `json:"target_users"` // 参与同步的本地账号数（按分组过滤后）
	Synced      int      `json:"synced"`       // 本次新写入工号
	Overwritten int      `json:"overwritten"`  // 工号按目录更新（与原值不同）
	SkippedSame int      `json:"skipped_same"` // 已绑定且一致
	Ambiguous   []string `json:"ambiguous"`    // 角色成员同名多人：姓名(工号列表)
	Unmatched   []string `json:"unmatched"`    // 本地账号在角色成员中无同名：用户名
}

// SyncEmployeeIdsFromATrust 执行一次工号批量同步（角色成员为源，本地为目标）
func SyncEmployeeIdsFromATrust() (*ATrustSyncReport, error) {
	roleName := strings.TrimSpace(system_setting.ATrustSyncRole)
	if roleName == "" {
		return nil, fmt.Errorf("未配置同步角色（ATrustSyncRole）")
	}
	dirUsers, err := ATrustQueryRoleMembers(roleName)
	if err != nil {
		return nil, fmt.Errorf("拉取零信任角色成员失败: %v", err)
	}

	// displayName → 工号集合（角色成员同名多人时会有多个工号）+ 目录用户（取中心用）
	nameIndex := make(map[string][]string, len(dirUsers))
	nameUsers := make(map[string]map[string]ATrustDirectoryUser, len(dirUsers))
	for _, u := range dirUsers {
		name := strings.TrimSpace(u.DisplayName)
		if name != "" {
			emp := strings.TrimSpace(u.Name)
			nameIndex[name] = append(nameIndex[name], emp)
			if nameUsers[name] == nil {
				nameUsers[name] = map[string]ATrustDirectoryUser{}
			}
			nameUsers[name][emp] = u
		}
	}

	// 目标本地账号：启用账号（可再用本地分组收窄，默认全部）
	groups := parseATrustSyncGroups()
	var targets []model.User
	q := model.DB.Where("status = ?", common.UserStatusEnabled)
	if len(groups) > 0 {
		q = q.Where(map[string]interface{}{"group": groups})
	}
	if err := q.Find(&targets).Error; err != nil {
		return nil, err
	}

	report := &ATrustSyncReport{
		RoleMembers: len(dirUsers),
		TargetUsers: len(targets),
		Ambiguous:   []string{},
		Unmatched:   []string{},
	}

	for i := range targets {
		u := &targets[i]
		// 本地 username/display_name 精确匹配目录 displayName，去重工号
		ids := map[string]bool{}
		for _, key := range []string{strings.TrimSpace(u.Username), strings.TrimSpace(u.DisplayName)} {
			if key == "" {
				continue
			}
			for _, id := range nameIndex[key] {
				ids[id] = true
			}
		}
		switch len(ids) {
		case 0:
			report.Unmatched = append(report.Unmatched, u.Username)
		case 1:
			var employeeId string
			for id := range ids {
				employeeId = id
			}
			// 所在中心：以目录为准，工号相同也刷新（组织调动的场景）
			center := ""
			if du, ok := nameUsers[strings.TrimSpace(u.Username)][employeeId]; ok {
				center = CenterFromGroupPath(du.GroupPath)
			} else if du, ok := nameUsers[strings.TrimSpace(u.DisplayName)][employeeId]; ok {
				center = CenterFromGroupPath(du.GroupPath)
			}
			if u.EmployeeId == employeeId {
				if center != "" && center != u.Center {
					if err := model.DB.Model(u).Update("center", center).Error; err != nil {
						common.SysError("[工号同步] 更新中心失败 " + u.Username + ": " + err.Error())
					} else {
						common.SysLog("[工号同步] 更新中心: " + u.Username + " → " + center)
					}
				}
				report.SkippedSame++
				continue
			}
			if u.EmployeeId != "" {
				report.Overwritten++
			} else {
				report.Synced++
			}
			updates := map[string]interface{}{"employee_id": employeeId}
			if center != "" {
				updates["center"] = center
			}
			if err := model.DB.Model(u).Updates(updates).Error; err != nil {
				common.SysError("[工号同步] 写入失败 " + u.Username + ": " + err.Error())
			} else {
				common.SysLog("[工号同步] " + u.Username + " ← 工号 " + employeeId)
			}
		default:
			names := make([]string, 0, len(ids))
			for id := range ids {
				names = append(names, id)
			}
			report.Ambiguous = append(report.Ambiguous,
				u.DisplayName+"("+strings.Join(names, "/")+")")
		}
	}
	return report, nil
}

// parseATrustSyncGroups 解析同步分组配置（逗号分隔，空=全部）
func parseATrustSyncGroups() []string {
	var groups []string
	for _, g := range strings.Split(system_setting.ATrustSyncGroups, ",") {
		if g = strings.TrimSpace(g); g != "" {
			groups = append(groups, g)
		}
	}
	return groups
}

// StartATrustEmployeeSyncLoop 定时工号同步：每 6 小时一次，
// OpenAPI 未配置时静默跳过。
func StartATrustEmployeeSyncLoop() {
	go func() {
		// 启动后延迟 5 分钟再跑首次，避开启动高峰
		time.Sleep(5 * time.Minute)
		runATrustEmployeeSync()
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			runATrustEmployeeSync()
		}
	}()
}

func runATrustEmployeeSync() {
	cfg := GetATrustConfig()
	if cfg.Server == "" || cfg.APIId == "" || cfg.APISecret == "" {
		return
	}
	report, err := SyncEmployeeIdsFromATrust()
	if err != nil {
		common.SysError("[工号定时同步] " + err.Error())
	} else {
		common.SysLog(fmt.Sprintf("[工号定时同步] 角色成员 %d 新绑定 %d 更新 %d 一致 %d",
			report.RoleMembers, report.Synced, report.Overwritten, report.SkippedSame))
	}

	// AI用户角色成员自动建号：加入角色即自动创建账号（含工号）
	rr, err := SyncATrustRoleUsers()
	if err != nil {
		common.SysError("[角色用户同步] " + err.Error())
		return
	}
	if rr.Created > 0 || rr.Failed > 0 {
		common.SysLog(fmt.Sprintf("[角色用户同步] 成员 %d 新建 %d 跳过 %d 失败 %d",
			rr.RoleMembers, rr.Created, rr.Skipped, rr.Failed))
		for _, e := range rr.Errors {
			if len(rr.Errors) <= 5 || rr.Failed <= 5 {
				common.SysLog("[角色用户同步] " + e)
			}
		}
	}
}
