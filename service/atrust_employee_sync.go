package service

// atrust_employee_sync.go — 从零信任在线用户批量回填本地用户工号。
//
// 用 aTrust OpenAPI（getUserStatus，含 name 工号 + displayName 姓名）按姓名
// 匹配本地账号并写入 employee_id。仅回填不建号：同名歧义跳过并报告，
// 未匹配的报告（这些用户首次零信任登录时会走 SSO 匹配/建号流程）。

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// ATrustSyncReport 工号同步结果报告
type ATrustSyncReport struct {
	OnlineTotal  int      `json:"online_total"`   // aTrust 在线用户数（去重后）
	Synced       int      `json:"synced"`         // 本次新写入工号
	Overwritten  int      `json:"overwritten"`    // 工号被更新（与原值不同）
	SkippedSame  int      `json:"skipped_same"`   // 已绑定且一致，跳过
	Ambiguous    []string `json:"ambiguous"`      // 同名歧义未处理：姓名(工号)
	Unmatched    []string `json:"unmatched"`      // 零信任在线但本地无账号：姓名(工号)
	NoEmployeeId []string `json:"no_employee_id"` // 零信任侧缺工号：姓名
}

// SyncEmployeeIdsFromATrust 执行一次工号批量同步
func SyncEmployeeIdsFromATrust() (*ATrustSyncReport, error) {
	online, err := ATrustFetchOnlineUsers()
	if err != nil {
		return nil, fmt.Errorf("拉取零信任在线用户失败: %v", err)
	}

	report := &ATrustSyncReport{
		Ambiguous:    []string{},
		Unmatched:    []string{},
		NoEmployeeId: []string{},
	}

	// 去重（同一人多个会话会产生多条）
	seen := make(map[string]bool)

	for i := range online {
		u := &online[i]
		displayName := strings.TrimSpace(u.DisplayName)
		employeeId := strings.TrimSpace(u.Name)
		key := employeeId + "|" + displayName
		if seen[key] {
			continue
		}
		seen[key] = true
		report.OnlineTotal++

		if employeeId == "" {
			if displayName != "" {
				report.NoEmployeeId = append(report.NoEmployeeId, displayName)
			}
			continue
		}

		// 已按工号绑定 → 一致跳过
		var bound model.User
		if err := model.DB.Where("employee_id = ?", employeeId).First(&bound).Error; err == nil {
			report.SkippedSame++
			continue
		}

		// 姓名匹配（username 或 display_name），同名多人跳过
		if displayName == "" {
			report.Unmatched = append(report.Unmatched, "("+employeeId+")")
			continue
		}
		var ms []model.User
		if err := model.DB.Where(
			"username = ? OR display_name = ?", displayName, displayName,
		).Find(&ms).Error; err != nil {
			continue
		}
		switch len(ms) {
		case 0:
			report.Unmatched = append(report.Unmatched, displayName+"("+employeeId+")")
		case 1:
			m := &ms[0]
			if m.Status != common.UserStatusEnabled {
				report.Ambiguous = append(report.Ambiguous, displayName+"("+employeeId+")→ 账号已禁用")
				continue
			}
			if m.EmployeeId != "" {
				// 该账号已绑了别的工号：以零信任为准更新并记录
				report.Overwritten++
			} else {
				report.Synced++
			}
			if err := model.DB.Model(m).Update("employee_id", employeeId).Error; err != nil {
				common.SysError("[工号同步] 写入失败 " + m.Username + ": " + err.Error())
			} else {
				common.SysLog("[工号同步] " + m.Username + " ← " + displayName + "(" + employeeId + ")")
			}
		default:
			report.Ambiguous = append(report.Ambiguous, displayName+"("+employeeId+")")
		}
	}
	return report, nil
}
