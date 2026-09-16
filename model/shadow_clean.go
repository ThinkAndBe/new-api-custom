package model

import "fmt"

// CleanShadowSediment 清理对话沉淀数据（保留工具拉取 source=upload 的内容）：
//   1. 删除 chat_file_extracts 中 source != 'upload' 的行
//   2. 找出没有任何 upload 行的 (user, project)，删除其项目元数据
//   3. 返回被清理的项目清单（调用方据此删除对应物化仓库目录）
func CleanShadowSediment() (rowsDeleted int64, projects []ShadowRepoSummary, err error) {
	// 先取全量项目汇总（含 upload 与非 upload）
	var all []ShadowRepoSummary
	if err = LOG_DB.Model(&ChatFileExtract{}).
		Select("user_id, MAX(username) as username, project_name, MAX(created_at) as last_update").
		Group("user_id, project_name").Find(&all).Error; err != nil {
		return 0, nil, err
	}
	// 有 upload 内容的项目集合
	// 字段名必须与列名蛇形对应（ProjectName→project_name）。
	// 首版误用 Project 映射失败，keep 集合全空导致拉取项目被误判为沉淀。
	type up struct {
		UserId      int
		ProjectName string
	}
	var ups []up
	if err = LOG_DB.Model(&ChatFileExtract{}).
		Select("user_id, project_name").Where("source = ?", "upload").
		Group("user_id, project_name").Find(&ups).Error; err != nil {
		return 0, nil, err
	}
	keep := map[string]bool{}
	for _, u := range ups {
		keep[fmt.Sprintf("%d|%s", u.UserId, u.ProjectName)] = true
	}
	for _, r := range all {
		if !keep[fmt.Sprintf("%d|%s", r.UserId, r.ProjectName)] {
			projects = append(projects, r)
		}
	}

	// 删非 upload 行
	res := LOG_DB.Where("source != ?", "upload").Delete(&ChatFileExtract{})
	if res.Error != nil {
		return 0, projects, res.Error
	}
	rowsDeleted = res.RowsAffected

	// 删纯沉淀项目的元数据（描述）
	for _, r := range projects {
		DB.Where("user_id = ? AND project_name = ?", r.UserId, r.ProjectName).
			Delete(&ShadowProject{})
	}
	return rowsDeleted, projects, nil
}
// ShadowProjectHasUpload 该 (user, project) 是否存在工具拉取内容（删除目录前的双保险）
func ShadowProjectHasUpload(userId int, project string) bool {
	var n int64
	LOG_DB.Model(&ChatFileExtract{}).
		Where("user_id = ? AND project_name = ? AND source = ?", userId, project, "upload").
		Count(&n)
	return n > 0
}

