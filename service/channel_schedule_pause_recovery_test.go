package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
)

// 测试定时暂停渠道的恢复时间计算逻辑。
// 重点验证：跨天规则、当天规则、未命中、非法规则。
func TestNextSchedulePauseRecovery(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	// 固定基准时刻：周三 22:30（用 UTC-0 的时刻，下面统一在 loc 时区下计算）
	base := time.Date(2026, 7, 29, 14, 30, 0, 0, time.UTC) // UTC 14:30 = +8 22:30 周三
	base = base.In(loc)

	cases := []struct {
		name string
		now  time.Time
		rule dto.SchedulePauseRule
		want int64 // 期望恢复时间戳，0 表示无解
	}{
		{
			name: "当天规则_22-次日2点_当前22:30_应恢复到次日02:00",
			now:  base,
			rule: dto.SchedulePauseRule{Days: []int{3}, Start: "22:00", End: "02:00"},
			want: time.Date(2026, 7, 30, 2, 0, 0, 0, loc).Unix(),
		},
		{
			name: "当天规则_9-23点_当前22:30_应恢复到当天23:00",
			now:  base,
			rule: dto.SchedulePauseRule{Days: []int{3}, Start: "9:00", End: "23:00"},
			want: time.Date(2026, 7, 29, 23, 0, 0, 0, loc).Unix(),
		},
		{
			name: "未命中_规则是周一周二_当前周三_应返回0",
			now:  base,
			rule: dto.SchedulePauseRule{Days: []int{1, 2}, Start: "9:00", End: "18:00"},
			want: 0,
		},
		{
			name: "非法规则_start空_应返回0",
			now:  base,
			rule: dto.SchedulePauseRule{Days: []int{3}, Start: "", End: "23:00"},
			want: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NextSchedulePauseRecovery(c.now, []dto.SchedulePauseRule{c.rule})
			if got != c.want {
				t.Errorf("got=%d want=%d\n  got=%v\n want=%v",
					got, c.want, time.Unix(got, 0).In(loc), time.Unix(c.want, 0).In(loc))
			}
		})
	}
}
