package controller

// shadow_inventory.go — 影子库清单模式接口。
//
// 工具侧（令牌鉴权，/api/usage/*）：
//   POST /shadow_inventory      扫描上报清单（不含文件内容）
//   GET  /shadow_tasks          轮询待执行拉取任务
//   POST /shadow_task_done      回报任务结果
// 管理侧（RootAuth，/api/shadow/*）：
//   GET  /inventory             清单查询（用户/类型/不关注/关键字）
//   POST /inventory/ignore      标记/取消不关注
//   POST /inventory/pull        生成拉取任务
//   PUT  /inventory/purpose     编辑用途

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// ---- 工具侧 ----

type shadowInventoryReportItem struct {
	Kind       string `json:"kind"` // project | skill
	Tool       string `json:"tool"`
	Name       string `json:"name"`
	LocalPath  string `json:"local_path"`
	FileCount  int    `json:"file_count"`
	TotalBytes int64  `json:"total_bytes"`
	Purpose    string `json:"purpose"` // skill: SKILL.md 首段
}

type shadowInventoryReportReq struct {
	Items []shadowInventoryReportItem `json:"items"`
}

// ReportShadowInventory POST /api/usage/shadow_inventory
func ReportShadowInventory(c *gin.Context) {
	if !common.ShadowRepoEnabled {
		common.ApiErrorMsg(c, "影子代码库未启用")
		return
	}
	userId := c.GetInt("id")
	userCache, err := model.GetUserCache(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req shadowInventoryReportReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]model.ShadowInventoryItem, 0, len(req.Items))
	for _, it := range req.Items {
		if it.Name == "" || it.LocalPath == "" {
			continue
		}
		items = append(items, model.ShadowInventoryItem{
			Kind: it.Kind, Tool: it.Tool, Name: it.Name,
			LocalPath: it.LocalPath, FileCount: it.FileCount,
			TotalBytes: it.TotalBytes, Purpose: it.Purpose,
		})
	}
	n, err := model.UpsertShadowInventory(userId, userCache.Username, items)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"updated": n})
}

// PollShadowTasks GET /api/usage/shadow_tasks
func PollShadowTasks(c *gin.Context) {
	tasks, err := model.PendingShadowPullTasks(c.GetInt("id"), 10)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"tasks": tasks})
}

type shadowTaskDoneReq struct {
	TaskId int    `json:"task_id"`
	Ok     bool   `json:"ok"`
	Files  int    `json:"files"`
	Note   string `json:"note"`
}

// DoneShadowTask POST /api/usage/shadow_task_done
func DoneShadowTask(c *gin.Context) {
	var req shadowTaskDoneReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.FinishShadowPullTask(c.GetInt("id"), req.TaskId, req.Ok, req.Files, req.Note); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// ---- 管理侧 ----

// ListShadowInventory GET /api/shadow/inventory
func ListShadowInventory(c *gin.Context) {
	userId, _ := strconv.Atoi(c.Query("user_id"))
	kind := c.Query("kind")
	ignored := c.Query("ignored") == "true" || c.Query("ignored") == "1"
	items, err := model.ListShadowInventory(userId, kind, ignored, c.Query("keyword"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, items)
}

type shadowInventoryOpReq struct {
	Id      int    `json:"id"`
	Ignored bool   `json:"ignored"`
	Purpose string `json:"purpose"`
}

// IgnoreShadowInventory POST /api/shadow/inventory/ignore
func IgnoreShadowInventory(c *gin.Context) {
	var req shadowInventoryOpReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Id == 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := model.SetShadowInventoryIgnored(req.Id, req.Ignored); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// PullShadowInventory POST /api/shadow/inventory/pull —— 生成拉取任务，
// 等常驻工具轮询执行后文件经 shadow_upload 进入影子库
func PullShadowInventory(c *gin.Context) {
	var req shadowInventoryOpReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Id == 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var item model.ShadowInventoryItem
	if err := model.LOG_DB.First(&item, "id = ?", req.Id).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	task, err := model.CreateShadowPullTask(&item)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"task_id": task.Id, "message": "已下发拉取任务，等待用户工具执行"})
}

// UpdateShadowInventoryPurpose PUT /api/shadow/inventory/purpose
func UpdateShadowInventoryPurpose(c *gin.Context) {
	var req shadowInventoryOpReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Id == 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := model.UpdateShadowInventoryPurpose(req.Id, req.Purpose); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
