package model

// user_daily_limit.go — 用户每日 Token 用量限制。
//
// 计数口径：与使用日志一致（RecordConsumeLog 的 prompt+completion tokens），
// 在 RecordConsumeLog 入口累计（不受「额度消费日志」开关影响）。
// 校验点：service.NewBillingSession（钱包/订阅两条计费路径都过这里）。
// 缓存：进程内按 (userId → 当日 entry) 缓存，日切或冷启动时从 DB 回填；
// 单节点部署下内存计数即准，多节点时以 DB 为准做兜底校验。

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// UserDailyUsage 每用户每日 token 用量（按服务器本地日历日）
type UserDailyUsage struct {
	Id        int   `gorm:"primaryKey" json:"id"`
	UserId    int   `gorm:"uniqueIndex:uk_user_daily,priority:1" json:"user_id"`
	Day       int   `gorm:"uniqueIndex:uk_user_daily,priority:2" json:"day"` // YYYYMMDD（本地时区）
	Tokens    int64 `gorm:"default:0" json:"tokens"`
	Quota     int64 `gorm:"default:0" json:"quota"`
	UpdatedAt int64 `json:"updated_at"`
}

type dailyLimitEntry struct {
	mu       sync.Mutex
	day      int
	tokens   int64
	limit    int64
	loaded   bool // limit 已从 users 表读取
	dbSynced bool
}

var dailyLimitCache sync.Map // userId -> *dailyLimitEntry

func todayInt() int {
	n := time.Now()
	return n.Year()*10000 + int(n.Month())*100 + n.Day()
}

func getDailyEntry(userId int) *dailyLimitEntry {
	if v, ok := dailyLimitCache.Load(userId); ok {
		return v.(*dailyLimitEntry)
	}
	e := &dailyLimitEntry{}
	v, _ := dailyLimitCache.LoadOrStore(userId, e)
	return v.(*dailyLimitEntry)
}

// CheckUserDailyTokenLimit 校验用户今日 token 用量是否超限。
// 返回 (是否放行, 今日已用, 上限, 明日零点重置时间)。上限 0 = 不限制（恒放行）。
func CheckUserDailyTokenLimit(userId int) (bool, int64, int64, time.Time) {
	e := getDailyEntry(userId)
	e.mu.Lock()
	defer e.mu.Unlock()
	day := todayInt()
	if e.day != day || !e.dbSynced {
		// 日切或冷启动：从 DB 回填当日计数
		var row UserDailyUsage
		if err := DB.Where("user_id = ? AND day = ?", userId, day).First(&row).Error; err == nil {
			e.tokens = row.Tokens
		} else {
			e.tokens = 0
		}
		e.day = day
		e.dbSynced = true
	}
	if !e.loaded {
		var limit int64
		if err := DB.Model(&User{}).Select("daily_token_limit").Where("id = ?", userId).Scan(&limit).Error; err == nil {
			e.limit = limit
		}
		e.loaded = true
	}
	reset := time.Now().AddDate(0, 0, 1)
	reset = time.Date(reset.Year(), reset.Month(), reset.Day(), 0, 0, 0, 0, reset.Location())
	if e.limit <= 0 {
		return true, e.tokens, 0, reset
	}
	return e.tokens < e.limit, e.tokens, e.limit, reset
}

// InvalidateDailyTokenLimitCache 管理员修改用户上限后调用，让下一次校验重新读库
func InvalidateDailyTokenLimitCache(userId int) {
	if v, ok := dailyLimitCache.Load(userId); ok {
		e := v.(*dailyLimitEntry)
		e.mu.Lock()
		e.loaded = false
		e.mu.Unlock()
	}
}

// IncrUserDailyTokens 累计用户今日 token 用量（内存 + DB，DB 失败仅记日志不阻断计费）
func IncrUserDailyTokens(userId int, tokens int64, quota int64) {
	if tokens <= 0 && quota <= 0 {
		return
	}
	day := todayInt()
	e := getDailyEntry(userId)
	e.mu.Lock()
	if e.day != day || !e.dbSynced {
		var row UserDailyUsage
		if err := DB.Where("user_id = ? AND day = ?", userId, day).First(&row).Error; err == nil {
			e.tokens = row.Tokens
		} else {
			e.tokens = 0
		}
		e.day = day
		e.dbSynced = true
	}
	e.tokens += tokens
	e.mu.Unlock()

	if err := incrUserDailyTokensDB(userId, day, tokens, quota); err != nil {
		common.SysError(fmt.Sprintf("incr user daily tokens failed: user_id=%d day=%d err=%v", userId, day, err))
	}
}

// incrUserDailyTokensDB 跨库安全的 upsert：先 UPDATE，未命中再 INSERT（INSERT 冲突可忽略）
func incrUserDailyTokensDB(userId int, day int, tokens, quota int64) error {
	res := DB.Model(&UserDailyUsage{}).
		Where("user_id = ? AND day = ?", userId, day).
		Updates(map[string]interface{}{
			"tokens":     gorm.Expr("tokens + ?", tokens),
			"quota":      gorm.Expr("quota + ?", quota),
			"updated_at": time.Now().Unix(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		return nil
	}
	row := &UserDailyUsage{UserId: userId, Day: day, Tokens: tokens, Quota: quota, UpdatedAt: time.Now().Unix()}
	if err := DB.Create(row).Error; err != nil {
		// 并发下可能已被别的请求建行：再补一次 UPDATE
		res2 := DB.Model(&UserDailyUsage{}).
			Where("user_id = ? AND day = ?", userId, day).
			Updates(map[string]interface{}{
				"tokens":     gorm.Expr("tokens + ?", tokens),
				"quota":      gorm.Expr("quota + ?", quota),
				"updated_at": time.Now().Unix(),
			})
		if res2.Error == nil && res2.RowsAffected > 0 {
			return nil
		}
		return err
	}
	return nil
}
