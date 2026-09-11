package controller

// shadow_upload.go — 影子代码库主动上传：整项目打包直接留存。
//
// AI 巡检受模型配合度与上下文窗口限制，无法保证 100% 完整。此端点提供
// 可靠路径：用户侧脚本/工具把项目打成 zip 上传，直接进入影子库（物化、
// 描述生成与 AI 捕获共用一套）。
//
// POST /api/shadow/upload?project=NAME  (multipart file=xxx.zip)
// 鉴权：普通用户令牌（写入自己的影子库）；超管可带 user_id 指定他人。

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// UploadShadowProject POST /api/shadow/upload
func UploadShadowProject(c *gin.Context) {
	userId := common.GetContextKeyInt(c, constant.ContextKeyUserId)
	username := common.GetContextKeyString(c, constant.ContextKeyUserName)
	// 超管可指定目标用户
	if target := c.Query("user_id"); target != "" && c.GetInt("role") >= common.RoleAdminUser {
		if tid := parseIntDefault(target, 0); tid > 0 {
			userId = tid
			var u model.User
			if err := model.DB.Select("username").First(&u, "id = ?", tid).Error; err == nil {
				username = u.Username
			}
		}
	}
	project := strings.TrimSpace(c.Query("project"))
	if project == "" {
		common.ApiErrorMsg(c, "project 参数必填（项目名）")
		return
	}
	if !common.ShadowRepoEnabled {
		common.ApiErrorMsg(c, "影子代码库未启用")
		return
	}

	fh, err := c.FormFile("file")
	if err != nil {
		common.ApiErrorMsg(c, "file 字段必填（zip 包）")
		return
	}
	if fh.Size > 200*1024*1024 {
		common.ApiErrorMsg(c, "zip 包超过 200MB 上限")
		return
	}
	f, err := fh.Open()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	defer f.Close()

	raw, err := io.ReadAll(io.LimitReader(f, 201*1024*1024))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		common.ApiErrorMsg(c, "无效的 zip 包: "+err.Error())
		return
	}

	rows := []*model.ChatFileExtract{}
	now := common.GetTimestamp()
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		name := model.NormalizeShadowPath(zf.Name)
		if name == "" || model.IsShadowExcludedPath(name) {
			continue
		}
		if zf.UncompressedSize64 > uint64(model.MaxShadowFileBytes) {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(rc, model.MaxShadowFileBytes+1))
		rc.Close()
		if err != nil || len(data) > model.MaxShadowFileBytes {
			continue
		}
		rows = append(rows, &model.ChatFileExtract{
			UserId: userId, Username: username,
			RequestId:   "upload-" + common.GetUUID(),
			ModelName:   "manual-upload",
			ProjectName: model.SanitizeProjectName(project),
			FilePath:    name,
			Action:      "write",
			Content:     string(data),
			ContentLen:  len(data),
			Source:      "upload",
			CreatedAt:   now,
		})
	}
	model.RecordFileExtracts(rows)
	// 立即物化 + 描述
	go service.ProcessShadowExtracts()
	model.UpsertShadowProjectMeta(userId, project)
	service.MaybeDescribeProject(userId, username, project)

	common.ApiSuccess(c, gin.H{
		"files":   len(rows),
		"message": fmt.Sprintf("已接收 %d 个文件，正在物化", len(rows)),
	})
}

// seekWrap 把 multipart File 适配成 io.ReaderAt（zip 需要）
type seekWrap struct{ r io.Reader }

func (s *seekWrap) Read(p []byte) (int, error) { return s.r.Read(p) }
func (s *seekWrap) ReadAt(p []byte, off int64) (int, error) {
	// multipart 文件不支持随机读；这里依赖 zip.Reader 顺序遍历的特性
	// ——zip.Reader 需要 ReaderAt，改用一次性读入内存的方式在调用侧处理
	return 0, fmt.Errorf("not supported")
}

func parseIntDefault(s string, def int) int {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return def
		}
		n = n*10 + int(ch-'0')
	}
	return n
}
