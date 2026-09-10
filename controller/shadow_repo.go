package controller

// shadow_repo.go — 影子代码库管理端 API（仅超管）。
//
// 仓库由物化器生成在 {SHADOW_REPO_DIR}/{用户}/{项目}.git；本 API 从
// chat_file_extracts 的最新状态（与物化内容一致）读数据，避免解析 git 对象。

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// ListShadowRepos GET /api/shadow/repos
func ListShadowRepos(c *gin.Context) {
	repos, err := model.ShadowRepoSummaries()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 合并项目功能描述
	for _, r := range repos {
		if meta, err := model.GetShadowProject(r.UserId, r.ProjectName); err == nil {
			r.Description = meta.Description
			r.DescribedAt = meta.DescribedAt
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": repos,
		"base_dir": service.ShadowRepoBaseDir()})
}

// RedescribeShadowProject POST /api/shadow/redescribe?user_id=&project=
func RedescribeShadowProject(c *gin.Context) {
	userId, project := shadowParams(c)
	if userId == 0 || project == "" {
		common.ApiErrorMsg(c, "user_id 与 project 必填")
		return
	}
	var username string
	model.DB.Table("users").Where("id = ?", userId).Pluck("username", &username)
	go func() {
		if err := service.RedescribeProject(userId, username, project); err != nil {
			common.SysLog("shadow: redescribe failed: " + err.Error())
		}
	}()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已触发重新生成，稍后刷新查看"})
}

// GetShadowTree GET /api/shadow/tree?user_id=&project=
func GetShadowTree(c *gin.Context) {
	userId, project := shadowParams(c)
	if userId == 0 || project == "" {
		common.ApiErrorMsg(c, "user_id 与 project 必填")
		return
	}
	files, err := model.ShadowRepoFiles(userId, project)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": files})
}

// GetShadowFile GET /api/shadow/file?user_id=&project=&path=
func GetShadowFile(c *gin.Context) {
	userId, project := shadowParams(c)
	filePath := c.Query("path")
	if userId == 0 || project == "" || filePath == "" {
		common.ApiErrorMsg(c, "user_id、project、path 必填")
		return
	}
	row, err := model.ShadowRepoFileContent(userId, project, filePath)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
}

// TriggerShadowSync POST /api/shadow/sync 手动触发一轮物化
func TriggerShadowSync(c *gin.Context) {
	go service.ProcessShadowExtracts()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已触发物化，稍后刷新查看"})
}

func shadowParams(c *gin.Context) (int, string) {
	userId, _ := strconv.Atoi(c.Query("user_id"))
	return userId, c.Query("project")
}
