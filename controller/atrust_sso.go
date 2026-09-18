package controller

// atrust_sso.go — aTrust 反向 OAuth2 单点登录入口与回调。
//
// /api/oauth/atrust/start   ：生成 state、写防重定向 Cookie，302 到 aTrust 获取 code
// /api/oauth/atrust         ：aTrust 302 回到这里（?code=&state=），换用户信息并登录
//
// 与企微/标准 OAuth 不同，本流程是纯浏览器 302 链：成功 302 到控制台，
// 失败 302 回登录页并携带 atrust_error 参数（登录页读取后提示）。

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// atrustSSOTriedCookie 防自动跳转死循环：start 时写入，成功登录后清除。
// 未登录用户每次页面请求都会被中间件导向 SSO；若 aTrust 侧登录态异常，
// 靠该 Cookie 让用户落到正常登录页而不是无限重定向。
const atrustSSOTriedCookie = "atrust_sso_tried"

// atrustSSOCallbackURL 回调地址必须与 aTrust 应用配置的回调地址完全一致，
// 故从系统配置的站点地址推导，而不是取请求 Host（反代后不可靠）。
func atrustSSOCallbackURL() string {
	base := strings.TrimSuffix(system_setting.ServerAddress, "/")
	if base == "" {
		base = "http://localhost:3000"
	}
	return base + "/api/oauth/atrust"
}

// ATrustSSOStart 发起零信任单点登录
func ATrustSSOStart(c *gin.Context) {
	if !service.ATrustSSOConfigured() {
		c.Redirect(http.StatusFound, "/login")
		return
	}

	session := sessions.Default(c)

	// 已登录则回到 SSO 回调页拉登录态（写 localStorage）再进控制台。
	// 不直接跳 /console：SPA 路由守卫依赖 localStorage 用户对象，
	// 缺失时会被弹回登录页（前端登录态引导缺口）。
	if session.Get("username") != nil {
		c.Redirect(http.StatusFound, "/oauth/atrust?sso=1")
		return;
	}

	// state（复用通用 OAuth 的会话字段，回调时校验）
	state := common.GetRandomString(12)
	session.Set("oauth_state", state)
	_ = session.Save()

	// 防循环标记（会话级 Cookie）
	c.SetCookie(atrustSSOTriedCookie, "1", 0, "/", "", false, true)

	c.Redirect(http.StatusFound,
		service.ATrustSSOAuthorizeURL(atrustSSOCallbackURL(), state))
}

// ATrustSSOCallback aTrust 授权回调：code 换用户信息并登录
func ATrustSSOCallback(c *gin.Context) {
	redirectLogin := func(errMsg string) {
		common.SysError("[aTrust SSO] 登录失败: " + errMsg)
		c.Redirect(http.StatusFound, "/login?atrust_error="+url.QueryEscape(errMsg))
	}

	if !service.ATrustSSOConfigured() {
		redirectLogin("管理员未启用零信任单点登录")
		return
	}

	// 校验 state（CSRF）
	session := sessions.Default(c)
	state := c.Query("state")
	if state == "" || session.Get("oauth_state") == nil || state != session.Get("oauth_state").(string) {
		redirectLogin("登录状态校验失败，请重试")
		return
	}

	code := c.Query("code")
	if code == "" {
		redirectLogin("未获取到授权码")
		return
	}

	ssoUser, err := service.ATrustSSOGetUserInfoByCode(code)
	if err != nil {
		redirectLogin(err.Error())
		return
	}

	user, err := service.MatchOrCreateATrustSSOUser(ssoUser)
	if err != nil {
		redirectLogin(err.Error())
		return
	}
	if user.Status != common.UserStatusEnabled {
		redirectLogin("账号已被禁用，请联系管理员")
		return
	}

	// 建立会话（写 session 后浏览器跳转控制台即为登录态）
	session.Set("id", user.Id)
	session.Set("username", user.Username)
	session.Set("role", user.Role)
	session.Set("status", user.Status)
	session.Set("group", user.Group)
	if err := session.Save(); err != nil {
		redirectLogin("会话保存失败: " + err.Error())
		return
	}
	model.UpdateUserLastLoginAt(user.Id)
	recordLoginAudit(user, c)

	// 清除防循环 Cookie，放行后续自动跳转
	c.SetCookie(atrustSSOTriedCookie, "", -1, "/", "", false, true)

	common.SysLog("[aTrust SSO] 用户登录成功: " + user.Username +
		"（零信任身份 name=" + ssoUser.Name + " displayName=" + ssoUser.DisplayName + "）")

	// 跳到前端回调页完成登录引导（写 localStorage 用户态、初始化上下文）。
	// 服务端直跳 /console 会被路由守卫当作未登录弹回（localStorage 无用户对象）。
	c.Redirect(http.StatusFound, "/oauth/atrust?sso=1")
}

// ATrustSSOFinish 前端回调页拉取登录态：会话已建立，返回用户信息
// 供前端写入 localStorage（与 /api/user/login 响应同构）。
// 失败路径记日志：假 401 / 会话丢失这类「服务端 SSO 成功但前端未落位」
// 的问题此前在 docker logs 里不可见，排障只能靠猜。
func ATrustSSOFinish(c *gin.Context) {
	session := sessions.Default(c)
	id := session.Get("id")
	if id == nil {
		common.SysLog("[aTrust SSO] finish 失败: 无会话（Cookie 未达或已过期）ip=" + c.ClientIP())
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "零信任会话不存在，请重新发起登录",
		})
		return
	}
	user := model.User{Id: id.(int)}
	if err := user.FillUserById(); err != nil || user.Id == 0 {
		session.Clear()
		_ = session.Save()
		common.SysError("[aTrust SSO] finish 失败: 用户不存在或已注销 id=" + fmt.Sprintf("%d", user.Id))
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户不存在或已注销",
		})
		return
	}
	if user.Status != common.UserStatusEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "账号已被禁用，请联系管理员",
		})
		return
	}
	setupLogin(&user, c)
}
