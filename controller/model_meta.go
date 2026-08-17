package controller

import (
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

// GetAllModelsMeta 获取模型列表（分页）
func GetAllModelsMeta(c *gin.Context) {

	pageInfo := common.GetPageQuery(c)
	modelsMeta, err := model.GetAllModels(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 批量填充附加字段，提升列表接口性能
	enrichModels(modelsMeta)
	var total int64
	model.DB.Model(&model.Model{}).Count(&total)

	// 统计供应商计数（全部数据，不受分页影响）
	vendorCounts, _ := model.GetVendorModelCounts()

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(modelsMeta)
	common.ApiSuccess(c, gin.H{
		"items":         modelsMeta,
		"total":         total,
		"page":          pageInfo.GetPage(),
		"page_size":     pageInfo.GetPageSize(),
		"vendor_counts": vendorCounts,
	})
}

// SearchModelsMeta 搜索模型列表
func SearchModelsMeta(c *gin.Context) {

	keyword := c.Query("keyword")
	vendor := c.Query("vendor")
	pageInfo := common.GetPageQuery(c)

	modelsMeta, total, err := model.SearchModels(keyword, vendor, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 批量填充附加字段，提升列表接口性能
	enrichModels(modelsMeta)
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(modelsMeta)
	common.ApiSuccess(c, pageInfo)
}

// GetModelMeta 根据 ID 获取单条模型信息
func GetModelMeta(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var m model.Model
	if err := model.DB.First(&m, id).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	enrichModels([]*model.Model{&m})
	common.ApiSuccess(c, &m)
}

// CreateModelMeta 新建模型
func CreateModelMeta(c *gin.Context) {
	var m model.Model
	if err := c.ShouldBindJSON(&m); err != nil {
		common.ApiError(c, err)
		return
	}
	if m.ModelName == "" {
		common.ApiErrorMsg(c, "模型名称不能为空")
		return
	}
	// 名称冲突检查
	if dup, err := model.IsModelNameDuplicated(0, m.ModelName); err != nil {
		common.ApiError(c, err)
		return
	} else if dup {
		common.ApiErrorMsg(c, "模型名称已存在")
		return
	}

	if err := m.Insert(); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RefreshPricing()
	model.InvalidateRegisteredModelCache()
	common.ApiSuccess(c, &m)
}

// UpdateModelMeta 更新模型
func UpdateModelMeta(c *gin.Context) {
	statusOnly := c.Query("status_only") == "true"

	var m model.Model
	if err := c.ShouldBindJSON(&m); err != nil {
		common.ApiError(c, err)
		return
	}
	if m.Id == 0 {
		common.ApiErrorMsg(c, "缺少模型 ID")
		return
	}

	if statusOnly {
		// 只更新状态，防止误清空其他字段
		if err := model.DB.Model(&model.Model{}).Where("id = ?", m.Id).Update("status", m.Status).Error; err != nil {
			common.ApiError(c, err)
			return
		}
	} else {
		// 名称冲突检查
		if dup, err := model.IsModelNameDuplicated(m.Id, m.ModelName); err != nil {
			common.ApiError(c, err)
			return
		} else if dup {
			common.ApiErrorMsg(c, "模型名称已存在")
			return
		}

		// 同步定价覆盖到全局 ratio 配置（与模型参数 params_locked 同一思路）
		if err := syncModelPricingOverrides(&m); err != nil {
			common.ApiError(c, err)
			return
		}

		if err := m.Update(); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	model.RefreshPricing()
	model.InvalidateRegisteredModelCache()
	common.ApiSuccess(c, &m)
}

// syncModelPricingOverrides 将模型直接价格覆盖同步到全局配置。
// 与模型参数 params_locked 同一思路：人工编辑后覆盖全局默认，litellm/官方同步不再覆盖。
//
// 价格单位：人民币 / 1M tokens。0 表示未配置，恢复该维度的全局默认。
// 计费内核已改为人民币基准：ratio 1 = ¥2/1M 输入（QuotaPerUnit=500000 即 ¥1=50万 quota）。
// 因此人民币价格直接除以 2 写入 model_ratio，不做汇率换算。
//
// Set-or-restore 语义：价格 > 0 写入覆盖值；价格 = 0 时把该模型从全局 map 里恢复为内置默认
// （若内置默认也无，则删除条目，等价于走 hardcoded 兜底分支）。
func syncModelPricingOverrides(m *model.Model) error {
	name := m.ModelName

	// 输入价格 -> ModelRatio（ratio = 人民币价格(¥/1M) / 2）。0 恢复内置默认。
	modelRatioMap := ratio_setting.GetModelRatioCopy()
	if m.InputPrice > 0 {
		modelRatioMap[name] = m.InputPrice / 2
	} else {
		if dv, ok := ratio_setting.GetDefaultModelRatioMap()[name]; ok {
			modelRatioMap[name] = dv
		} else {
			delete(modelRatioMap, name)
		}
	}
	if b, err := common.Marshal(modelRatioMap); err == nil {
		if err := model.UpdateOption("ModelRatio", string(b)); err != nil {
			return err
		}
		_ = ratio_setting.UpdateModelRatioByJSONString(string(b))
	}

	// 输出价格 -> CompletionRatio（输出倍率 = 输出价格 / 输入价格，同币种比值不变）。0 恢复内置默认。
	completionRatioMap := ratio_setting.GetCompletionRatioCopy()
	if m.OutputPrice > 0 && m.InputPrice > 0 {
		completionRatioMap[name] = m.OutputPrice / m.InputPrice
	} else {
		if dv, ok := ratio_setting.GetDefaultCompletionRatioMap()[name]; ok {
			completionRatioMap[name] = dv
		} else {
			delete(completionRatioMap, name)
		}
	}
	if b, err := common.Marshal(completionRatioMap); err == nil {
		if err := model.UpdateOption("CompletionRatio", string(b)); err != nil {
			return err
		}
		_ = ratio_setting.UpdateCompletionRatioByJSONString(string(b))
	}

	// 缓存命中价格 -> CacheRatio（比值 = 缓存命中价格 / 输入价格）。0 恢复内置默认。
	cacheRatioMap := ratio_setting.GetCacheRatioCopy()
	if m.CacheHitPrice > 0 && m.InputPrice > 0 {
		cacheRatioMap[name] = m.CacheHitPrice / m.InputPrice
	} else {
		if dv, ok := ratio_setting.GetDefaultCacheRatioMap()[name]; ok {
			cacheRatioMap[name] = dv
		} else {
			delete(cacheRatioMap, name)
		}
	}
	if b, err := common.Marshal(cacheRatioMap); err == nil {
		if err := model.UpdateOption("CacheRatio", string(b)); err != nil {
			return err
		}
		_ = ratio_setting.UpdateCacheRatioByJSONString(string(b))
	}

	// 缓存创建价格 = 输入价格（缓存未命中时按输入价计费，创建缓存也按输入价）
	// 写入 CreateCacheRatio = 1（即与输入价相同）
	createCacheRatioMap := ratio_setting.GetCreateCacheRatioCopy()
	if m.InputPrice > 0 {
		createCacheRatioMap[name] = 1
	} else {
		if dv, ok := ratio_setting.GetDefaultCreateCacheRatioMap()[name]; ok {
			createCacheRatioMap[name] = dv
		} else {
			delete(createCacheRatioMap, name)
		}
	}
	if b, err := common.Marshal(createCacheRatioMap); err == nil {
		if err := model.UpdateOption("CreateCacheRatio", string(b)); err != nil {
			return err
		}
		_ = ratio_setting.UpdateCreateCacheRatioByJSONString(string(b))
	}
	return nil
}

// DeleteModelMeta 删除模型
func DeleteModelMeta(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DB.Delete(&model.Model{}, id).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	model.RefreshPricing()
	model.InvalidateRegisteredModelCache()
	common.ApiSuccess(c, nil)
}

// enrichModels 批量填充附加信息：端点、渠道、分组、计费类型，避免 N+1 查询
func enrichModels(models []*model.Model) {
	if len(models) == 0 {
		return
	}

	// 1) 拆分精确与规则匹配
	exactNames := make([]string, 0)
	exactIdx := make(map[string][]int) // modelName -> indices in models
	ruleIndices := make([]int, 0)
	for i, m := range models {
		if m == nil {
			continue
		}
		if m.NameRule == model.NameRuleExact {
			exactNames = append(exactNames, m.ModelName)
			exactIdx[m.ModelName] = append(exactIdx[m.ModelName], i)
		} else {
			ruleIndices = append(ruleIndices, i)
		}
	}

	// 2) 批量查询精确模型的绑定渠道
	channelsByModel, _ := model.GetBoundChannelsByModelsMap(exactNames)

	// 3) 精确模型：端点从缓存、渠道批量映射、分组/计费类型从缓存
	for name, indices := range exactIdx {
		chs := channelsByModel[name]
		for _, idx := range indices {
			mm := models[idx]
			if mm.Endpoints == "" {
				eps := model.GetModelSupportEndpointTypes(mm.ModelName)
				if b, err := common.Marshal(eps); err == nil {
					mm.Endpoints = string(b)
				}
			}
			mm.BoundChannels = chs
			mm.EnableGroups = model.GetModelEnableGroups(mm.ModelName)
			mm.QuotaTypes = model.GetModelQuotaTypes(mm.ModelName)
		}
	}

	if len(ruleIndices) == 0 {
		return
	}

	// 4) 一次性读取定价缓存，内存匹配所有规则模型
	pricings := model.GetPricing()

	// 为全部规则模型收集匹配名集合、端点并集、分组并集、配额集合
	matchedNamesByIdx := make(map[int][]string)
	endpointSetByIdx := make(map[int]map[constant.EndpointType]struct{})
	groupSetByIdx := make(map[int]map[string]struct{})
	quotaSetByIdx := make(map[int]map[int]struct{})

	for _, p := range pricings {
		for _, idx := range ruleIndices {
			mm := models[idx]
			var matched bool
			switch mm.NameRule {
			case model.NameRulePrefix:
				matched = strings.HasPrefix(p.ModelName, mm.ModelName)
			case model.NameRuleSuffix:
				matched = strings.HasSuffix(p.ModelName, mm.ModelName)
			case model.NameRuleContains:
				matched = strings.Contains(p.ModelName, mm.ModelName)
			}
			if !matched {
				continue
			}
			matchedNamesByIdx[idx] = append(matchedNamesByIdx[idx], p.ModelName)

			es := endpointSetByIdx[idx]
			if es == nil {
				es = make(map[constant.EndpointType]struct{})
				endpointSetByIdx[idx] = es
			}
			for _, et := range p.SupportedEndpointTypes {
				es[et] = struct{}{}
			}

			gs := groupSetByIdx[idx]
			if gs == nil {
				gs = make(map[string]struct{})
				groupSetByIdx[idx] = gs
			}
			for _, g := range p.EnableGroup {
				gs[g] = struct{}{}
			}

			qs := quotaSetByIdx[idx]
			if qs == nil {
				qs = make(map[int]struct{})
				quotaSetByIdx[idx] = qs
			}
			qs[p.QuotaType] = struct{}{}
		}
	}

	// 5) 汇总所有匹配到的模型名称，批量查询一次渠道
	allMatchedSet := make(map[string]struct{})
	for _, names := range matchedNamesByIdx {
		for _, n := range names {
			allMatchedSet[n] = struct{}{}
		}
	}
	allMatched := make([]string, 0, len(allMatchedSet))
	for n := range allMatchedSet {
		allMatched = append(allMatched, n)
	}
	matchedChannelsByModel, _ := model.GetBoundChannelsByModelsMap(allMatched)

	// 6) 回填每个规则模型的并集信息
	for _, idx := range ruleIndices {
		mm := models[idx]

		// 端点并集 -> 序列化
		if es, ok := endpointSetByIdx[idx]; ok && mm.Endpoints == "" {
			eps := make([]constant.EndpointType, 0, len(es))
			for et := range es {
				eps = append(eps, et)
			}
			if b, err := common.Marshal(eps); err == nil {
				mm.Endpoints = string(b)
			}
		}

		// 分组并集
		if gs, ok := groupSetByIdx[idx]; ok {
			groups := make([]string, 0, len(gs))
			for g := range gs {
				groups = append(groups, g)
			}
			mm.EnableGroups = groups
		}

		// 配额类型集合（保持去重并排序）
		if qs, ok := quotaSetByIdx[idx]; ok {
			arr := make([]int, 0, len(qs))
			for k := range qs {
				arr = append(arr, k)
			}
			sort.Ints(arr)
			mm.QuotaTypes = arr
		}

		// 渠道并集
		names := matchedNamesByIdx[idx]
		channelSet := make(map[string]model.BoundChannel)
		for _, n := range names {
			for _, ch := range matchedChannelsByModel[n] {
				key := ch.Name + "_" + strconv.Itoa(ch.Type)
				channelSet[key] = ch
			}
		}
		if len(channelSet) > 0 {
			chs := make([]model.BoundChannel, 0, len(channelSet))
			for _, ch := range channelSet {
				chs = append(chs, ch)
			}
			mm.BoundChannels = chs
		}

		// 匹配信息
		mm.MatchedModels = names
		mm.MatchedCount = len(names)
	}
}
