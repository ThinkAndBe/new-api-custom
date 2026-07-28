package service

import (
	"strings"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func GetUserUsableGroups(userGroup string) map[string]string {
	groupsCopy := setting.GetUserUsableGroupsCopy()
	if userGroup != "" {
		specialSettings, b := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Get(userGroup)
		if b {
			// 处理特殊可用分组
			for specialGroup, desc := range specialSettings {
				if strings.HasPrefix(specialGroup, "-:") {
					// 移除分组
					groupToRemove := strings.TrimPrefix(specialGroup, "-:")
					delete(groupsCopy, groupToRemove)
				} else if strings.HasPrefix(specialGroup, "+:") {
					// 添加分组
					groupToAdd := strings.TrimPrefix(specialGroup, "+:")
					groupsCopy[groupToAdd] = desc
				} else {
					// 直接添加分组
					groupsCopy[specialGroup] = desc
				}
			}
		}
		// 如果userGroup不在UserUsableGroups中，返回UserUsableGroups + userGroup
		if _, ok := groupsCopy[userGroup]; !ok {
			groupsCopy[userGroup] = "用户分组"
		}
	}
	return groupsCopy
}

func GroupInUserUsableGroups(userGroup, groupName string) bool {
	_, ok := GetUserUsableGroups(userGroup)[groupName]
	return ok
}

// GetUserOwnedGroups 返回用户"自身权限所属"的分组集合（不包含管理员开放的"用户可选分组"）。
// 用于使用教程、模型广场等展示场景：普通用户只应看到自己分组实际可用的模型，
// 避免把其他可选分组（如 svip）的模型也暴露出来。
// 若配置了 GroupSpecialUsableGroup 特殊规则（+: 追加 / -: 移除），同样作用于自身分组集合。
func GetUserOwnedGroups(userGroup string) map[string]string {
	groups := map[string]string{}
	if userGroup == "" {
		// 匿名/未设置分组的用户只回落到 default 分组
		groups["default"] = setting.GetUsableGroupDescription("default")
		return groups
	}
	groups[userGroup] = setting.GetUsableGroupDescription(userGroup)
	// 应用特殊可用分组规则（仅 +:/直接添加 和 -:移除 生效于自身分组集合）
	if specialSettings, ok := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Get(userGroup); ok {
		for specialGroup, desc := range specialSettings {
			if strings.HasPrefix(specialGroup, "-:") {
				delete(groups, strings.TrimPrefix(specialGroup, "-:"))
			} else if strings.HasPrefix(specialGroup, "+:") {
				groups[strings.TrimPrefix(specialGroup, "+:")] = desc
			} else {
				groups[specialGroup] = desc
			}
		}
	}
	// 兜底：特殊规则把自身分组也移除时，至少保留自身分组，避免空集合
	if len(groups) == 0 {
		groups[userGroup] = "用户分组"
	}
	return groups
}

// GetUserAutoGroup 根据用户分组获取自动分组设置
func GetUserAutoGroup(userGroup string) []string {
	groups := GetUserUsableGroups(userGroup)
	autoGroups := make([]string, 0)
	for _, group := range setting.GetAutoGroups() {
		if _, ok := groups[group]; ok {
			autoGroups = append(autoGroups, group)
		}
	}
	return autoGroups
}

// GetUserGroupRatio 获取用户使用某个分组的倍率
// userGroup 用户分组
// group 需要获取倍率的分组
func GetUserGroupRatio(userGroup, group string) float64 {
	ratio, ok := ratio_setting.GetGroupGroupRatio(userGroup, group)
	if ok {
		return ratio
	}
	return ratio_setting.GetGroupRatio(group)
}
