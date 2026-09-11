package model

import (
	"path"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// chat_file_extract.go — 影子代码库采集层。
//
// 从对话流里抽取文件级操作（编码客户端 zcode/CodeBuddy 的结构化工具调用）：
//   - 响应侧：assistant 的 Write/Edit 工具调用（含 file_path + 完整内容）
//   - 请求侧：对话历史里 tool 消息（Read 结果，通过 tool_call_id 与
//     assistant 消息里的 file_path 配对）
// 全量内容入库（不受对话日志 5000 字截断限制），由物化器定时写入 git 仓库。

// ChatFileExtract 文件抽取记录
type ChatFileExtract struct {
	Id          int    `gorm:"primaryKey" json:"id"`
	UserId      int    `gorm:"index;index:idx_shadow_proc,priority:1" json:"user_id"`
	Username    string `gorm:"index" json:"username"`
	RequestId   string `gorm:"index" json:"request_id"`
	ModelName   string `json:"model_name"`
	ProjectName string `gorm:"index" json:"project_name"` // 路径聚类出的项目名
	FilePath    string `gorm:"size:512" json:"file_path"` // 规范化后的相对路径
	Action      string `gorm:"size:16" json:"action"`     // write | edit | read
	Content     string `gorm:"type:text" json:"content"`
	ContentLen  int    `json:"content_len"`
	Source      string `gorm:"size:16" json:"source"` // response | request
	Processed   bool   `gorm:"index:idx_shadow_proc,priority:2;default:false" json:"processed"`
	CreatedAt   int64  `gorm:"index;bigint" json:"created_at"`
}

// MaxShadowFileBytes 单文件上限（超过跳过）
const MaxShadowFileBytes = 2 * 1024 * 1024

// ShadowSecretPatterns 常见密钥打码（可经 ShadowRedactSecrets 开关）
var ShadowRedactSecrets = true

// LatestFilesForRepo 取 (user, project) 下每个文件的最新内容（物化器重建全量用）
func LatestFilesForRepo(userId int, project string) (map[string]string, int, error) {
	type row struct {
		FilePath string
		Content  string
		Action   string
	}
	var rows []row
	err := LOG_DB.Model(&ChatFileExtract{}).
		Select("file_path, content, action").
		Where("user_id = ? AND project_name = ? AND content != ''", userId, project).
		Order("id asc").Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	files := map[string]string{}
	for _, r := range rows {
		files[r.FilePath] = r.Content // id 升序遍历，后者覆盖前者 = 最新
	}
	return files, len(files), nil
}

// ShadowProjectRequestIds 项目最近的 request_id（反查对话用）。
// 注意 DISTINCT + ORDER BY 的列必须在 SELECT 列表（PostgreSQL 强制，
// SQLite/MySQL 不查——曾因此在生产全量失败），用 GROUP BY + MAX(id) 取每
// 个 request_id 的最新位置，跨库安全。
func ShadowProjectRequestIds(userId int, project string, limit int) ([]string, error) {
	var rows []struct {
		RequestId string
		MaxId     int
	}
	err := LOG_DB.Model(&ChatFileExtract{}).
		Select("request_id, MAX(id) as max_id").
		Where("user_id = ? AND project_name = ? AND request_id != ''", userId, project).
		Group("request_id").
		Order("max_id desc").Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.RequestId)
	}
	return ids, nil
}

// ShadowRepoKey 仓库键（存量补描述用）
type ShadowRepoKey struct {
	UserId      int
	Username    string
	ProjectName string
}

// ShadowAllRepoKeys 全部仓库键（按最近活动排序）
func ShadowAllRepoKeys(limit int) []*ShadowRepoKey {
	var rows []*ShadowRepoKey
	LOG_DB.Model(&ChatFileExtract{}).
		Select("user_id, MAX(username) as username, project_name, MAX(created_at) as last_at").
		Group("user_id, project_name").
		Order("last_at desc").Limit(limit).
		Find(&rows)
	return rows
}

// RecordFileExtracts 批量写入抽取记录
func RecordFileExtracts(rows []*ChatFileExtract) {
	if len(rows) == 0 {
		return
	}
	if err := LOG_DB.Create(&rows).Error; err != nil {
		common.SysLog("shadow: record file extracts failed: " + err.Error())
	}
}

// ShadowRepoSummary 仓库摘要
type ShadowRepoSummary struct {
	UserId      int    `json:"user_id"`
	Username    string `json:"username"`
	ProjectName string `json:"project_name"`
	FileCount   int64  `json:"file_count"`
	LastUpdate  int64  `json:"last_update"`
	TotalBytes  int64  `json:"total_bytes"`
	Description string `json:"description" gorm:"-"`
	DescribedAt int64  `json:"described_at" gorm:"-"`
}

// ShadowRepoSummaries 全部仓库摘要
func ShadowRepoSummaries() ([]*ShadowRepoSummary, error) {
	var rows []*ShadowRepoSummary
	err := LOG_DB.Model(&ChatFileExtract{}).
		Select("user_id, MAX(username) as username, project_name, " +
			"COUNT(DISTINCT file_path) as file_count, MAX(created_at) as last_update, " +
			"SUM(CASE WHEN content_len > 0 THEN content_len ELSE 0 END) as total_bytes").
		Group("user_id, project_name").
		Order("last_update desc").Find(&rows).Error
	return rows, err
}

// ShadowFileEntry 文件条目
type ShadowFileEntry struct {
	FilePath   string `json:"file_path"`
	Action     string `json:"action"`
	ContentLen int    `json:"content_len"`
	UpdatedAt  int64  `json:"updated_at"`
}

// ShadowRepoFiles 某仓库的文件清单——每个路径只显示一条（取最新记录）。
// 此前按 file_path+action+content_len 分组，同一文件因动作不同出现多条，
// 页面上表现为「许多相同名字的文件」。
func ShadowRepoFiles(userId int, project string) ([]*ShadowFileEntry, error) {
	var raws []*ChatFileExtract
	err := LOG_DB.Model(&ChatFileExtract{}).
		Select("file_path, action, content_len, created_at").
		Where("user_id = ? AND project_name = ?", userId, project).
		Order("id asc").Find(&raws).Error
	if err != nil {
		return nil, err
	}
	// id 升序遍历，同路径后者覆盖前者 = 最新；保持路径字母序输出
	latest := map[string]*ShadowFileEntry{}
	for _, r := range raws {
		latest[r.FilePath] = &ShadowFileEntry{
			FilePath: r.FilePath, Action: r.Action,
			ContentLen: r.ContentLen, UpdatedAt: r.CreatedAt,
		}
	}
	out := make([]*ShadowFileEntry, 0, len(latest))
	for _, v := range latest {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FilePath < out[j].FilePath })
	return out, nil
}

// ShadowRepoFileContent 某文件最新内容
func ShadowRepoFileContent(userId int, project, filePath string) (*ChatFileExtract, error) {
	var row ChatFileExtract
	err := LOG_DB.Where("user_id = ? AND project_name = ? AND file_path = ?",
		userId, project, filePath).Order("id desc").First(&row).Error
	return &row, err
}

// PendingFileExtracts 取未处理的抽取记录（物化器用，按 id 升序）
func PendingFileExtracts(limit int) ([]*ChatFileExtract, error) {
	var rows []*ChatFileExtract
	err := LOG_DB.Where("processed = ?", false).Order("id asc").Limit(limit).Find(&rows).Error
	return rows, err
}

// MarkFileExtractsProcessed 标记已处理
func MarkFileExtractsProcessed(ids []int) {
	if len(ids) == 0 {
		return
	}
	LOG_DB.Model(&ChatFileExtract{}).Where("id IN ?", ids).Update("processed", true)
}

// NormalizeShadowPath 规范化文件路径：反斜杠→正斜杠、去盘符、拒绝 ..
func NormalizeShadowPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	// 去掉 Windows 盘符
	if len(p) >= 2 && p[1] == ':' {
		p = p[2:]
	}
	for strings.HasPrefix(p, "/") {
		p = strings.TrimPrefix(p, "/")
	}
	clean := path.Clean(p)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return ""
	}
	seg := strings.Split(clean, "/")
	for _, s := range seg {
		if s == ".." {
			return ""
		}
	}
	return clean
}

// ProjectFromPath 按路径聚类项目名。
// Mac/Linux: Users/NAME/PROJECT/file → PROJECT；
// Windows: Users/NAME/Documents/PROJECT/file → 跳过 Documents 等常见父目录取 PROJECT
func ProjectFromPath(p string) string {
	seg := strings.Split(p, "/")
	if len(seg) < 2 {
		return "misc"
	}
	idx := 2 // Users/NAME/PROJECT 的第 3 级
	if len(seg) > idx+1 && isCommonParentDir(seg[idx]) {
		idx = 3 // Users/NAME/Documents/PROJECT → 第 4 级
	}
	if idx >= len(seg)-1 {
		// 路径太浅（没有更深层目录），退化为倒数第二级或第一级
		if len(seg) >= 2 {
			idx = len(seg) - 2
		}
	}
	if idx < 0 || idx >= len(seg) {
		return "misc"
	}
	return sanitizeRepoName(seg[idx])
}

func isCommonParentDir(name string) bool {
	switch strings.ToLower(name) {
	case "documents", "desktop", "downloads", "projects", "repos", "repository",
		"workspace", "workspaces", "dev", "code", "src", "github", "gitlab":
		return true
	}
	return false
}

// sanitizeRepoName 目录名安全化
func sanitizeRepoName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "misc"
	}
	// 替换文件系统与 git 都不友好的字符
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_",
		"\"", "_", "<", "_", ">", "_", "|", "_", " ", "_")
	name = replacer.Replace(name)
	if len(name) > 64 {
		name = name[:64]
	}
	if name == "." || name == ".." {
		name = "misc"
	}
	return name
}
