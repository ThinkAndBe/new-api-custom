package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

func filterPricingByUsableGroups(pricing []model.Pricing, usableGroup map[string]string) []model.Pricing {
	if len(pricing) == 0 {
		return pricing
	}
	if len(usableGroup) == 0 {
		return []model.Pricing{}
	}

	filtered := make([]model.Pricing, 0, len(pricing))
	for _, item := range pricing {
		if common.StringsContains(item.EnableGroup, "all") {
			filtered = append(filtered, item)
			continue
		}
		for _, group := range item.EnableGroup {
			if _, ok := usableGroup[group]; ok {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered
}

func GetPricing(c *gin.Context) {
	pricing := model.GetPricing()
	userId, exists := c.Get("id")
	usableGroup := map[string]string{}
	groupRatio := map[string]float64{}
	for s, f := range ratio_setting.GetGroupRatioCopy() {
		groupRatio[s] = f
	}
	var group string
	isAdmin := false
	if exists {
		uid := userId.(int)
		isAdmin = model.IsAdmin(uid)
		user, err := model.GetUserCache(uid)
		if err == nil {
			group = user.Group
			for g := range groupRatio {
				ratio, ok := ratio_setting.GetGroupGroupRatio(group, g)
				if ok {
					groupRatio[g] = ratio
				}
			}
		}
	}

	// 模型可见性按权限区分：
	// - 管理员：保持现状，看到所有"用户可选分组"的模型（便于排查）
	// - 普通登录用户：只看自身分组实际可用的模型，避免泄露 svip 等其它分组的模型
	// - 匿名访客：只回落到 default 分组
	if isAdmin {
		usableGroup = service.GetUserUsableGroups(group)
	} else if exists {
		usableGroup = service.GetUserOwnedGroups(group)
	} else {
		usableGroup = service.GetUserOwnedGroups("")
	}
	pricing = filterPricingByUsableGroups(pricing, usableGroup)
	// check groupRatio contains usableGroup
	for group := range ratio_setting.GetGroupRatioCopy() {
		if _, ok := usableGroup[group]; !ok {
			delete(groupRatio, group)
		}
	}

	// 补充能力参数 + 可用状态（与使用教程同一数据源：models 表 + abilities 表）
	availableSet := model.GetAvailableModelSet()
	paramsMap := model.GetModelParamsMap()
	for i := range pricing {
		pricing[i].Available = availableSet[pricing[i].ModelName]
		if p, ok := paramsMap[pricing[i].ModelName]; ok {
			pricing[i].MaxInputTokens = p.MaxInputTokens
			pricing[i].MaxOutputTokens = p.MaxOutputTokens
			pricing[i].SupportsToolCall = p.SupportsToolCall
			pricing[i].SupportsImages = p.SupportsImages
			pricing[i].SupportsReasoning = p.SupportsReasoning
		}
	}

	c.JSON(200, gin.H{
		"success":            true,
		"data":               pricing,
		"vendors":            model.GetVendors(),
		"group_ratio":        groupRatio,
		"usable_group":       usableGroup,
		"supported_endpoint": model.GetSupportedEndpointMap(),
		"auto_groups":        service.GetUserAutoGroup(group),
		"pricing_version":    "a42d372ccf0b5dd13ecf71203521f9d2",
	})
}

func ResetModelRatio(c *gin.Context) {
	defaultStr := ratio_setting.DefaultModelRatio2JSONString()
	err := model.UpdateOption("ModelRatio", defaultStr)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	err = ratio_setting.UpdateModelRatioByJSONString(defaultStr)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(200, gin.H{
		"success": true,
		"message": "重置模型倍率成功",
	})
}
