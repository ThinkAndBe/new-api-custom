package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

func mkLimitUser(t *testing.T, id int, limit int64) {
	t.Helper()
	u := &model.User{Id: id, Username: limitName(id), Password: "pwd12345", DisplayName: limitName(id),
		AffCode: limitName(id), Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", DailyTokenLimit: limit}
	if err := model.DB.Create(u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
}

func limitName(id int) string {
	return "limit-u-" + time.Now().Format("150405") + "-" + string(rune('a'+id%26)) + "-" + string(rune('0'+id%10))
}

// 限额生效：到量拒绝、次日口径、改限即失效缓存、0=不限
func TestUserDailyTokenLimit(t *testing.T) {
	mkLimitUser(t, 701, 1000)

	// 初始放行
	if ok, used, limit, _ := model.CheckUserDailyTokenLimit(701); !ok || used != 0 || limit != 1000 {
		t.Fatalf("初始应放行 used=0 limit=1000，实际 ok=%v used=%d limit=%d", ok, used, limit)
	}
	// 累计 999 仍放行
	model.IncrUserDailyTokens(701, 600, 0)
	model.IncrUserDailyTokens(701, 399, 0)
	if ok, used, _, _ := model.CheckUserDailyTokenLimit(701); !ok || used != 999 {
		t.Fatalf("999 应放行，实际 ok=%v used=%d", ok, used)
	}
	// 到 1000 → 拒绝
	model.IncrUserDailyTokens(701, 1, 0)
	if ok, used, _, reset := model.CheckUserDailyTokenLimit(701); ok || used != 1000 {
		t.Fatalf("1000 应拒绝，实际 ok=%v used=%d", ok, used)
	} else if !reset.After(time.Now()) {
		t.Fatalf("重置时间应在未来")
	}

	// DB 落库校验（缓存之外的数据真相）
	day := time.Now().Year()*10000 + int(time.Now().Month())*100 + time.Now().Day()
	var row model.UserDailyUsage
	if err := model.DB.Where("user_id = ? AND day = ?", 701, day).First(&row).Error; err != nil {
		t.Fatalf("日用量行未落库: %v", err)
	}
	if row.Tokens != 1000 {
		t.Fatalf("DB tokens=%d，期望 1000", row.Tokens)
	}

	// 管理员改限 → 缓存失效 → 放行
	model.InvalidateDailyTokenLimitCache(701)
	model.DB.Model(&model.User{}).Where("id = ?", 701).Update("daily_token_limit", 2000)
	if ok, _, limit, _ := model.CheckUserDailyTokenLimit(701); !ok || limit != 2000 {
		t.Fatalf("改限后应放行且 limit=2000，实际 ok=%v limit=%d", ok, limit)
	}

	// 0 = 不限（已超也放行）
	mkLimitUser(t, 702, 0)
	model.IncrUserDailyTokens(702, 99999, 0)
	if ok, _, limit, _ := model.CheckUserDailyTokenLimit(702); !ok || limit != 0 {
		t.Fatalf("0 应恒放行，实际 ok=%v limit=%d", ok, limit)
	}
}

// 消费日志入口 → 每日计数联动（口径=输入+输出）
func TestRecordConsumeLogIncrDaily(t *testing.T) {
	mkLimitUser(t, 703, 50000)
	common.LogConsumeEnabled = true
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	model.RecordConsumeLog(ctx, 703, model.RecordConsumeLogParams{
		PromptTokens: 12000, CompletionTokens: 3000, Quota: 150000,
		ModelName: "glm-5.3", Content: "t",
	})
	model.RecordConsumeLog(ctx, 703, model.RecordConsumeLogParams{
		PromptTokens: 8000, CompletionTokens: 2000, Quota: 100000,
		ModelName: "glm-5.3", Content: "t",
	})
	if ok, used, _, _ := model.CheckUserDailyTokenLimit(703); !ok || used != 25000 {
		t.Fatalf("两次消费应累计 25000，实际 used=%d ok=%v", used, ok)
	}
	// 关闭消费日志开关，计数仍应累计（限额不依赖日志开关）
	common.LogConsumeEnabled = false
	model.RecordConsumeLog(ctx, 703, model.RecordConsumeLogParams{
		PromptTokens: 30000, CompletionTokens: 0, Quota: 0,
		ModelName: "glm-5.3", Content: "t",
	})
	common.LogConsumeEnabled = true
	if ok, used, _, _ := model.CheckUserDailyTokenLimit(703); ok || used != 55000 {
		t.Fatalf("日志关闭时计数也应累计：used=%d（期望 55000 且应超限拒绝）ok=%v", used, ok)
	}
}
