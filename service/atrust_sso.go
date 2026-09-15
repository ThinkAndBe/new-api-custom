package service

// atrust_sso.go — aTrust 反向 OAuth2 单点登录（官方票据共享方案）。
//
// 依据《零信任aTrust资源单点登录方案-OAuth对接&票据注入》章节5：
//  1. 应用将未登录浏览器 302 到 aTrust「客户端接入地址」的
//     /passport/v1/public/auth2ssoLogin?appid=&redirectUrl=&responseType=code&state=
//  2. 用户已在零信任客户端登录 → aTrust 302 回 redirectUrl?code=&state=
//  3. 服务端用 code 调 /passport/v1/user/getUserInfoByCode，
//     请求头 X-SDP-Signature = hex(HMAC-SHA256(appSecret,
//     "appid={appid}\ncode={code}"))  （key 按字母序、\n 分隔）
//  4. 拿到 name/displayName 后匹配本地账号并建立会话
//
// code 5 分钟有效、一次性。

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// ATrustSSOUser getUserInfoByCode 返回的用户信息（仅取登录所需字段）
type ATrustSSOUser struct {
	Name        string `json:"name"`        // 用户名（工号/账号）
	DisplayName string `json:"displayName"` // 姓名
	Email       string `json:"email"`
	GroupPath   string `json:"groupPath"`
	AuthMethod  string `json:"authMethod"`
}

type atrustSSOResponse struct {
	Code    int           `json:"code"`
	Message string        `json:"message"`
	Data    ATrustSSOUser `json:"data"`
}

// ATrustSSOConfigured 反向 OAuth2 是否已配置完整
func ATrustSSOConfigured() bool {
	return system_setting.ATrustSSOEnabled &&
		system_setting.ATrustSSOServer != "" &&
		system_setting.ATrustSSOAppId != "" &&
		system_setting.ATrustSSOAppSecret != ""
}

// ATrustSSOAuthorizeURL 构造获取 code 的跳转地址（步骤1）
func ATrustSSOAuthorizeURL(redirectURI, state string) string {
	q := url.Values{}
	q.Set("appid", system_setting.ATrustSSOAppId)
	q.Set("redirectUrl", redirectURI)
	q.Set("responseType", "code")
	if state != "" {
		q.Set("state", state)
	}
	return strings.TrimSuffix(system_setting.ATrustSSOServer, "/") +
		"/passport/v1/public/auth2ssoLogin?" + q.Encode()
}

// atrustSSOSign 按附录7.1：query key 字母序、\n 拼接、HMAC-SHA256 十六进制
func atrustSSOSign(appId, code, appSecret string) string {
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte("appid=" + appId + "\ncode=" + code))
	return hex.EncodeToString(mac.Sum(nil))
}

// atrustSSOAPIBase 换用户信息接口的基地址：分体部署时接入网关（443）只认
// 浏览器会话、控制中心（如 4433）才提供签名 API，故允许单独配置回落。
func atrustSSOAPIBase() string {
	if system_setting.ATrustSSOAPIServer != "" {
		return strings.TrimSuffix(system_setting.ATrustSSOAPIServer, "/")
	}
	return strings.TrimSuffix(system_setting.ATrustSSOServer, "/")
}

// ATrustSSOGetUserInfoByCode 用 code 换用户信息（步骤3）
func ATrustSSOGetUserInfoByCode(code string) (*ATrustSSOUser, error) {
	appId := system_setting.ATrustSSOAppId
	q := url.Values{}
	q.Set("appid", appId)
	q.Set("code", code)
	reqURL := atrustSSOAPIBase() +
		"/passport/v1/user/getUserInfoByCode?" + q.Encode()

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-SDP-Signature", atrustSSOSign(appId, code, system_setting.ATrustSSOAppSecret))

	// aTrust 接入地址通常为自签证书
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aTrust SSO 请求失败: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var ssoResp atrustSSOResponse
	if err := common.Unmarshal(body, &ssoResp); err != nil {
		return nil, fmt.Errorf("解析 aTrust SSO 响应失败: %v", err)
	}
	if ssoResp.Code != 0 {
		return nil, fmt.Errorf("aTrust SSO 错误 code=%d msg=%s", ssoResp.Code, ssoResp.Message)
	}
	if ssoResp.Data.Name == "" && ssoResp.Data.DisplayName == "" {
		return nil, fmt.Errorf("aTrust SSO 返回用户信息为空")
	}
	return &ssoResp.Data, nil
}

// MatchOrCreateATrustSSOUser 按零信任身份匹配本地账号。
// 优先级与原免认证中间件一致：中文姓名（username/display_name）→ 工号（name）
// → 自动创建（ATrustSSOAutoRegister 开启时）。
func MatchOrCreateATrustSSOUser(u *ATrustSSOUser) (*model.User, error) {
	displayName := strings.TrimSpace(u.DisplayName)
	employeeId := strings.TrimSpace(u.Name)

	// 1. 按中文姓名匹配（CSV 导入用户的 username/display_name 为中文姓名）。
	//    同名多人时拒绝登录而不是随机取第一条，避免登进别人的账号。
	if displayName != "" {
		var ms []model.User
		if err := model.DB.Where(
			"username = ? OR display_name = ?", displayName, displayName,
		).Find(&ms).Error; err == nil && len(ms) > 0 {
			if len(ms) > 1 {
				return nil, fmt.Errorf("存在多个匹配「%s」的账号，为避免登错请联系管理员处理", displayName)
			}
			if ms[0].Status != common.UserStatusEnabled {
				return nil, fmt.Errorf("账号 %s 已被禁用，请联系管理员", ms[0].Username)
			}
			return &ms[0], nil
		}
	}

	// 2. 按工号/账号匹配
	if employeeId != "" {
		var m model.User
		if err := model.DB.Where("username = ?", employeeId).First(&m).Error; err == nil {
			if m.Status != common.UserStatusEnabled {
				return nil, fmt.Errorf("账号 %s 已被禁用，请联系管理员", m.Username)
			}
			return &m, nil
		}
	}

	// 3. 自动创建
	if !system_setting.ATrustSSOAutoRegister {
		return nil, fmt.Errorf("未找到匹配账号（姓名=%s 工号=%s），请联系管理员",
			displayName, employeeId)
	}
	username := displayName
	if username == "" {
		username = employeeId
	}
	if username == "" || len(username) > model.UserNameMaxLength {
		return nil, fmt.Errorf("aTrust 用户信息不完整，无法自动创建账号")
	}
	newUser := &model.User{
		Username:    username,
		DisplayName: displayName,
		Password:    common.GetUUID(), // 随机密码，用户经零信任登录永远用不到
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	if err := newUser.Insert(0); err != nil {
		return nil, fmt.Errorf("自动创建账号失败: %v", err)
	}
	common.SysLog("[aTrust SSO] 自动创建用户 " + username)
	return newUser, nil
}

// IsATrustProxyIP 判断来源 IP 是否为 aTrust 反代出口（用于自动跳转 SSO）
func IsATrustProxyIP(ip string) bool {
	for _, p := range strings.Split(system_setting.ATrustProxyIPs, ",") {
		if strings.TrimSpace(p) != "" && strings.TrimSpace(p) == ip {
			return true
		}
	}
	return false
}
