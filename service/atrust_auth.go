package service

// atrust_auth.go — 深信服零信任 aTrust 免认证：通过 IP 反查在线用户身份。
//
// 原理：用户通过 aTrust 客户端连接后访问 tokenhub，请求来源 IP 即为
// aTrust 分配的虚拟 IP（每用户唯一）。new-api 用此 IP 调 aTrust OpenAPI
// 的「查询在线用户」接口，匹配到用户后自动登录。
//
// 签名算法：HMAC-SHA256(签名密钥, 签名串)，详见 OpenAPI 文档 2.3.2。

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// ATrustConfig 零信任配置（从 options 表读取）
type ATrustConfig struct {
	Enabled   bool   // 是否启用免认证
	Server    string // aTrust 控制中心地址（如 https://10.0.0.1:4433）
	APIId     string
	APISecret string
}

// ATrustOnlineUser 在线用户条目
type ATrustOnlineUser struct {
	Id          string `json:"id"`
	Name        string `json:"name"`        // 用户名
	DisplayName string `json:"displayName"` // 显示名
	RemoteIp    string `json:"remoteIp"`    // 接入 IP
	UserId      string `json:"userId"`
	GroupPath   string `json:"groupPath"`
}

// ATrustOnlineResponse 在线用户查询响应
type ATrustOnlineResponse struct {
	Code int `json:"code"`
	Data struct {
		Data  []ATrustOnlineUser `json:"data"`
		Count int                `json:"count"`
	} `json:"data"`
	Msg string `json:"msg"`
}

var (
	atrustCache    map[string]*ATrustOnlineUser // ip → user
	atrustCacheMu  sync.RWMutex
	atrustCacheAt  time.Time
	atrustCacheTTL = 30 * time.Second // 缓存 30 秒，避免每次请求都查 aTrust
)

// GetATrustConfig 从系统配置读取
func GetATrustConfig() ATrustConfig {
	return ATrustConfig{
		Enabled:   system_setting.ATrustEnabled,
		Server:    system_setting.ATrustServer,
		APIId:     system_setting.ATrustAPIId,
		APISecret: system_setting.ATrustAPISecret,
	}
}

// atrustSign 计算 HMAC-SHA256 签名
func atrustSign(apiId, apiSecret, timestamp, nonce, method, path, query, body string) string {
	// 1. 构造签名串
	signStr := path
	if query != "" {
		// query 参数按 key ASCII 排序
		params := strings.Split(query, "&")
		sort.Strings(params)
		sortedQuery := strings.Join(params, "&")
		signStr = path + "?" + sortedQuery
		if body != "" {
			signStr = signStr + "&" + body
		}
	} else if body != "" {
		signStr = path + "?" + body
	}

	// 2. 构造签名密钥
	keyStr := "appId=" + apiId +
		"&appSecret=" + apiSecret +
		"&timestamp=" + timestamp +
		"&nonce=" + nonce

	// 3. HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(keyStr))
	mac.Write([]byte(signStr))
	return hex.EncodeToString(mac.Sum(nil))
}

// atrustRequest 发送带签名的请求
func atrustRequest(cfg ATrustConfig, method, path, query string) ([]byte, error) {
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	nonce := common.GetUUID()

	sign := atrustSign(cfg.APIId, cfg.APISecret, timestamp, nonce, method, path, query, "")

	fullURL := strings.TrimSuffix(cfg.Server, "/") + path
	if query != "" {
		fullURL = fullURL + "?" + query
	}

	req, err := http.NewRequest(method, fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-ca-sign", sign)
	req.Header.Set("x-ca-key", cfg.APIId)
	req.Header.Set("x-ca-timestamp", timestamp)
	req.Header.Set("x-ca-nonce", nonce)
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")

	// aTrust 可能用自签证书，跳过验证
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aTrust 请求失败: %v", err)
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// ATrustLookupByIP 通过 IP 查 aTrust 在线用户
func ATrustLookupByIP(clientIP string) (*ATrustOnlineUser, error) {
	cfg := GetATrustConfig()
	if !cfg.Enabled || cfg.Server == "" {
		return nil, fmt.Errorf("aTrust 免认证未启用")
	}

	// 先查缓存
	atrustCacheMu.RLock()
	if time.Since(atrustCacheAt) < atrustCacheTTL {
		if u, ok := atrustCache[clientIP]; ok {
			atrustCacheMu.RUnlock()
			return u, nil
		}
		// IP 不在缓存里，且缓存还新鲜 → 不在线
		atrustCacheMu.RUnlock()
		return nil, fmt.Errorf("IP 不在 aTrust 在线列表")
	}
	atrustCacheMu.RUnlock()

	// 缓存过期，拉取全部在线用户重建缓存
	query := url.Values{}
	query.Set("pageSize", "500")
	query.Set("pageIndex", "1")

	body, err := atrustRequest(cfg, "GET", "/api/v1/monitor/getUserStatus", query.Encode())
	if err != nil {
		return nil, err
	}

	var resp ATrustOnlineResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析 aTrust 响应失败: %v", err)
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("aTrust 返回错误 code=%d msg=%s", resp.Code, resp.Msg)
	}

	// 重建缓存
	atrustCacheMu.Lock()
	atrustCache = make(map[string]*ATrustOnlineUser)
	for i := range resp.Data.Data {
		u := &resp.Data.Data[i]
		if u.RemoteIp != "" {
			atrustCache[u.RemoteIp] = u
		}
	}
	atrustCacheAt = time.Now()
	atrustCacheMu.Unlock()

	if u, ok := atrustCache[clientIP]; ok {
		return u, nil
	}
	return nil, fmt.Errorf("IP %s 不在 aTrust 在线列表", clientIP)
}
