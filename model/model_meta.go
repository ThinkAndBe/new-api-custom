package model

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const (
	NameRuleExact = iota
	NameRulePrefix
	NameRuleContains
	NameRuleSuffix
)

type BoundChannel struct {
	Name string `json:"name"`
	Type int    `json:"type"`
}

type Model struct {
	Id           int            `json:"id"`
	ModelName    string         `json:"model_name" gorm:"size:128;not null;uniqueIndex:uk_model_name_delete_at,priority:1"`
	Description  string         `json:"description,omitempty" gorm:"type:text"`
	Icon         string         `json:"icon,omitempty" gorm:"type:varchar(128)"`
	Tags         string         `json:"tags,omitempty" gorm:"type:varchar(255)"`
	VendorID     int            `json:"vendor_id,omitempty" gorm:"index"`
	Endpoints    string         `json:"endpoints,omitempty" gorm:"type:text"`
	// 模型能力参数（用于客户端配置导出/使用教程）。来源：管理员维护 + 七牛目录刷新。
	// ParamsLocked=true 表示已被人工编辑，七牛刷新时跳过（避免覆盖人工微调）。
	MaxInputTokens    int  `json:"max_input_tokens,omitempty" gorm:"default:0"`
	MaxOutputTokens   int  `json:"max_output_tokens,omitempty" gorm:"default:0"`
	SupportsToolCall  bool `json:"supports_tool_call,omitempty" gorm:"default:false"`
	SupportsImages    bool `json:"supports_images,omitempty" gorm:"default:false"`
	SupportsReasoning bool `json:"supports_reasoning,omitempty" gorm:"default:false"`
	ParamsLocked      bool `json:"params_locked,omitempty" gorm:"default:false"`
	// 定价覆盖字段（直接存价格，非倍率）。0 = 未配置/使用全局默认。
	// 与 params_locked 同一思路：人工编辑后锁定，七牛同步不再覆盖。
	InputPrice    float64 `json:"input_price,omitempty" gorm:"default:0"`     // 每 1M tokens 输入价格（人民币）= 缓存创建价格
	OutputPrice   float64 `json:"output_price,omitempty" gorm:"default:0"`    // 每 1M tokens 输出价格（人民币）
	CacheHitPrice float64 `json:"cache_hit_price,omitempty" gorm:"default:0"` // 每 1M tokens 缓存命中价格（人民币）
	PricingLocked bool    `json:"pricing_locked,omitempty" gorm:"default:false"`
	Status       int            `json:"status" gorm:"default:1"`
	SyncOfficial int            `json:"sync_official" gorm:"default:1"`
	CreatedTime  int64          `json:"created_time" gorm:"bigint"`
	UpdatedTime  int64          `json:"updated_time" gorm:"bigint"`
	DeletedAt    gorm.DeletedAt `json:"-" gorm:"index;uniqueIndex:uk_model_name_delete_at,priority:2"`

	BoundChannels []BoundChannel `json:"bound_channels,omitempty" gorm:"-"`
	EnableGroups  []string       `json:"enable_groups,omitempty" gorm:"-"`
	QuotaTypes    []int          `json:"quota_types,omitempty" gorm:"-"`
	NameRule      int            `json:"name_rule" gorm:"default:0"`

	MatchedModels []string `json:"matched_models,omitempty" gorm:"-"`
	MatchedCount  int      `json:"matched_count,omitempty" gorm:"-"`
}

func (mi *Model) Insert() error {
	now := common.GetTimestamp()
	mi.CreatedTime = now
	mi.UpdatedTime = now

	// 保存原始值（因为 Create 后可能被 GORM 的 default 标签覆盖为 1）
	originalStatus := mi.Status
	originalSyncOfficial := mi.SyncOfficial

	// 先创建记录（GORM 会对零值字段应用默认值）
	if err := DB.Create(mi).Error; err != nil {
		return err
	}

	// 使用保存的原始值进行更新，确保零值能正确保存
	return DB.Model(&Model{}).Where("id = ?", mi.Id).Updates(map[string]interface{}{
		"status":        originalStatus,
		"sync_official": originalSyncOfficial,
	}).Error
}

func IsModelNameDuplicated(id int, name string) (bool, error) {
	if name == "" {
		return false, nil
	}
	var cnt int64
	err := DB.Model(&Model{}).Where("model_name = ? AND id <> ?", name, id).Count(&cnt).Error
	return cnt > 0, err
}

func (mi *Model) Update() error {
	mi.UpdatedTime = common.GetTimestamp()
	// 使用 Select 强制更新所有字段，包括零值
	return DB.Model(&Model{}).Where("id = ?", mi.Id).
		Select("model_name", "description", "icon", "tags", "vendor_id", "endpoints",
			"max_input_tokens", "max_output_tokens", "supports_tool_call", "supports_images",
			"supports_reasoning", "params_locked",
			"input_price", "output_price", "cache_hit_price", "pricing_locked",
			"status", "sync_official", "name_rule", "updated_time").
		Updates(mi).Error
}

func (mi *Model) Delete() error {
	return DB.Delete(mi).Error
}

// ---- 模型白名单：仅模型管理里配置的模型对用户可见/可调用 ----

var (
	registeredModelCache     map[string]struct{} // 精确名集合（status=1）
	registeredRuleCache      []Model             // 非精确规则行（prefix/suffix/contains, status=1）
	registeredModelCacheLock sync.RWMutex
	registeredModelCacheAt   time.Time
)

const registeredModelCacheTTL = 60 * time.Second

// refreshRegisteredModelCache 重建注册模型缓存。仅精确名走 map；
// 规则行保留原始对象按 NameRule 匹配。
func refreshRegisteredModelCache() {
	var rows []Model
	DB.Where("status = ?", 1).Find(&rows)
	exact := make(map[string]struct{}, len(rows))
	rules := make([]Model, 0)
	for _, r := range rows {
		if r.NameRule == NameRuleExact {
			exact[r.ModelName] = struct{}{}
		} else {
			rules = append(rules, r)
		}
	}
	registeredModelCacheLock.Lock()
	registeredModelCache = exact
	registeredRuleCache = rules
	registeredModelCacheAt = time.Now()
	registeredModelCacheLock.Unlock()
}

// InvalidateRegisteredModelCache 模型管理增删改后调用，立即失效缓存。
func InvalidateRegisteredModelCache() {
	registeredModelCacheLock.Lock()
	registeredModelCacheAt = time.Time{}
	registeredModelCacheLock.Unlock()
}

// IsModelRegistered 判断模型是否已在模型管理注册（精确名或规则匹配）。
// 白名单语义：未注册的模型对用户隐藏且调用被拦截。
func IsModelRegistered(name string) bool {
	if name == "" {
		return false
	}
	registeredModelCacheLock.RLock()
	if time.Since(registeredModelCacheAt) > registeredModelCacheTTL {
		registeredModelCacheLock.RUnlock()
		refreshRegisteredModelCache()
		registeredModelCacheLock.RLock()
	}
	if _, ok := registeredModelCache[name]; ok {
		registeredModelCacheLock.RUnlock()
		return true
	}
	rules := registeredRuleCache
	registeredModelCacheLock.RUnlock()
	for _, r := range rules {
		switch r.NameRule {
		case NameRulePrefix:
			if strings.HasPrefix(name, r.ModelName) {
				return true
			}
		case NameRuleSuffix:
			if strings.HasSuffix(name, r.ModelName) {
				return true
			}
		case NameRuleContains:
			if strings.Contains(name, r.ModelName) {
				return true
			}
		}
	}
	return false
}

// FilterRegisteredModels 过滤掉未注册模型，保持原顺序。
func FilterRegisteredModels(models []string) []string {
	out := make([]string, 0, len(models))
	for _, m := range models {
		if IsModelRegistered(m) {
			out = append(out, m)
		}
	}
	return out
}

func GetVendorModelCounts() (map[int64]int64, error) {
	var stats []struct {
		VendorID int64
		Count    int64
	}
	if err := DB.Model(&Model{}).
		Select("vendor_id as vendor_id, count(*) as count").
		Group("vendor_id").
		Scan(&stats).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]int64, len(stats))
	for _, s := range stats {
		m[s.VendorID] = s.Count
	}
	return m, nil
}

func GetAllModels(offset int, limit int) ([]*Model, error) {
	var models []*Model
	err := DB.Order("id DESC").Offset(offset).Limit(limit).Find(&models).Error
	return models, err
}

func GetBoundChannelsByModelsMap(modelNames []string) (map[string][]BoundChannel, error) {
	result := make(map[string][]BoundChannel)
	if len(modelNames) == 0 {
		return result, nil
	}
	type row struct {
		Model string
		Name  string
		Type  int
	}
	var rows []row
	err := DB.Table("channels").
		Select("abilities.model as model, channels.name as name, channels.type as type").
		Joins("JOIN abilities ON abilities.channel_id = channels.id").
		Where("abilities.model IN ? AND abilities.enabled = ?", modelNames, true).
		Distinct().
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		result[r.Model] = append(result[r.Model], BoundChannel{Name: r.Name, Type: r.Type})
	}
	return result, nil
}

func normalizeLookupValues(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}

func GetPreferredModelOwnerChannelTypes(modelNames []string, groups []string) (map[string]int, error) {
	result := make(map[string]int)
	modelNames = normalizeLookupValues(modelNames)
	if len(modelNames) == 0 {
		return result, nil
	}

	type row struct {
		Model       string
		ChannelType int
	}
	var rows []row

	query := DB.Table("abilities").
		Select("abilities.model as model, channels.type as channel_type").
		Joins("JOIN channels ON abilities.channel_id = channels.id").
		Where("abilities.model IN ? AND abilities.enabled = ? AND channels.status = ?", modelNames, true, common.ChannelStatusEnabled).
		Order("COALESCE(abilities.priority, 0) DESC").
		Order("abilities.weight DESC").
		Order("abilities.channel_id ASC")

	groups = normalizeLookupValues(groups)
	if len(groups) > 0 {
		query = query.Where("abilities."+commonGroupCol+" IN ?", groups)
	}

	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}

	for _, r := range rows {
		if _, ok := result[r.Model]; ok {
			continue
		}
		result[r.Model] = r.ChannelType
	}
	return result, nil
}

func SearchModels(keyword string, vendor string, offset int, limit int) ([]*Model, int64, error) {
	var models []*Model
	db := DB.Model(&Model{})
	if keyword != "" {
		like := "%" + keyword + "%"
		db = db.Where("model_name LIKE ? OR description LIKE ? OR tags LIKE ?", like, like, like)
	}
	if vendor != "" {
		if vid, err := strconv.Atoi(vendor); err == nil {
			db = db.Where("models.vendor_id = ?", vid)
		} else {
			db = db.Joins("JOIN vendors ON vendors.id = models.vendor_id").Where("vendors.name LIKE ?", "%"+vendor+"%")
		}
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := db.Order("models.id DESC").Offset(offset).Limit(limit).Find(&models).Error; err != nil {
		return nil, 0, err
	}
	return models, total, nil
}
