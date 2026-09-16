package model

// shadow_inventory.go — 影子代码库清单模式。
//
// 从「聊天提取文件自动积累」转为「工具扫描上报清单 + 管理端按需手动拉取」：
//   工具扫描 → 上报项目/技能清单（不含文件内容，skill 用途取 SKILL.md 首段）
//   管理端看清单 → 点「拉取」生成任务 → 工具轮询领任务 → 上传该项目文件
//                 （走既有 shadow_upload → 物化/描述链路）
//   不感兴趣的标记 ignored → 归入不关注视图，不再默认展示
// 技能只收个人开发的：市场/公共技能（目录带 _skillhub_meta.json）与
// 软件安装目录自带技能由工具端过滤，不上报。

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// ShadowInventoryItem 项目/技能清单项（按 user+kind+name 唯一，工具上报 upsert）
type ShadowInventoryItem struct {
	Id         int    `gorm:"primaryKey" json:"id"`
	UserId     int    `gorm:"index;uniqueIndex:idx_shadow_inv_user_name,priority:1" json:"user_id"`
	Username   string `gorm:"index" json:"username"`
	Kind       string `gorm:"size:16;uniqueIndex:idx_shadow_inv_user_name,priority:2" json:"kind"` // project | skill
	Tool       string `gorm:"size:32" json:"tool"`                                                // workbuddy | codebuddy | zcode | ...
	Name       string `gorm:"size:128;uniqueIndex:idx_shadow_inv_user_name,priority:3" json:"name"`
	LocalPath  string `gorm:"size:512" json:"local_path"`
	FileCount  int    `json:"file_count"`
	TotalBytes int64  `json:"total_bytes"`
	Purpose    string `gorm:"type:text" json:"purpose"` // 用途（skill 自动取 SKILL.md；项目可手动编辑）
	Ignored    bool   `gorm:"index" json:"ignored"`
	LastScanAt int64  `gorm:"index" json:"last_scan_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

// ShadowPullTask 手动拉取任务（工具按归属用户轮询）
type ShadowPullTask struct {
	Id          int    `gorm:"primaryKey" json:"id"`
	UserId      int    `gorm:"index" json:"user_id"`
	Username    string `json:"username"`
	InventoryId int    `gorm:"index" json:"inventory_id"`
	ItemName    string `gorm:"size:128" json:"item_name"`
	LocalPath   string `gorm:"size:512" json:"local_path"`
	Status      string `gorm:"size:16;index" json:"status"` // pending | done | failed
	Files       int    `json:"files"`                       // 上传文件数
	Note        string `gorm:"size:255" json:"note"`
	CreatedAt   int64  `json:"created_at"`
	DoneAt      int64  `json:"done_at"`
}

// UpsertShadowInventory 工具上报 upsert：同 user+kind+name 更新统计与路径，
// 保留 purpose/ignored（人工标记不被扫描覆盖）
func UpsertShadowInventory(userId int, username string, items []ShadowInventoryItem) (int, error) {
	now := time.Now().Unix()
	n := 0
	for _, it := range items {
		var exist ShadowInventoryItem
		err := LOG_DB.Where("user_id = ? AND kind = ? AND name = ?",
			userId, it.Kind, it.Name).First(&exist).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				it.UserId = userId
				it.Username = username
				it.LastScanAt = now
				it.UpdatedAt = now
				if err := LOG_DB.Create(&it).Error; err != nil {
					continue
				}
				n++
			}
			continue
		}
		updates := map[string]interface{}{
			"local_path":  it.LocalPath,
			"file_count":  it.FileCount,
			"total_bytes": it.TotalBytes,
			"tool":        it.Tool,
			"last_scan_at": now,
			"updated_at":  now,
		}
		// skill 的用途随扫描刷新（个人技能改动即更新）；项目用途保留人工编辑
		if it.Kind == "skill" && it.Purpose != "" {
			updates["purpose"] = it.Purpose
		}
		if err := LOG_DB.Model(&exist).Updates(updates).Error; err == nil {
			n++
		}
	}
	return n, nil
}

// ListShadowInventory 管理端清单查询
func ListShadowInventory(userId int, kind string, ignored bool, keyword string) ([]ShadowInventoryItem, error) {
	q := LOG_DB.Model(&ShadowInventoryItem{})
	if userId > 0 {
		q = q.Where("user_id = ?", userId)
	}
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	q = q.Where("ignored = ?", ignored)
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("name LIKE ? OR username LIKE ? OR purpose LIKE ?", like, like, like)
	}
	var items []ShadowInventoryItem
	err := q.Order("updated_at desc").Limit(500).Find(&items).Error
	return items, err
}

// SetShadowInventoryIgnored 标记/取消不关注
func SetShadowInventoryIgnored(id int, ignored bool) error {
	return LOG_DB.Model(&ShadowInventoryItem{}).Where("id = ?", id).
		Updates(map[string]interface{}{"ignored": ignored, "updated_at": time.Now().Unix()}).Error
}

// UpdateShadowInventoryPurpose 手动编辑用途
func UpdateShadowInventoryPurpose(id int, purpose string) error {
	return LOG_DB.Model(&ShadowInventoryItem{}).Where("id = ?", id).
		Updates(map[string]interface{}{"purpose": purpose, "updated_at": time.Now().Unix()}).Error
}

// CreateShadowPullTask 生成拉取任务（同项目已有 pending 不重复）
func CreateShadowPullTask(item *ShadowInventoryItem) (*ShadowPullTask, error) {
	var dup ShadowPullTask
	err := LOG_DB.Where("inventory_id = ? AND status = ?", item.Id, "pending").First(&dup).Error
	if err == nil {
		return &dup, nil
	}
	now := time.Now().Unix()
	task := &ShadowPullTask{
		UserId:      item.UserId,
		Username:    item.Username,
		InventoryId: item.Id,
		ItemName:    item.Name,
		LocalPath:   item.LocalPath,
		Status:      "pending",
		CreatedAt:   now,
	}
	if err := LOG_DB.Create(task).Error; err != nil {
		return nil, err
	}
	return task, nil
}

// PendingShadowPullTasks 工具轮询：取归属用户待执行任务（置为执行中不引入，
// 工具完成即回报；重复执行幂等——重新上传覆盖同路径内容）
func PendingShadowPullTasks(userId int, limit int) ([]ShadowPullTask, error) {
	var tasks []ShadowPullTask
	err := LOG_DB.Where("user_id = ? AND status = ?", userId, "pending").
		Order("id asc").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// FinishShadowPullTask 工具回报任务结果
func FinishShadowPullTask(userId, taskId int, ok bool, files int, note string) error {
	status := "done"
	if !ok {
		status = "failed"
	}
	updates := map[string]interface{}{"status": status, "done_at": time.Now().Unix(), "files": files}
	if note != "" {
		updates["note"] = note
	}
	return LOG_DB.Model(&ShadowPullTask{}).
		Where("id = ? AND user_id = ?", taskId, userId).Updates(updates).Error
}
