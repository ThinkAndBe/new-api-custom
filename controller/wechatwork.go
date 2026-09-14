package controller

// wechatwork.go — 企业微信扫码登录：登录 + 绑定。
//
// 匹配顺序（登录回调时）：
//  1. wechat_work_id 已绑定 → 直接登录；
//  2. 企微通讯录姓名精确匹配存量启用账号（管理员 CSV 导入的员工）→
//     自动补绑并登录（同名多人则拒绝，提示手动绑定）；
//  3. WeChatWorkAutoRegister 开启时自动建号，否则提示引导绑定。
//
// 绑定：已登录状态下访问同一回调即为绑定流程（与通用 OAuth 一致）。

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// WeChatWorkLogin 企业微信 OAuth 回调（登录 / 绑定）
func WeChatWorkLogin(c *gin.Context) {
	if !service.WeChatWorkEnabled() {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "管理员未开启企业微信登录",
		})
		return
	}

	session := sessions.Default(c)

	// CSRF：校验 state
	state := c.Query("state")
	if state == "" || session.Get("oauth_state") == nil || state != session.Get("oauth_state").(string) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "无效的登录状态，请重新发起",
		})
		return
	}

	code := c.Query("code")
	if code == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "缺少授权码",
		})
		return
	}

	userid, err := service.WeChatWorkUserIDByCode(code)
	if err != nil {
		common.SysError("[企业微信登录] code 换 userid 失败: " + err.Error())
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "企业微信授权失败：" + err.Error(),
		})
		return
	}

	// 已登录 → 绑定流程
	if session.Get("username") != nil {
		weChatWorkBind(c, session, userid)
		return
	}

	// 1. 绑定关系直接登录
	if model.IsWeChatWorkIdAlreadyTaken(userid) {
		user := model.User{WeChatWorkId: userid}
		if err := user.FillUserByWeChatWorkId(); err != nil {
			common.ApiError(c, err)
			return
		}
		if user.Id == 0 {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "该企业微信账号绑定的用户已注销",
			})
			return
		}
		if user.Status != common.UserStatusEnabled {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "该账号已被封禁，请联系管理员",
			})
			return
		}
		setupLogin(&user, c)
		return
	}

	// 2. 姓名匹配存量账号（管理员导入的员工 display_name 为中文姓名）
	name, nameErr := service.WeChatWorkUserName(userid)
	if nameErr != nil {
		// 通讯录权限未开等情况，仅记录，走后续流程
		common.SysLog("[企业微信登录] 查询成员姓名失败（不影响绑定登录）: " + nameErr.Error())
	}
	if name != "" {
		users, err := model.GetEnabledUsersByDisplayName(name)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		switch len(users) {
		case 1:
			user := users[0]
			user.WeChatWorkId = userid
			if err := user.Update(false); err != nil {
				common.ApiError(c, err)
				return
			}
			common.SysLog("[企业微信登录] 姓名匹配自动绑定: " + name + " → 用户 " + user.Username)
			setupLogin(&user, c)
			return
		case 0:
			// 无同名，走自动注册判断
		default:
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "存在多个同名账号（" + name + "），无法自动匹配，请用密码登录后在「个人设置」绑定企业微信",
			})
			return
		}
	}

	// 3. 自动注册
	if !system_setting.WeChatWorkAutoRegister {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "该企业微信未绑定账号，请用密码登录后在「个人设置-第三方登录绑定」中绑定",
		})
		return
	}

	user := &model.User{
		Role:   common.RoleCommonUser,
		Status: common.UserStatusEnabled,
	}
	// 企微 userid 通常是字母数字账号名，直接作用户名（校验长度）
	if len(userid) <= model.UserNameMaxLength {
		if exists, err := model.CheckUserExistOrDeleted(userid, ""); err == nil && !exists {
			user.Username = userid
		}
	}
	if user.Username == "" {
		user.Username = "ww_" + userid
		if len(user.Username) > model.UserNameMaxLength {
			user.Username = "ww_" + common.GetRandomString(8)
		}
	}
	if name != "" {
		user.DisplayName = name
	} else {
		user.DisplayName = userid
	}
	if err := user.Insert(0); err != nil {
		common.ApiError(c, err)
		return
	}
	user.WeChatWorkId = userid
	if err := user.Update(false); err != nil {
		common.ApiError(c, err)
		return
	}
	common.SysLog("[企业微信登录] 自动注册用户: " + user.Username + "（企微 userid=" + userid + "）")
	setupLogin(user, c)
}

// weChatWorkBind 已登录用户绑定企业微信
func weChatWorkBind(c *gin.Context, session sessions.Session, userid string) {
	if model.IsWeChatWorkIdAlreadyTaken(userid) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "该企业微信账号已被绑定",
		})
		return
	}
	id := session.Get("id")
	user := model.User{Id: id.(int)}
	if err := user.FillUserById(); err != nil {
		common.ApiError(c, err)
		return
	}
	if user.Id == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户已注销",
		})
		return
	}
	user.WeChatWorkId = userid
	if err := user.Update(false); err != nil {
		common.ApiError(c, err)
		return
	}
	common.SysLog("[企业微信登录] 绑定成功: 用户 " + user.Username + " ← 企微 userid=" + userid)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "企业微信绑定成功",
		"data":    gin.H{"action": "bind"},
	})
}
