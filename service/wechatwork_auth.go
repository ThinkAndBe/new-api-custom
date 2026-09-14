package service

// wechatwork_auth.go — 企业微信扫码登录（自建应用 OAuth）。
//
// 背景：aTrust 反向代理不透传用户身份（所有请求来源 IP 相同、无身份头），
// IP 匹配免登录不可行。员工的 aTrust 认证方式即企业微信扫码，故改用
// 企微自建应用 OAuth 实现同体验的免密登录：
//
//	浏览器跳转企微统一登录页扫码 → 回调携带 code →
//	服务端用 code 换 userid（企业成员唯一标识）→ 匹配本地账号。
//
// 前置条件（企业管理员在企微后台创建自建应用）：
//  1. 拿到 CorpID / AgentId / Secret；
//  2. 应用「网页授权及 JS-SDK 可信域名」配置为 tokenhub 域名；
//  3. 如需按姓名匹配存量账号，应用需有通讯录只读权限（user/get 接口）。

import (
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const weChatWorkAPIBase = "https://qyapi.weixin.qq.com/cgi-bin"

type weChatWorkTokenResp struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type weChatWorkUserInfoResp struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
	UserID  string `json:"userid"` // 企业成员账号标识
}

type weChatWorkUserDetailResp struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
	Name    string `json:"name"` // 成员姓名
}

var (
	weChatWorkToken       string
	weChatWorkTokenExpiry time.Time
)

// WeChatWorkEnabled 企业微信扫码登录是否就绪
func WeChatWorkEnabled() bool {
	return system_setting.WeChatWorkAuthEnabled &&
		system_setting.WeChatWorkCorpID != "" &&
		system_setting.WeChatWorkSecret != ""
}

func weChatWorkHTTPGet(url string, out any) error {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := common.DecodeJson(resp.Body, out); err != nil {
		return fmt.Errorf("解析企业微信响应失败: %v", err)
	}
	return nil
}

// weChatWorkAccessToken 获取 access_token（缓存，提前 5 分钟刷新）
func weChatWorkAccessToken() (string, error) {
	if weChatWorkToken != "" && time.Now().Before(weChatWorkTokenExpiry) {
		return weChatWorkToken, nil
	}
	var resp weChatWorkTokenResp
	url := fmt.Sprintf("%s/gettoken?corpid=%s&corpsecret=%s",
		weChatWorkAPIBase, system_setting.WeChatWorkCorpID, system_setting.WeChatWorkSecret)
	if err := weChatWorkHTTPGet(url, &resp); err != nil {
		return "", err
	}
	if resp.ErrCode != 0 || resp.AccessToken == "" {
		return "", fmt.Errorf("企业微信 gettoken 失败 code=%d msg=%s", resp.ErrCode, resp.ErrMsg)
	}
	weChatWorkToken = resp.AccessToken
	weChatWorkTokenExpiry = time.Now().Add(time.Duration(resp.ExpiresIn)*time.Second - 5*time.Minute)
	return weChatWorkToken, nil
}

// WeChatWorkUserIDByCode 用 OAuth 回调 code 换企业成员 userid
func WeChatWorkUserIDByCode(code string) (string, error) {
	token, err := weChatWorkAccessToken()
	if err != nil {
		return "", err
	}
	var resp weChatWorkUserInfoResp
	url := fmt.Sprintf("%s/auth/getuserinfo?access_token=%s&code=%s",
		weChatWorkAPIBase, token, code)
	if err := weChatWorkHTTPGet(url, &resp); err != nil {
		return "", err
	}
	if resp.ErrCode != 0 {
		return "", fmt.Errorf("企业微信 getuserinfo 失败 code=%d msg=%s", resp.ErrCode, resp.ErrMsg)
	}
	if resp.UserID == "" {
		return "", fmt.Errorf("非企业成员，无法登录")
	}
	return resp.UserID, nil
}

// WeChatWorkUserName 查成员姓名（需应用有通讯录只读权限；失败仅影响
// 姓名匹配，不影响绑定关系登录，故调用方应忽略错误继续）
func WeChatWorkUserName(userid string) (string, error) {
	token, err := weChatWorkAccessToken()
	if err != nil {
		return "", err
	}
	var resp weChatWorkUserDetailResp
	url := fmt.Sprintf("%s/user/get?access_token=%s&userid=%s",
		weChatWorkAPIBase, token, userid)
	if err := weChatWorkHTTPGet(url, &resp); err != nil {
		return "", err
	}
	if resp.ErrCode != 0 {
		return "", fmt.Errorf("企业微信 user/get 失败 code=%d msg=%s", resp.ErrCode, resp.ErrMsg)
	}
	return resp.Name, nil
}
