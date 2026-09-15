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
	Name        string `json:"name"`        // 工号（账号名）
	DisplayName string `json:"displayName"` // 姓名
	Status      int    `json:"status"`      // 0-禁用 1-启用
	IsDeleted   int    `json:"isDeleted"`
}

type atrustDirectoryResponse struct {
	Code interface{} `json:"code"` // 成功为字符串 "OK"，失败为数字
	Msg  string      `json:"msg"`
	Data struct {
		Count int                   `json:"count"`
		Data  []ATrustDirectoryUser `json:"data"`
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

// ATrustSyncReport 工号同步结果报告
type ATrustSyncReport struct {
	DirectoryTotal int      `json:"directory_total"` // 零信任目录成员数（启用未删）
	TargetUsers    int      `json:"target_users"`    // 参与同步的本地账号数（按分组过滤后）
	Synced         int      `json:"synced"`          // 本次新写入工号
	Overwritten    int      `json:"overwritten"`     // 工号按目录更新（与原值不同）
	SkippedSame    int      `json:"skipped_same"`    // 已绑定且一致
	Ambiguous      []string `json:"ambiguous"`       // 目录同名多人：姓名(工号列表)
	Unmatched      []string `json:"unmatched"`       // 本地账号在目录中无同名：用户名
}

// SyncEmployeeIdsFromATrust 执行一次工号批量同步（目录为源，本地为目标）
func SyncEmployeeIdsFromATrust() (*ATrustSyncReport, error) {
	dirUsers, err := ATrustQueryDirectoryUsers()
	if err != nil {
		return nil, fmt.Errorf("拉取零信任目录失败: %v", err)
	}

	// displayName → 工号集合（目录同名多人时会有多个工号）
	nameIndex := make(map[string][]string, len(dirUsers))
	for _, u := range dirUsers {
		name := strings.TrimSpace(u.DisplayName)
		if name != "" {
			nameIndex[name] = append(nameIndex[name], strings.TrimSpace(u.Name))
		}
	}

	// 目标本地账号：指定分组的启用账号（map 条件由 GORM 按方言转义保留字 `group`）
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
		DirectoryTotal: len(dirUsers),
		TargetUsers:    len(targets),
		Ambiguous:      []string{},
		Unmatched:      []string{},
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
			if u.EmployeeId == employeeId {
				report.SkippedSame++
				continue
			}
			if u.EmployeeId != "" {
				report.Overwritten++
			} else {
				report.Synced++
			}
			if err := model.DB.Model(u).Update("employee_id", employeeId).Error; err != nil {
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
		return
	}
	common.SysLog(fmt.Sprintf("[工号定时同步] 目录 %d 目标 %d 新绑定 %d 更新 %d 一致 %d 歧义 %d 未匹配 %d",
		report.DirectoryTotal, report.TargetUsers, report.Synced, report.Overwritten,
		report.SkippedSame, len(report.Ambiguous), len(report.Unmatched)))
}
