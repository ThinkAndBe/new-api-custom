package model

// model_auto_register.go — 渠道模型自动注册进模型管理。
//
// 约定（2026-08）：渠道保存时新配置的模型自动进入模型管理，占位记录带
// 默认参数（100 万输入 / 12.8 万输出），管理员用「刷新参数」从七牛模型
// 目录补全真实参数与价格。

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	// DefaultMaxInputTokens 无信息模型的默认输入上限
	DefaultMaxInputTokens = 1_000_000
	// DefaultMaxOutputTokens 无信息模型的默认输出上限
	DefaultMaxOutputTokens = 128_000
)

// EnsureModelsRegistered 把渠道上配置但模型管理缺失的模型自动注册为占位记录。
// 幂等：同名模型已存在则跳过；在传入事务内执行，失败只记日志不阻断渠道保存。
// 返回本次新注册的模型名列表。
func EnsureModelsRegistered(tx *gorm.DB, names []string) []string {
	clean := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		clean = append(clean, n)
	}
	if len(clean) == 0 {
		return nil
	}

	var existing []string
	if err := tx.Model(&Model{}).Where("model_name IN ?", clean).Pluck("model_name", &existing).Error; err != nil {
		common.SysError("自动注册模型查询失败: " + err.Error())
		return nil
	}
	exSet := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		exSet[e] = struct{}{}
	}

	now := common.GetTimestamp()
	var registered []string
	for _, n := range clean {
		if _, ok := exSet[n]; ok {
			continue
		}
		mi := &Model{
			ModelName:       n,
			Status:          1,
			MaxInputTokens:  DefaultMaxInputTokens,
			MaxOutputTokens: DefaultMaxOutputTokens,
		}
		mi.CreatedTime = now
		mi.UpdatedTime = now
		if err := tx.Create(mi).Error; err != nil {
			common.SysError("自动注册模型 " + n + " 失败: " + err.Error())
			continue
		}
		registered = append(registered, n)
	}
	return registered
}
