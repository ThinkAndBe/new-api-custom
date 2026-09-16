package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

// 按恢复时间自动恢复：到点即恢复（不看禁用原因关键词、不受单渠道探活开关影响），
// 未到点不恢复、无恢复时间不恢复、手动禁用不恢复、暂停窗口内不恢复。
func TestRecoverChannelsByRecoveryAt(t *testing.T) {
	now := time.Now()
	wd := int(now.Weekday())

	type tc struct {
		id        int
		name      string
		status    int
		recovery  int64
		reason    string
		settings  dto.ChannelOtherSettings
		wantAfter int // 期望恢复后的状态
	}
	pauseRules := []dto.SchedulePauseRule{{Days: []int{wd}, Start: "00:00", End: "23:59", Reason: "tmp"}}
	cases := []tc{
		{id: 921, name: "到点-探活已关", status: common.ChannelStatusAutoDisabled, recovery: now.Add(-time.Minute).Unix(),
			reason: "status_code=429, 已达到 5 小时使用上限，2099-01-01 00:00:00 后可继续使用",
			settings: dto.ChannelOtherSettings{HealthCheckDisabled: true}, wantAfter: common.ChannelStatusEnabled},
		{id: 922, name: "到点-原因无关键词", status: common.ChannelStatusAutoDisabled, recovery: now.Add(-time.Minute).Unix(),
			reason: "上游连接被重置", settings: dto.ChannelOtherSettings{}, wantAfter: common.ChannelStatusEnabled},
		{id: 923, name: "未到点", status: common.ChannelStatusAutoDisabled, recovery: now.Add(time.Hour).Unix(),
			reason: "429 quota", settings: dto.ChannelOtherSettings{}, wantAfter: common.ChannelStatusAutoDisabled},
		{id: 924, name: "无恢复时间", status: common.ChannelStatusAutoDisabled, recovery: 0,
			reason: "429 quota", settings: dto.ChannelOtherSettings{}, wantAfter: common.ChannelStatusAutoDisabled},
		{id: 925, name: "手动禁用", status: common.ChannelStatusManuallyDisabled, recovery: now.Add(-time.Minute).Unix(),
			reason: "手动", settings: dto.ChannelOtherSettings{}, wantAfter: common.ChannelStatusManuallyDisabled},
		{id: 926, name: "暂停窗口内到点", status: common.ChannelStatusAutoDisabled, recovery: now.Add(-time.Minute).Unix(),
			reason: "429 quota", settings: dto.ChannelOtherSettings{SchedulePauseEnabled: true, SchedulePauseRules: pauseRules},
			wantAfter: common.ChannelStatusAutoDisabled},
	}

	for _, c := range cases {
		ch := &model.Channel{Id: c.id, Type: 1, Key: "sk-x", Status: c.status, Name: c.name,
			Models: "gpt-4o", Group: "default", CreatedTime: now.Unix()}
		info := map[string]interface{}{"status_reason": c.reason, "status_time": now.Unix()}
		if c.recovery > 0 {
			info["recovery_at"] = c.recovery
		}
		ch.SetOtherInfo(info)
		ch.SetOtherSettings(c.settings)
		if err := model.DB.Create(ch).Error; err != nil {
			t.Fatalf("insert %d: %v", c.id, err)
		}
	}

	RecoverChannelsByRecoveryAt()

	for _, c := range cases {
		got, err := model.GetChannelById(c.id, true)
		if err != nil {
			t.Fatalf("get %d: %v", c.id, err)
		}
		if got.Status != c.wantAfter {
			t.Errorf("%s(#%d): status=%d, 期望 %d", c.name, c.id, got.Status, c.wantAfter)
		}
		t.Logf("%s(#%d) -> status=%d ✓", c.name, c.id, got.Status)
	}

	// 到点恢复的渠道应清掉恢复时间，避免下一次扫描重复处理
	if inv := ChannelRecoveryAt(mustGet(t, 921)); inv != 0 {
		t.Errorf("恢复后 recovery_at 应被清除，实际=%d", inv)
	}

	for _, c := range cases {
		model.DB.Unscoped().Where("id = ?", c.id).Delete(&model.Channel{})
	}
	fmt.Println("done")
}

func mustGet(t *testing.T, id int) *model.Channel {
	t.Helper()
	ch, err := model.GetChannelById(id, true)
	if err != nil {
		t.Fatalf("get %d: %v", id, err)
	}
	return ch
}
