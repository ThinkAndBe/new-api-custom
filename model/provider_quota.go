package model

import (
	"github.com/QuantumNous/new-api/common"
)

// provider_quota.go — 套餐额度监控：多云服务商账号的套餐用量追踪。
//
// 智谱 bigmodel.cn 的 coding plan 有验证码防护无法程序化登录，
// 采用 Cookie 导入模式：管理员在浏览器登录后复制 session token
// 粘贴到管理页，后台定期用它调内部 API 抓取额度。

// ProviderQuotaAccount 服务商账号
type ProviderQuotaAccount struct {
	Id           int    `gorm:"primaryKey" json:"id"`
	Provider     string `gorm:"size:32;index" json:"provider"` // zhipu / volcano / minimax
	AccountName  string `gorm:"size:128" json:"account_name"`  // 显示名（如 手机号/邮箱）
	SessionToken string `gorm:"type:text" json:"-"`            // Cookie/Token（不序列化到前端）
	OrgId        string `gorm:"size:64" json:"org_id"`         // 选中的机构 ID（智谱多机构需指定）
	QuotaData    string `gorm:"type:text" json:"quota_data"`   // 最近一次抓取的额度 JSON
	LastFetchAt  int64  `gorm:"json:"last_fetch_at"`
	FetchStatus  string `gorm:"size:16" json:"fetch_status"` // ok / expired / error
	FetchError   string `gorm:"size:255" json:"fetch_error"`
	Status       int    `gorm:"default:1" json:"status"`
	CreatedAt    int64  `gorm:"json:"created_at"`
	UpdatedAt    int64  `gorm:"json:"updated_at"`
}

// GetProviderQuotaAccounts 全部账号
func GetProviderQuotaAccounts() ([]*ProviderQuotaAccount, error) {
	var rows []*ProviderQuotaAccount
	err := DB.Where("status <> ?", 0).Order("provider, id").Find(&rows).Error
	return rows, err
}

// GetProviderQuotaAccount 单个
func GetProviderQuotaAccount(id int) (*ProviderQuotaAccount, error) {
	var row ProviderQuotaAccount
	err := DB.First(&row, "id = ?", id).Error
	return &row, err
}

// CreateProviderQuotaAccount 创建
func CreateProviderQuotaAccount(a *ProviderQuotaAccount) error {
	now := common.GetTimestamp()
	a.CreatedAt = now
	a.UpdatedAt = now
	return DB.Create(a).Error
}

// UpdateProviderQuotaAccount 更新
func UpdateProviderQuotaAccount(a *ProviderQuotaAccount) error {
	a.UpdatedAt = common.GetTimestamp()
	return DB.Save(a).Error
}

// DeleteProviderQuotaAccount 软删
func DeleteProviderQuotaAccount(id int) error {
	return DB.Model(&ProviderQuotaAccount{}).Where("id = ?", id).Update("status", 0).Error
}

// SaveQuotaResult 保存抓取结果
func SaveQuotaResult(id int, quotaJSON, fetchStatus, fetchError string) {
	now := common.GetTimestamp()
	DB.Model(&ProviderQuotaAccount{}).Where("id = ?", id).Updates(map[string]interface{}{
		"quota_data":    quotaJSON,
		"last_fetch_at": now,
		"fetch_status":  fetchStatus,
		"fetch_error":   fetchError,
		"updated_at":    now,
	})
}
