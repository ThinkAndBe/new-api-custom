package controller

// atrust_employee_sync.go — 管理端：零信任同步（角色成员自动建号 + 工号回填 + 误建清理）。

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// SyncEmployeeIds POST /api/user/sync_employee_ids
// 零信任同步（管理端「零信任同步」按钮）：工号回填 + 角色成员自动建号
func SyncEmployeeIds(c *gin.Context) {
	fill, err := service.SyncEmployeeIdsFromATrust()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	created, err := service.SyncATrustRoleUsers()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeSystem,
		fmt.Sprintf("零信任同步：角色成员 %d，新建 %d，回填 %d，跳过 %d",
			created.RoleMembers, created.Created, fill.Synced, created.Skipped))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"role_members": created.RoleMembers,
			"created":      created.Created,
			"skipped":      created.Skipped,
			"failed":       created.Failed,
			"errors":       created.Errors,
			"backfilled":   fill.Synced,
			"overwritten":  fill.Overwritten,
			"ambiguous":    len(fill.Ambiguous),
		},
	})
}

// ListATrustOrphans GET /api/user/atrust_orphans
// 列出「孤儿账号」：本地已绑定工号、但工号不在当前角色成员中的用户。
// 用于清理早期误同步（roleIdList 语义误判）产生的非成员账号。
func ListATrustOrphans(c *gin.Context) {
	orphans, err := service.ListATrustOrphanUsers()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"users": orphans, "total": len(orphans)})
}

type orphanCleanupReq struct {
	Ids []int `json:"ids"`
}

// CleanupATrustOrphans POST /api/user/atrust_orphans/cleanup
// 删除选中的孤儿账号（软删，与用户管理删除一致）
func CleanupATrustOrphans(c *gin.Context) {
	var req orphanCleanupReq
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Ids) == 0 {
		common.ApiErrorMsg(c, "请选择要清理的账号")
		return
	}
	if len(req.Ids) > 1000 {
		common.ApiErrorMsg(c, "单次最多清理 1000 个")
		return
	}
	deleted, err := service.DeleteUsersByIds(req.Ids)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeSystem,
		fmt.Sprintf("清理零信任误建账号：删除 %d 个", deleted))
	common.ApiSuccess(c, gin.H{"deleted": deleted})
}
