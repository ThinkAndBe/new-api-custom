package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

var (
	errUserPasswordUnset    = errors.New("user password is not set")
	errOriginalPasswordFail = errors.New("original password is incorrect")
)

func Login(c *gin.Context) {
	if !common.PasswordLoginEnabled {
		common.ApiErrorI18n(c, i18n.MsgUserPasswordLoginDisabled)
		return
	}
	var loginRequest LoginRequest
	err := json.NewDecoder(c.Request.Body).Decode(&loginRequest)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	username := loginRequest.Username
	password := loginRequest.Password
	if username == "" || password == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	user := model.User{
		Username: username,
		Password: password,
	}
	err = user.ValidateAndFill()
	if err != nil {
		switch {
		case errors.Is(err, model.ErrDatabase):
			common.SysLog(fmt.Sprintf("Login database error for user %s: %v", username, err))
			common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		case errors.Is(err, model.ErrUserEmptyCredentials):
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		default:
			common.ApiErrorI18n(c, i18n.MsgUserUsernameOrPasswordError)
		}
		return
	}

	// 检查是否启用2FA
	if model.IsTwoFAEnabled(user.Id) {
		// 设置pending session，等待2FA验证
		session := sessions.Default(c)
		session.Set("pending_username", user.Username)
		session.Set("pending_user_id", user.Id)
		err := session.Save()
		if err != nil {
			common.ApiErrorI18n(c, i18n.MsgUserSessionSaveFailed)
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": i18n.T(c, i18n.MsgUserRequire2FA),
			"success": true,
			"data": map[string]interface{}{
				"require_2fa": true,
			},
		})
		return
	}

	// 检查是否需要强制修改密码（管理员创建的用户首次登录）
	if user.MustChangePassword {
		// 设置 pending session，等待首次改密完成
		session := sessions.Default(c)
		session.Set("pending_username", user.Username)
		session.Set("pending_user_id", user.Id)
		session.Set("pending_must_change_password", true)
		if err := session.Save(); err != nil {
			common.ApiErrorI18n(c, i18n.MsgUserSessionSaveFailed)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message": "首次登录，请修改密码",
			"success": true,
			"data": map[string]interface{}{
				"must_change_password": true,
				"username":             user.Username,
			},
		})
		return
	}

	setupLogin(&user, c)
}

// ChangePasswordOnFirstLogin 首次登录强制改密接口。
// 需要 pending session（由 Login 设置），验证旧密码后更新密码并清除标志。
func ChangePasswordOnFirstLogin(c *gin.Context) {
	var req struct {
		Username         string `json:"username"`
		OriginalPassword string `json:"original_password"`
		NewPassword      string `json:"new_password"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if req.NewPassword == "" || len(req.NewPassword) < 8 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "密码长度至少为8个字符",
		})
		return
	}

	// 校验 pending session
	session := sessions.Default(c)
	pendingUsername, ok1 := session.Get("pending_username").(string)
	pendingMustChange, ok2 := session.Get("pending_must_change_password").(bool)
	if !ok1 || !ok2 || !pendingMustChange || pendingUsername == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "会话已过期，请重新登录",
		})
		return
	}
	if req.Username != "" && req.Username != pendingUsername {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户名不匹配",
		})
		return
	}

	// 查用户（不通过 ValidateAndFill，因为不需要密码验证登录）
	user := model.User{Username: pendingUsername}
	if err := model.DB.Where("username = ?", pendingUsername).First(&user).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户验证失败",
		})
		return
	}
	// 验证旧密码（原始密码）
	if !common.ValidatePasswordAndHash(req.OriginalPassword, user.Password) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "原密码不正确",
		})
		return
	}

	// 更新密码并清除标志
	user.Password = req.NewPassword
	user.MustChangePassword = false
	if err := user.Update(true); err != nil {
		common.ApiError(c, err)
		return
	}

	// 清除 pending session 标志
	session.Delete("pending_must_change_password")
	session.Delete("pending_username")
	session.Delete("pending_user_id")
	_ = session.Save()

	// 完成登录
	setupLogin(&user, c)
}

// loginMethodFromContext 根据请求路径推导登录方式，用于登录审计日志。
func loginMethodFromContext(c *gin.Context) string {
	switch c.FullPath() {
	case "/api/user/login":
		return "password"
	case "/api/user/login/2fa":
		return "2fa"
	case "/api/user/passkey/login/finish":
		return "passkey"
	case "/api/oauth/wechat":
		return "wechat"
	case "/api/oauth/telegram/login":
		return "telegram"
	case "/api/oauth/:provider":
		if provider := c.Param("provider"); provider != "" {
			return "oauth:" + provider
		}
		return "oauth"
	default:
		return "unknown"
	}
}

// recordLoginAudit 记录登录成功审计日志（对所有用户启用，仅记录成功，不记录失败）。
func recordLoginAudit(user *model.User, c *gin.Context) {
	method := loginMethodFromContext(c)
	ip := c.ClientIP()
	extra := map[string]interface{}{
		"login_method": method,
		"user_agent":   c.Request.UserAgent(),
	}
	content := fmt.Sprintf("Logged in successfully via %s", method)
	model.RecordLoginLog(user.Id, user.Username, content, ip, "login", map[string]interface{}{
		"method": method,
	}, extra)
}

// setup session & cookies and then return user info
func setupLogin(user *model.User, c *gin.Context) {
	model.UpdateUserLastLoginAt(user.Id)
	session := sessions.Default(c)
	session.Set("id", user.Id)
	session.Set("username", user.Username)
	session.Set("role", user.Role)
	session.Set("status", user.Status)
	session.Set("group", user.Group)
	err := session.Save()
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserSessionSaveFailed)
		return
	}
	recordLoginAudit(user, c)
	c.JSON(http.StatusOK, gin.H{
		"message": "",
		"success": true,
		"data": map[string]any{
			"id":           user.Id,
			"username":     user.Username,
			"display_name": user.DisplayName,
			"role":         user.Role,
			"status":       user.Status,
			"group":        user.Group,
		},
	})
}

func Logout(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	err := session.Save()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"message": err.Error(),
			"success": false,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "",
		"success": true,
	})
}

func Register(c *gin.Context) {
	if !common.RegisterEnabled {
		common.ApiErrorI18n(c, i18n.MsgUserRegisterDisabled)
		return
	}
	if !common.PasswordRegisterEnabled {
		common.ApiErrorI18n(c, i18n.MsgUserPasswordRegisterDisabled)
		return
	}
	var user model.User
	err := json.NewDecoder(c.Request.Body).Decode(&user)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	user.Username = strings.TrimSpace(user.Username)
	user.Email = model.NormalizeEmail(user.Email)
	if user.Username == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := common.Validate.Struct(&user); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserInputInvalid, map[string]any{"Error": err.Error()})
		return
	}
	if common.EmailVerificationEnabled {
		if user.Email == "" || user.VerificationCode == "" {
			common.ApiErrorI18n(c, i18n.MsgUserEmailVerificationRequired)
			return
		}
		if !common.VerifyCodeWithKey(user.Email, user.VerificationCode, common.EmailVerificationPurpose) {
			common.ApiErrorI18n(c, i18n.MsgUserVerificationCodeError)
			return
		}
		if err := model.EnsureEmailAvailable(user.Email, 0); err != nil {
			if errors.Is(err, model.ErrEmailAlreadyTaken) {
				common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
				return
			}
			common.ApiErrorI18n(c, i18n.MsgDatabaseError)
			return
		}
	}
	emailForExistCheck := ""
	if common.EmailVerificationEnabled {
		emailForExistCheck = user.Email
	}
	exist, err := model.CheckUserExistOrDeleted(user.Username, emailForExistCheck)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		common.SysLog(fmt.Sprintf("CheckUserExistOrDeleted error: %v", err))
		return
	}
	if exist {
		common.ApiErrorI18n(c, i18n.MsgUserExists)
		return
	}
	affCode := user.AffCode // this code is the inviter's code, not the user's own code
	inviterId, _ := model.GetUserIdByAffCode(affCode)
	cleanUser := model.User{
		Username:    user.Username,
		Password:    user.Password,
		DisplayName: user.Username,
		InviterId:   inviterId,
		Role:        common.RoleCommonUser, // 明确设置角色为普通用户
	}
	if common.EmailVerificationEnabled {
		cleanUser.Email = user.Email
	}
	if err := cleanUser.Insert(inviterId); err != nil {
		if errors.Is(err, model.ErrEmailAlreadyTaken) {
			common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
			return
		}
		common.ApiError(c, err)
		return
	}

	// 获取插入后的用户ID
	var insertedUser model.User
	if err := model.DB.Where("username = ?", cleanUser.Username).First(&insertedUser).Error; err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserRegisterFailed)
		return
	}
	// 生成默认令牌
	if constant.GenerateDefaultToken {
		key, err := common.GenerateKey()
		if err != nil {
			common.ApiErrorI18n(c, i18n.MsgUserDefaultTokenFailed)
			common.SysLog("failed to generate token key: " + err.Error())
			return
		}
		// 生成默认令牌
		token := model.Token{
			UserId:             insertedUser.Id, // 使用插入后的用户ID
			Name:               cleanUser.Username + "的初始令牌",
			Key:                key,
			CreatedTime:        common.GetTimestamp(),
			AccessedTime:       common.GetTimestamp(),
			ExpiredTime:        -1,     // 永不过期
			RemainQuota:        500000, // 示例额度
			UnlimitedQuota:     true,
			ModelLimitsEnabled: false,
		}
		if setting.DefaultUseAutoGroup {
			token.Group = "auto"
		}
		if err := token.Insert(); err != nil {
			common.ApiErrorI18n(c, i18n.MsgCreateDefaultTokenErr)
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func GetAllUsers(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	users, total, err := model.GetAllUsers(pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(users)

	common.ApiSuccess(c, pageInfo)
	return
}

func SearchUsers(c *gin.Context) {
	keyword := c.Query("keyword")
	group := c.Query("group")
	var role *int
	if roleStr := c.Query("role"); roleStr != "" {
		if parsed, err := strconv.Atoi(roleStr); err == nil {
			role = &parsed
		}
	}
	var status *int
	if statusStr := c.Query("status"); statusStr != "" {
		if parsed, err := strconv.Atoi(statusStr); err == nil {
			status = &parsed
		}
	}
	pageInfo := common.GetPageQuery(c)
	users, total, err := model.SearchUsers(keyword, group, role, status, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(users)
	common.ApiSuccess(c, pageInfo)
	return
}

func canManageTargetRole(myRole int, targetRole int) bool {
	return myRole == common.RoleRootUser || myRole > targetRole
}

func GetUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	user, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	myRole := c.GetInt("role")
	if !canManageTargetRole(myRole, user.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionSameLevel)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    user,
	})
	return
}

func GenerateAccessToken(c *gin.Context) {
	id := c.GetInt("id")
	user, err := model.GetUserById(id, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// get rand int 28-32
	randI := common.GetRandomInt(4)
	key, err := common.GenerateRandomKey(29 + randI)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgGenerateFailed)
		common.SysLog("failed to generate key: " + err.Error())
		return
	}
	user.SetAccessToken(key)

	if model.DB.Where("access_token = ?", user.AccessToken).First(user).RowsAffected != 0 {
		common.ApiErrorI18n(c, i18n.MsgUuidDuplicate)
		return
	}

	if err := user.Update(false); err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    user.AccessToken,
	})
	return
}

type TransferAffQuotaRequest struct {
	Quota int `json:"quota" binding:"required"`
}

func TransferAffQuota(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	id := c.GetInt("id")
	user, err := model.GetUserById(id, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	tran := TransferAffQuotaRequest{}
	if err := c.ShouldBindJSON(&tran); err != nil {
		common.ApiError(c, err)
		return
	}
	err = user.TransferAffQuotaToQuota(tran.Quota)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserTransferFailed, map[string]any{"Error": err.Error()})
		return
	}
	common.ApiSuccessI18n(c, i18n.MsgUserTransferSuccess, nil)
}

func GetAffCode(c *gin.Context) {
	id := c.GetInt("id")
	user, err := model.GetUserById(id, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if user.AffCode == "" {
		user.AffCode = common.GetRandomString(4)
		if err := user.Update(false); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    user.AffCode,
	})
	return
}

func GetSelf(c *gin.Context) {
	id := c.GetInt("id")
	userRole := c.GetInt("role")
	user, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// Hide admin remarks: set to empty to trigger omitempty tag, ensuring the remark field is not included in JSON returned to regular users
	user.Remark = ""

	// 计算用户权限信息
	permissions := calculateUserPermissions(userRole)

	// 获取用户设置并提取sidebar_modules
	userSetting := user.GetSetting()

	// 构建响应数据，包含用户信息和权限
	responseData := map[string]interface{}{
		"id":                user.Id,
		"username":          user.Username,
		"display_name":      user.DisplayName,
		"role":              user.Role,
		"status":            user.Status,
		"email":             user.Email,
		"github_id":         user.GitHubId,
		"discord_id":        user.DiscordId,
		"oidc_id":           user.OidcId,
		"wechat_id":         user.WeChatId,
		"telegram_id":       user.TelegramId,
		"group":             user.Group,
		"quota":             user.Quota,
		"used_quota":        user.UsedQuota,
		"request_count":     user.RequestCount,
		"aff_code":          user.AffCode,
		"aff_count":         user.AffCount,
		"aff_quota":         user.AffQuota,
		"aff_history_quota": user.AffHistoryQuota,
		"inviter_id":        user.InviterId,
		"linux_do_id":       user.LinuxDOId,
		"setting":           user.Setting,
		"stripe_customer":   user.StripeCustomer,
		"sidebar_modules":   userSetting.SidebarModules, // 正确提取sidebar_modules字段
		"permissions":       permissions,                // 新增权限字段
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    responseData,
	})
	return
}

// 计算用户权限的辅助函数
func calculateUserPermissions(userRole int) map[string]interface{} {
	permissions := map[string]interface{}{}

	// 根据用户角色计算权限
	if userRole == common.RoleRootUser {
		// 超级管理员不需要边栏设置功能
		permissions["sidebar_settings"] = false
		permissions["sidebar_modules"] = map[string]interface{}{}
	} else if userRole == common.RoleAdminUser {
		// 管理员可以设置边栏，但不包含系统设置功能
		permissions["sidebar_settings"] = true
		permissions["sidebar_modules"] = map[string]interface{}{
			"admin": map[string]interface{}{
				"setting": false, // 管理员不能访问系统设置
			},
		}
	} else {
		// 普通用户只能设置个人功能，不包含管理员区域
		permissions["sidebar_settings"] = true
		permissions["sidebar_modules"] = map[string]interface{}{
			"admin": false, // 普通用户不能访问管理员区域
		}
	}

	return permissions
}

// 根据用户角色生成默认的边栏配置
func generateDefaultSidebarConfig(userRole int) string {
	defaultConfig := map[string]interface{}{}

	// 聊天区域 - 所有用户都可以访问
	defaultConfig["chat"] = map[string]interface{}{
		"enabled":    true,
		"playground": true,
		"chat":       true,
	}

	// 控制台区域 - 所有用户都可以访问
	defaultConfig["console"] = map[string]interface{}{
		"enabled":    true,
		"detail":     true,
		"token":      true,
		"log":        true,
		"midjourney": true,
		"task":       true,
	}

	// 个人中心区域 - 所有用户都可以访问
	defaultConfig["personal"] = map[string]interface{}{
		"enabled":  true,
		"topup":    true,
		"personal": true,
	}

	// 管理员区域 - 根据角色决定
	if userRole == common.RoleAdminUser {
		// 管理员可以访问管理员区域，但不能访问系统设置
		defaultConfig["admin"] = map[string]interface{}{
			"enabled":    true,
			"channel":    true,
			"models":     true,
			"redemption": true,
			"user":       true,
			"setting":    false, // 管理员不能访问系统设置
		}
	} else if userRole == common.RoleRootUser {
		// 超级管理员可以访问所有功能
		defaultConfig["admin"] = map[string]interface{}{
			"enabled":    true,
			"channel":    true,
			"models":     true,
			"redemption": true,
			"user":       true,
			"setting":    true,
		}
	}
	// 普通用户不包含admin区域

	// 转换为JSON字符串
	configBytes, err := json.Marshal(defaultConfig)
	if err != nil {
		common.SysLog("生成默认边栏配置失败: " + err.Error())
		return ""
	}

	return string(configBytes)
}

func GetUserModels(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		id = c.GetInt("id")
	}
	user, err := model.GetUserCache(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// all=1 返回"理论可用"的完整模型清单（含临时被禁用的渠道），
	// 用于使用教程页生成稳定的 models.json，避免渠道禁用/恢复导致用户配置抖动。
	// 默认行为保持原有：仅返回当前 enabled=true 的模型。
	includeDisabled := c.Query("all") == "1" || c.Query("all") == "true"
	// 普通用户只展示自身分组的模型；管理员可以看到所有用户可选分组的模型（便于排查）
	var groups map[string]string
	if model.IsAdmin(id) {
		groups = service.GetUserUsableGroups(user.Group)
	} else {
		groups = service.GetUserOwnedGroups(user.Group)
	}
	var models []string
	for group := range groups {
		var groupModels []string
		if includeDisabled {
			groupModels = model.GetGroupModels(group)
		} else {
			groupModels = model.GetGroupEnabledModels(group)
		}
		for _, g := range groupModels {
			if !common.StringsContains(models, g) {
				models = append(models, g)
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    models,
	})
	return
}

// userModelMeta 模型参数元数据（用户侧使用教程用）
type userModelMeta struct {
	ModelName         string `json:"model_name"`
	MaxInputTokens    int    `json:"max_input_tokens"`
	MaxOutputTokens   int    `json:"max_output_tokens"`
	SupportsToolCall  bool   `json:"supports_tool_call"`
	SupportsImages    bool   `json:"supports_images"`
	SupportsReasoning bool   `json:"supports_reasoning"`
}

// GetUserModelsMeta 返回用户当前分组下所有可用模型的参数元数据（max_in/out/tool/vision/reasoning）。
// 用于使用教程页面导出客户端配置（models.json），替代前端硬编码兜底。
// GET /api/user/models/meta
// 查询参数 all=1：返回"理论可用"的完整清单（含临时禁用渠道），用于生成稳定的 models.json。
func GetUserModelsMeta(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		id = c.GetInt("id")
	}
	user, err := model.GetUserCache(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	includeDisabled := c.Query("all") == "1" || c.Query("all") == "true"
	// 与 GetUserModels 保持一致：普通用户只看自身分组，管理员看全部可选分组
	var groups map[string]string
	if model.IsAdmin(id) {
		groups = service.GetUserUsableGroups(user.Group)
	} else {
		groups = service.GetUserOwnedGroups(user.Group)
	}
	// 收集所有可用模型名（去重）
	modelSet := make(map[string]struct{})
	for group := range groups {
		var groupModels []string
		if includeDisabled {
			groupModels = model.GetGroupModels(group)
		} else {
			groupModels = model.GetGroupEnabledModels(group)
		}
		for _, g := range groupModels {
			modelSet[g] = struct{}{}
		}
	}
	if len(modelSet) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": []userModelMeta{}})
		return
	}
	// 构造 IN 查询的模型名切片
	names := make([]string, 0, len(modelSet))
	for name := range modelSet {
		names = append(names, name)
	}
	// 批量查 models 表，取能力参数（max_input_tokens=0 说明未填，仍返回让前端兜底）
	var dbModels []model.Model
	if err := model.DB.Unscoped().Select("model_name", "max_input_tokens", "max_output_tokens", "supports_tool_call", "supports_images", "supports_reasoning").
		Where("model_name IN ?", names).Find(&dbModels).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	// 用 NameRule 匹配：未精确命中的模型尝试前缀/包含匹配
	metaMap := make(map[string]userModelMeta, len(dbModels))
	for _, m := range dbModels {
		metaMap[strings.ToLower(m.ModelName)] = userModelMeta{
			ModelName:         m.ModelName,
			MaxInputTokens:    m.MaxInputTokens,
			MaxOutputTokens:   m.MaxOutputTokens,
			SupportsToolCall:  m.SupportsToolCall,
			SupportsImages:    m.SupportsImages,
			SupportsReasoning: m.SupportsReasoning,
		}
	}
	result := make([]userModelMeta, 0, len(names))
	for _, name := range names {
		if meta, ok := metaMap[strings.ToLower(name)]; ok {
			result = append(result, meta)
		} else {
			// 数据库无此模型注册：返回零值，前端用兜底
			result = append(result, userModelMeta{ModelName: name})
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    result,
	})
	return
}

// modelRecoveryInfo 暂不可用模型的渠道恢复信息（用户侧使用教程用）
type modelRecoveryInfo struct {
	ModelName   string `json:"model_name"`
	RecoveryAt  int64  `json:"recovery_at"`  // 最早预计恢复时间戳（秒），0 表示无预计时间
	ChannelName string `json:"channel_name"` // 提供该恢复时间的渠道名
}

// GetUserModelsRecovery 返回当前用户分组下"暂不可用"模型（完整清单 - 当前可用）
// 对应的渠道预计恢复时间。用于使用教程页在暂不可用模型旁展示恢复时间。
// 同一模型可能由多个禁用渠道提供，取最早的 recovery_at 展示。
// GET /api/user/models/recovery
func GetUserModelsRecovery(c *gin.Context) {
	id := c.GetInt("id")
	user, err := model.GetUserCache(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 与 GetUserModels 保持一致：普通用户只看自身分组，管理员看全部可选分组
	var groups map[string]string
	if model.IsAdmin(id) {
		groups = service.GetUserUsableGroups(user.Group)
	} else {
		groups = service.GetUserOwnedGroups(user.Group)
	}
	groupNames := make([]string, 0, len(groups))
	for g := range groups {
		groupNames = append(groupNames, g)
	}
	if len(groupNames) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": []modelRecoveryInfo{}})
		return
	}
	// 暂不可用模型 = 分组理论清单（含禁用渠道）- 当前可用（仅 enabled）
	disabledSet := make(map[string]struct{})
	for _, g := range groupNames {
		enabledSet := make(map[string]struct{})
		for _, m := range model.GetGroupEnabledModels(g) {
			enabledSet[m] = struct{}{}
		}
		for _, m := range model.GetGroupModels(g) {
			if _, ok := enabledSet[m]; !ok {
				disabledSet[m] = struct{}{}
			}
		}
	}
	if len(disabledSet) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": []modelRecoveryInfo{}})
		return
	}
	disabledNames := make([]string, 0, len(disabledSet))
	for m := range disabledSet {
		disabledNames = append(disabledNames, m)
	}
	// 查这些模型在用户分组下的禁用渠道（未启用 + 未删除），取其 other_info 里的 recovery_at
	var channels []model.Channel
	query := model.DB.Select("id", "name", "models", model.CommonGroupCol(), "other_info").
		Where("status <> ?", common.ChannelStatusEnabled)
	for _, g := range groupNames {
		query = query.Or(model.CommonGroupCol()+" LIKE ?", "%,"+g+",%")
	}
	if err := query.Find(&channels).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	recoveryMap := make(map[string]*modelRecoveryInfo)
	for i := range channels {
		ch := &channels[i]
		info := ch.GetOtherInfo()
		var recoveryAt int64
		switch v := info["recovery_at"].(type) {
		case float64:
			recoveryAt = int64(v)
		case int64:
			recoveryAt = v
		case int:
			recoveryAt = int64(v)
		}
		for _, m := range strings.Split(ch.Models, ",") {
			m = strings.TrimSpace(m)
			if _, ok := disabledSet[m]; !ok {
				continue
			}
			if existing, ok := recoveryMap[m]; ok {
				// 已有记录：保留恢复时间更早的；都无时间则保持原样
				if recoveryAt > 0 && (existing.RecoveryAt == 0 || recoveryAt < existing.RecoveryAt) {
					existing.RecoveryAt = recoveryAt
					existing.ChannelName = ch.Name
				}
			} else {
				recoveryMap[m] = &modelRecoveryInfo{
					ModelName:   m,
					RecoveryAt:  recoveryAt,
					ChannelName: ch.Name,
				}
			}
		}
	}
	result := make([]modelRecoveryInfo, 0, len(recoveryMap))
	for _, m := range disabledNames {
		if info, ok := recoveryMap[m]; ok {
			result = append(result, *info)
		} else {
			result = append(result, modelRecoveryInfo{ModelName: m})
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    result,
	})
	return
}

// GetModelsJsonTemplate 返回管理员配置的 models.json 模板（用户侧）。
// 模板中 {{apiKey}} 和 {{baseUrl}} 占位符由前端替换为用户实际值。
// GET /api/user/models/template
func GetModelsJsonTemplate(c *gin.Context) {
	common.OptionMapRWMutex.RLock()
	template := common.OptionMap["ModelsJsonTemplate"]
	common.OptionMapRWMutex.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    template,
	})
}

func UpdateUser(c *gin.Context) {
	var updatedUser model.User
	err := json.NewDecoder(c.Request.Body).Decode(&updatedUser)
	if err != nil || updatedUser.Id == 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	updatedUser.Username = strings.TrimSpace(updatedUser.Username)
	if updatedUser.Username == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if updatedUser.Password == "" {
		updatedUser.Password = "$I_LOVE_U" // make Validator happy :)
	}
	if err := common.Validate.Struct(&updatedUser); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserInputInvalid, map[string]any{"Error": err.Error()})
		return
	}
	originUser, err := model.GetUserById(updatedUser.Id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	myRole := c.GetInt("role")
	if !canManageTargetRole(myRole, originUser.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
		return
	}
	if !canManageTargetRole(myRole, updatedUser.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserCannotCreateHigherLevel)
		return
	}
	if updatedUser.Password == "$I_LOVE_U" {
		updatedUser.Password = "" // rollback to what it should be
	}
	updatePassword := updatedUser.Password != ""
	if err := updatedUser.Edit(updatePassword); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, updatedUser.Id, "user.update", map[string]interface{}{
		"username": originUser.Username,
		"id":       updatedUser.Id,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func AdminClearUserBinding(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	bindingType := strings.ToLower(strings.TrimSpace(c.Param("binding_type")))
	if bindingType == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	user, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	myRole := c.GetInt("role")
	if !canManageTargetRole(myRole, user.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionSameLevel)
		return
	}

	if err := user.ClearBinding(bindingType); err != nil {
		common.ApiError(c, err)
		return
	}

	recordManageAuditFor(c, user.Id, "user.binding_clear", map[string]interface{}{
		"bindingType": bindingType,
		"username":    user.Username,
	})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "success",
	})
}

func UpdateSelf(c *gin.Context) {
	var requestData map[string]interface{}
	err := json.NewDecoder(c.Request.Body).Decode(&requestData)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	// 检查是否是用户设置更新请求 (sidebar_modules 或 language)
	if sidebarModules, sidebarExists := requestData["sidebar_modules"]; sidebarExists {
		userId := c.GetInt("id")
		user, err := model.GetUserById(userId, false)
		if err != nil {
			common.ApiError(c, err)
			return
		}

		// 获取当前用户设置
		currentSetting := user.GetSetting()

		// 更新sidebar_modules字段
		if sidebarModulesStr, ok := sidebarModules.(string); ok {
			currentSetting.SidebarModules = sidebarModulesStr
		}

		// 保存更新后的设置
		user.SetSetting(currentSetting)
		if err := user.Update(false); err != nil {
			common.ApiErrorI18n(c, i18n.MsgUpdateFailed)
			return
		}

		common.ApiSuccessI18n(c, i18n.MsgUpdateSuccess, nil)
		return
	}

	// 检查是否是语言偏好更新请求
	if language, langExists := requestData["language"]; langExists {
		userId := c.GetInt("id")
		user, err := model.GetUserById(userId, false)
		if err != nil {
			common.ApiError(c, err)
			return
		}

		// 获取当前用户设置
		currentSetting := user.GetSetting()

		// 更新language字段
		if langStr, ok := language.(string); ok {
			currentSetting.Language = langStr
		}

		// 保存更新后的设置
		user.SetSetting(currentSetting)
		if err := user.Update(false); err != nil {
			common.ApiErrorI18n(c, i18n.MsgUpdateFailed)
			return
		}

		common.ApiSuccessI18n(c, i18n.MsgUpdateSuccess, nil)
		return
	}

	// 原有的用户信息更新逻辑
	var user model.User
	requestDataBytes, err := json.Marshal(requestData)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	err = json.Unmarshal(requestDataBytes, &user)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	if user.Password == "" {
		user.Password = "$I_LOVE_U" // make Validator happy :)
	}
	if err := common.Validate.Struct(&user); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidInput)
		return
	}

	cleanUser := model.User{
		Id:          c.GetInt("id"),
		Username:    user.Username,
		Password:    user.Password,
		DisplayName: user.DisplayName,
	}
	if user.Password == "$I_LOVE_U" {
		user.Password = "" // rollback to what it should be
		cleanUser.Password = ""
	}
	updatePassword, err := checkUpdatePassword(user.OriginalPassword, user.Password, cleanUser.Id)
	if err != nil {
		if errors.Is(err, errUserPasswordUnset) {
			common.ApiErrorI18n(c, i18n.MsgUserPasswordUnset)
			return
		}
		if errors.Is(err, errOriginalPasswordFail) {
			common.ApiErrorI18n(c, i18n.MsgUserOriginalPasswordError)
			return
		}
		common.ApiError(c, err)
		return
	}
	if err := cleanUser.Update(updatePassword); err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func checkUpdatePassword(originalPassword string, newPassword string, userId int) (updatePassword bool, err error) {
	if newPassword == "" {
		return
	}
	var currentUser *model.User
	currentUser, err = model.GetUserById(userId, true)
	if err != nil {
		return
	}

	// 密码不为空,需要验证原密码
	if currentUser.Password == "" {
		err = errUserPasswordUnset
		return
	}
	if !common.ValidatePasswordAndHash(originalPassword, currentUser.Password) {
		err = errOriginalPasswordFail
		return
	}
	updatePassword = true
	return
}

func DeleteUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 用 Unscoped 查询，已注销（软删除）的用户也需要能被彻底删除
	originUser, err := model.GetUserByIdUnscoped(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	myRole := c.GetInt("role")
	if myRole <= originUser.Role {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
		return
	}
	err = model.HardDeleteUserById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, originUser.Id, "user.delete", map[string]interface{}{
		"username": originUser.Username,
		"id":       originUser.Id,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

// ReactivateUser 管理员恢复已注销的用户
func ReactivateUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 用 Unscoped 查询，能查到软删除的用户
	originUser, err := model.GetUserByIdUnscoped(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 允许恢复已注销(status=3)或软删除(DeletedAt有值)的用户
	if originUser.Status != common.UserStatusDeactivated && originUser.DeletedAt.Time.IsZero() {
		common.ApiErrorMsg(c, "该用户未处于注销状态")
		return
	}
	err = model.ReactivateUser(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, originUser.Id, "user.reactivate", map[string]interface{}{
		"username": originUser.Username,
		"id":       originUser.Id,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "用户已恢复",
	})
	return
}

func DeleteSelf(c *gin.Context) {
	id := c.GetInt("id")
	user, _ := model.GetUserById(id, false)

	if user.Role == common.RoleRootUser {
		common.ApiErrorI18n(c, i18n.MsgUserCannotDeleteRootUser)
		return
	}

	// 注销：标记为已注销状态，7天后自动删除
	err := model.DeactivateUser(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "账号已注销，7天后自动删除。如需恢复，请联系管理员。",
	})
	return
}

func CreateUser(c *gin.Context) {
	var user model.User
	err := json.NewDecoder(c.Request.Body).Decode(&user)
	user.Username = strings.TrimSpace(user.Username)
	if err != nil || user.Username == "" || user.Password == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := common.Validate.Struct(&user); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserInputInvalid, map[string]any{"Error": err.Error()})
		return
	}
	if user.DisplayName == "" {
		user.DisplayName = user.Username
	}
	myRole := c.GetInt("role")
	if user.Role >= myRole {
		common.ApiErrorI18n(c, i18n.MsgUserCannotCreateHigherLevel)
		return
	}
	// 捕获明文密码，Insert 后会被 hash 覆盖
	plainPassword := user.Password
	// Even for admin users, we cannot fully trust them!
	cleanUser := model.User{
		Username:           user.Username,
		Password:           user.Password,
		DisplayName:        user.DisplayName,
		Role:               user.Role, // 保持管理员设置的角色
		MustChangePassword: true,      // 管理员创建的用户首次登录必须改密
	}
	if user.Remark != "" {
		cleanUser.Remark = user.Remark
	}
	if err := cleanUser.Insert(0); err != nil {
		common.ApiError(c, err)
		return
	}

	recordManageAuditFor(c, cleanUser.Id, "user.create", map[string]interface{}{
		"username": cleanUser.Username,
		"role":     cleanUser.Role,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"username":             cleanUser.Username,
			"password":             plainPassword,
			"must_change_password": true,
		},
	})
	return
}

type ManageRequest struct {
	Id     int    `json:"id"`
	Action string `json:"action"`
	Value  int    `json:"value"`
	Mode   string `json:"mode"`
}

// ManageUser Only admin user can do this
func ManageUser(c *gin.Context) {
	var req ManageRequest
	err := json.NewDecoder(c.Request.Body).Decode(&req)

	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	user := model.User{
		Id: req.Id,
	}
	// Fill attributes
	model.DB.Unscoped().Where(&user).First(&user)
	if user.Id == 0 {
		common.ApiErrorI18n(c, i18n.MsgUserNotExists)
		return
	}
	myRole := c.GetInt("role")
	if !canManageTargetRole(myRole, user.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
		return
	}
	switch req.Action {
	case "disable":
		user.Status = common.UserStatusDisabled
		if user.Role == common.RoleRootUser {
			common.ApiErrorI18n(c, i18n.MsgUserCannotDisableRootUser)
			return
		}
	case "enable":
		user.Status = common.UserStatusEnabled
	case "delete":
		if user.Role == common.RoleRootUser {
			common.ApiErrorI18n(c, i18n.MsgUserCannotDeleteRootUser)
			return
		}
		if err := user.Delete(); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
		// 删除用户后，强制清理 Redis 中所有该用户令牌的缓存，
		// 避免已缓存的令牌在 TTL 过期前仍能通过 TokenAuth 校验。
		if err := model.InvalidateUserTokensCache(user.Id); err != nil {
			common.SysLog(fmt.Sprintf("failed to invalidate tokens cache for user %d: %s", user.Id, err.Error()))
		}
	case "promote":
		if myRole != common.RoleRootUser {
			common.ApiErrorI18n(c, i18n.MsgUserAdminCannotPromote)
			return
		}
		if user.Role >= common.RoleAdminUser {
			common.ApiErrorI18n(c, i18n.MsgUserAlreadyAdmin)
			return
		}
		user.Role = common.RoleAdminUser
	case "demote":
		if user.Role == common.RoleRootUser {
			common.ApiErrorI18n(c, i18n.MsgUserCannotDemoteRootUser)
			return
		}
		if user.Role == common.RoleCommonUser {
			common.ApiErrorI18n(c, i18n.MsgUserAlreadyCommon)
			return
		}
		user.Role = common.RoleCommonUser
	case "add_quota":
		switch req.Mode {
		case "add":
			if req.Value <= 0 {
				common.ApiErrorI18n(c, i18n.MsgUserQuotaChangeZero)
				return
			}
			if err := model.IncreaseUserQuota(user.Id, req.Value, true); err != nil {
				common.ApiError(c, err)
				return
			}
			recordManageAuditFor(c, user.Id, "user.quota_add", map[string]interface{}{
				"quota": logger.LogQuota(req.Value),
			})
		case "subtract":
			if req.Value <= 0 {
				common.ApiErrorI18n(c, i18n.MsgUserQuotaChangeZero)
				return
			}
			if err := model.DecreaseUserQuota(user.Id, req.Value, true); err != nil {
				common.ApiError(c, err)
				return
			}
			recordManageAuditFor(c, user.Id, "user.quota_subtract", map[string]interface{}{
				"quota": logger.LogQuota(req.Value),
			})
		case "override":
			oldQuota := user.Quota
			if err := model.DB.Model(&model.User{}).Where("id = ?", user.Id).Update("quota", req.Value).Error; err != nil {
				common.ApiError(c, err)
				return
			}
			recordManageAuditFor(c, user.Id, "user.quota_override", map[string]interface{}{
				"from": logger.LogQuota(oldQuota),
				"to":   logger.LogQuota(req.Value),
			})
		default:
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
		})
		return
	}

	if err := user.Update(false); err != nil {
		common.ApiError(c, err)
		return
	}
	// 禁用 / 角色调整后，强制失效用户缓存与其全部令牌缓存，
	// 避免在 Redis TTL 过期前仍使用旧状态（尤其是禁用后仍可发起请求的问题）。
	// InvalidateUserCache 会让下一次 GetUserCache 从数据库重新加载，
	// InvalidateUserTokensCache 则确保令牌侧的缓存也同步刷新。
	if req.Action == "disable" || req.Action == "promote" || req.Action == "demote" {
		if err := model.InvalidateUserCache(user.Id); err != nil {
			common.SysLog(fmt.Sprintf("failed to invalidate user cache for user %d: %s", user.Id, err.Error()))
		}
		if err := model.InvalidateUserTokensCache(user.Id); err != nil {
			common.SysLog(fmt.Sprintf("failed to invalidate tokens cache for user %d: %s", user.Id, err.Error()))
		}
	}
	recordManageAuditFor(c, user.Id, "user.manage", map[string]interface{}{
		"action":   req.Action,
		"username": user.Username,
		"id":       user.Id,
	})
	clearUser := model.User{
		Role:   user.Role,
		Status: user.Status,
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    clearUser,
	})
	return
}

type emailBindRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func EmailBind(c *gin.Context) {
	var req emailBindRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, errors.New("invalid request body"))
		return
	}
	email := req.Email
	email = model.NormalizeEmail(email)
	code := req.Code
	if !common.VerifyCodeWithKey(email, code, common.EmailVerificationPurpose) {
		common.ApiErrorI18n(c, i18n.MsgUserVerificationCodeError)
		return
	}
	session := sessions.Default(c)
	id := session.Get("id")
	user := model.User{
		Id: id.(int),
	}
	err := user.FillUserById()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.BindEmailToUser(&user, email); err != nil {
		if errors.Is(err, model.ErrEmailAlreadyTaken) {
			common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
			return
		}
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

type topUpRequest struct {
	Key string `json:"key"`
}

var topUpLocks sync.Map
var topUpCreateLock sync.Mutex

type topUpTryLock struct {
	ch chan struct{}
}

func newTopUpTryLock() *topUpTryLock {
	return &topUpTryLock{ch: make(chan struct{}, 1)}
}

func (l *topUpTryLock) TryLock() bool {
	select {
	case l.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

func (l *topUpTryLock) Unlock() {
	select {
	case <-l.ch:
	default:
	}
}

func getTopUpLock(userID int) *topUpTryLock {
	if v, ok := topUpLocks.Load(userID); ok {
		return v.(*topUpTryLock)
	}
	topUpCreateLock.Lock()
	defer topUpCreateLock.Unlock()
	if v, ok := topUpLocks.Load(userID); ok {
		return v.(*topUpTryLock)
	}
	l := newTopUpTryLock()
	topUpLocks.Store(userID, l)
	return l
}

func TopUp(c *gin.Context) {
	if !operation_setting.IsPaymentComplianceConfirmed() {
		common.ApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
		return
	}

	id := c.GetInt("id")
	lock := getTopUpLock(id)
	if !lock.TryLock() {
		common.ApiErrorI18n(c, i18n.MsgUserTopUpProcessing)
		return
	}
	defer lock.Unlock()
	req := topUpRequest{}
	err := c.ShouldBindJSON(&req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	quota, err := model.Redeem(req.Key, id)
	if err != nil {
		if errors.Is(err, model.ErrRedeemFailed) {
			common.ApiErrorI18n(c, i18n.MsgRedeemFailed)
			return
		}
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    quota,
	})
}

type UpdateUserSettingRequest struct {
	QuotaWarningType                 string  `json:"notify_type"`
	QuotaWarningThreshold            float64 `json:"quota_warning_threshold"`
	WebhookUrl                       string  `json:"webhook_url,omitempty"`
	WebhookSecret                    string  `json:"webhook_secret,omitempty"`
	NotificationEmail                string  `json:"notification_email,omitempty"`
	BarkUrl                          string  `json:"bark_url,omitempty"`
	GotifyUrl                        string  `json:"gotify_url,omitempty"`
	GotifyToken                      string  `json:"gotify_token,omitempty"`
	GotifyPriority                   int     `json:"gotify_priority,omitempty"`
	UpstreamModelUpdateNotifyEnabled *bool   `json:"upstream_model_update_notify_enabled,omitempty"`
	AcceptUnsetModelRatioModel       bool    `json:"accept_unset_model_ratio_model"`
	RecordIpLog                      bool    `json:"record_ip_log"`
}

func UpdateUserSetting(c *gin.Context) {
	var req UpdateUserSettingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	// 验证预警类型
	if req.QuotaWarningType != dto.NotifyTypeEmail && req.QuotaWarningType != dto.NotifyTypeWebhook && req.QuotaWarningType != dto.NotifyTypeBark && req.QuotaWarningType != dto.NotifyTypeGotify {
		common.ApiErrorI18n(c, i18n.MsgSettingInvalidType)
		return
	}

	// 验证预警阈值
	if req.QuotaWarningThreshold <= 0 {
		common.ApiErrorI18n(c, i18n.MsgQuotaThresholdGtZero)
		return
	}

	// 如果是webhook类型,验证webhook地址
	if req.QuotaWarningType == dto.NotifyTypeWebhook {
		if req.WebhookUrl == "" {
			common.ApiErrorI18n(c, i18n.MsgSettingWebhookEmpty)
			return
		}
		// 验证URL格式
		if _, err := url.ParseRequestURI(req.WebhookUrl); err != nil {
			common.ApiErrorI18n(c, i18n.MsgSettingWebhookInvalid)
			return
		}
	}

	// 如果是邮件类型，验证邮箱地址
	if req.QuotaWarningType == dto.NotifyTypeEmail && req.NotificationEmail != "" {
		// 验证邮箱格式
		if !strings.Contains(req.NotificationEmail, "@") {
			common.ApiErrorI18n(c, i18n.MsgSettingEmailInvalid)
			return
		}
	}

	// 如果是Bark类型，验证Bark URL
	if req.QuotaWarningType == dto.NotifyTypeBark {
		if req.BarkUrl == "" {
			common.ApiErrorI18n(c, i18n.MsgSettingBarkUrlEmpty)
			return
		}
		// 验证URL格式
		if _, err := url.ParseRequestURI(req.BarkUrl); err != nil {
			common.ApiErrorI18n(c, i18n.MsgSettingBarkUrlInvalid)
			return
		}
		// 检查是否是HTTP或HTTPS
		if !strings.HasPrefix(req.BarkUrl, "https://") && !strings.HasPrefix(req.BarkUrl, "http://") {
			common.ApiErrorI18n(c, i18n.MsgSettingUrlMustHttp)
			return
		}
	}

	// 如果是Gotify类型，验证Gotify URL和Token
	if req.QuotaWarningType == dto.NotifyTypeGotify {
		if req.GotifyUrl == "" {
			common.ApiErrorI18n(c, i18n.MsgSettingGotifyUrlEmpty)
			return
		}
		if req.GotifyToken == "" {
			common.ApiErrorI18n(c, i18n.MsgSettingGotifyTokenEmpty)
			return
		}
		// 验证URL格式
		if _, err := url.ParseRequestURI(req.GotifyUrl); err != nil {
			common.ApiErrorI18n(c, i18n.MsgSettingGotifyUrlInvalid)
			return
		}
		// 检查是否是HTTP或HTTPS
		if !strings.HasPrefix(req.GotifyUrl, "https://") && !strings.HasPrefix(req.GotifyUrl, "http://") {
			common.ApiErrorI18n(c, i18n.MsgSettingUrlMustHttp)
			return
		}
	}

	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	existingSettings := user.GetSetting()
	upstreamModelUpdateNotifyEnabled := existingSettings.UpstreamModelUpdateNotifyEnabled
	if user.Role >= common.RoleAdminUser && req.UpstreamModelUpdateNotifyEnabled != nil {
		upstreamModelUpdateNotifyEnabled = *req.UpstreamModelUpdateNotifyEnabled
	}

	// 构建设置
	settings := dto.UserSetting{
		NotifyType:                       req.QuotaWarningType,
		QuotaWarningThreshold:            req.QuotaWarningThreshold,
		UpstreamModelUpdateNotifyEnabled: upstreamModelUpdateNotifyEnabled,
		AcceptUnsetRatioModel:            req.AcceptUnsetModelRatioModel,
		RecordIpLog:                      req.RecordIpLog,
	}

	// 如果是webhook类型,添加webhook相关设置
	if req.QuotaWarningType == dto.NotifyTypeWebhook {
		settings.WebhookUrl = req.WebhookUrl
		if req.WebhookSecret != "" {
			settings.WebhookSecret = req.WebhookSecret
		}
	}

	// 如果提供了通知邮箱，添加到设置中
	if req.QuotaWarningType == dto.NotifyTypeEmail && req.NotificationEmail != "" {
		settings.NotificationEmail = req.NotificationEmail
	}

	// 如果是Bark类型，添加Bark URL到设置中
	if req.QuotaWarningType == dto.NotifyTypeBark {
		settings.BarkUrl = req.BarkUrl
	}

	// 如果是Gotify类型，添加Gotify配置到设置中
	if req.QuotaWarningType == dto.NotifyTypeGotify {
		settings.GotifyUrl = req.GotifyUrl
		settings.GotifyToken = req.GotifyToken
		// Gotify优先级范围0-10，超出范围则使用默认值5
		if req.GotifyPriority < 0 || req.GotifyPriority > 10 {
			settings.GotifyPriority = 5
		} else {
			settings.GotifyPriority = req.GotifyPriority
		}
	}

	// 更新用户设置
	user.SetSetting(settings)
	if err := user.Update(false); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUpdateFailed)
		return
	}

	common.ApiSuccessI18n(c, i18n.MsgSettingSaved, nil)
}
