package model

import (
	"github.com/QuantumNous/new-api/common"
)

// shadow_project.go — 影子代码库项目元数据：归属与功能描述。

// ShadowProject 项目元数据（描述由内部 LLM 调用生成，24h 自动刷新）
type ShadowProject struct {
	Id          int    `gorm:"primaryKey" json:"id"`
	UserId      int    `gorm:"uniqueIndex:uk_shadow_proj,priority:1" json:"user_id"`
	ProjectName string `gorm:"uniqueIndex:uk_shadow_proj,priority:2;size:128" json:"project_name"`
	Description string `gorm:"type:text" json:"description"`
	DescribedAt int64  `json:"described_at"` // 描述生成时间（水位）
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// GetShadowProject 取项目元数据
func GetShadowProject(userId int, project string) (*ShadowProject, error) {
	var row ShadowProject
	err := DB.Where("user_id = ? AND project_name = ?", userId, project).First(&row).Error
	return &row, err
}

// UpsertShadowProjectMeta 更新/插入（不动 Description）
func UpsertShadowProjectMeta(userId int, project string) {
	now := common.GetTimestamp()
	var row ShadowProject
	if err := DB.Where("user_id = ? AND project_name = ?", userId, project).First(&row).Error; err == nil {
		DB.Model(&row).Updates(map[string]interface{}{"updated_at": now})
		return
	}
	DB.Create(&ShadowProject{
		UserId: userId, ProjectName: project, CreatedAt: now, UpdatedAt: now,
	})
}

// SaveShadowProjectDescription 保存生成的描述
func SaveShadowProjectDescription(userId int, project, description string) {
	now := common.GetTimestamp()
	var row ShadowProject
	if err := DB.Where("user_id = ? AND project_name = ?", userId, project).First(&row).Error; err == nil {
		DB.Model(&row).Updates(map[string]interface{}{
			"description": description, "described_at": now, "updated_at": now,
		})
		return
	}
	DB.Create(&ShadowProject{
		UserId: userId, ProjectName: project,
		Description: description, DescribedAt: now, CreatedAt: now, UpdatedAt: now,
	})
}

// ShadowProjectParticipants 同名项目的参与用户（归属视图）
func ShadowProjectParticipants(project string) []string {
	var names []string
	LOG_DB.Raw(`SELECT DISTINCT username FROM chat_file_extracts WHERE project_name = ?`, project).Scan(&names)
	return names
}
