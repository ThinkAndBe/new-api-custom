package model

// open_api_key.go — 对话日志数据开放密钥（供顾问等外部消费方拉取）。
//
// 权限模型（Scope JSON）：
//   groups        可访问的用户分组（空=不限）
//   usernames     可访问的指定用户（与分组并集；两者都空=全部用户）
//   include_content 是否包含对话内容（默认 false：仅元数据，做用量总结够用）
//   max_days      最多回看天数（默认 30，防止一次拉全量历史）

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type OpenAPIKey struct {
	Id         int    `gorm:"primaryKey" json:"id"`
	Name       string `gorm:"size:64" json:"name"`
	Key        string `gorm:"size:64;uniqueIndex" json:"key"`
	Scope      string `gorm:"type:text" json:"scope"` // OpenKeyScope JSON
	Enabled    bool   `json:"enabled"`
	ExpiresAt  int64  `json:"expires_at"` // 0=永久
	ReqCount   int64  `json:"req_count"`
	LastUsedAt int64  `json:"last_used_at"`
	CreatedAt  int64  `json:"created_at"`
}

// OpenKeyScope 数据权限定义
type OpenKeyScope struct {
	Groups         []string `json:"groups"`
	Usernames      []string `json:"usernames"`
	IncludeContent bool     `json:"include_content"`
	MaxDays        int      `json:"max_days"`
}

func (s *OpenKeyScope) Normalize() {
	if s.MaxDays <= 0 {
		s.MaxDays = 30
	}
	if s.MaxDays > 366 {
		s.MaxDays = 366
	}
	if s.Groups == nil {
		s.Groups = []string{}
	}
	if s.Usernames == nil {
		s.Usernames = []string{}
	}
}

func ListOpenAPIKeys() ([]OpenAPIKey, error) {
	var keys []OpenAPIKey
	err := DB.Order("id desc").Find(&keys).Error
	return keys, err
}

func CreateOpenAPIKey(k *OpenAPIKey) error {
	k.CreatedAt = time.Now().Unix()
	return DB.Create(k).Error
}

func UpdateOpenAPIKey(k *OpenAPIKey) error {
	return DB.Model(&OpenAPIKey{}).Where("id = ?", k.Id).Updates(map[string]interface{}{
		"name": k.Name, "scope": k.Scope, "enabled": k.Enabled, "expires_at": k.ExpiresAt,
	}).Error
}

// UpdateOpenAPIKeyPreserveExpire 无 expires_at 的更新（编辑时 0=保持不变场景由
// 调用方走 UpdateOpenAPIKey；此变体在设置 expire 时使用，保留给未来扩展）
func UpdateOpenAPIKeyPreserveExpire(k *OpenAPIKey) error {
	return DB.Model(&OpenAPIKey{}).Where("id = ?", k.Id).Updates(map[string]interface{}{
		"name": k.Name, "scope": k.Scope, "enabled": k.Enabled, "expires_at": k.ExpiresAt,
	}).Error
}

func DeleteOpenAPIKey(id int) error {
	return DB.Delete(&OpenAPIKey{}, "id = ?", id).Error
}

// GetEnabledOpenAPIKey 校验并返回密钥（过期/禁用返回 nil）
func GetEnabledOpenAPIKey(key string) (*OpenAPIKey, *OpenKeyScope, error) {
	var k OpenAPIKey
	err := DB.Where("key = ?", key).First(&k).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, errors.New("密钥无效")
	}
	if err != nil {
		return nil, nil, err
	}
	if !k.Enabled {
		return nil, nil, errors.New("密钥已停用")
	}
	if k.ExpiresAt > 0 && time.Now().Unix() > k.ExpiresAt {
		return nil, nil, errors.New("密钥已过期")
	}
	scope := &OpenKeyScope{}
	if k.Scope != "" {
		if err := common.Unmarshal([]byte(k.Scope), scope); err != nil {
			return nil, nil, errors.New("密钥权限配置损坏")
		}
	}
	scope.Normalize()
	return &k, scope, nil
}

// TouchOpenAPIKey 记录使用
func TouchOpenAPIKey(id int) {
	DB.Model(&OpenAPIKey{}).Where("id = ?", id).Updates(map[string]interface{}{
		"req_count":    gorm.Expr("req_count + 1"),
		"last_used_at": time.Now().Unix(),
	})
}

// ScopeAllowedUsernames 解析 scope 允许的用户名集合；nil 表示不限制
func ScopeAllowedUsernames(scope *OpenKeyScope) ([]string, error) {
	hasGroup := len(scope.Groups) > 0
	hasUser := len(scope.Usernames) > 0
	if !hasGroup && !hasUser {
		return nil, nil
	}
	allowed := map[string]bool{}
	for _, u := range scope.Usernames {
		allowed[u] = true
	}
	if hasGroup {
		var names []string
		err := DB.Model(&User{}).Where(map[string]interface{}{"group": scope.Groups}).
			Pluck("username", &names).Error
		if err != nil {
			return nil, err
		}
		for _, u := range names {
			allowed[u] = true
		}
	}
	out := make([]string, 0, len(allowed))
	for u := range allowed {
		out = append(out, u)
	}
	return out, nil
}
