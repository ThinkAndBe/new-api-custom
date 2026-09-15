package system_setting

var ServerAddress = "http://localhost:3000"

// aTrust 零信任免认证
var (
	ATrustEnabled   = false
	ATrustServer    = "" // 如 https://10.0.0.1:4433
	ATrustAPIId     = ""
	ATrustAPISecret = ""
)

// aTrust 反向 OAuth2 单点登录（官方「票据共享-反向oauth2」方案，
// 详见《零信任aTrust资源单点登录方案-OAuth对接&票据注入》章节5）
var (
	ATrustSSOEnabled      = false
	ATrustSSOServer       = "" // aTrust 客户端接入地址，如 https://atrust.example.com
	ATrustSSOAppId        = "" // 应用开启单点登录后获取的 appid
	ATrustSSOAppSecret    = ""
	ATrustSSOAutoRegister = true // 未匹配到已有账号时自动创建（零信任登录即员工）
	ATrustProxyIPs        = ""   // aTrust 反代出口 IP（逗号分隔），命中才对未登录访客自动跳转 SSO
)

// 企业微信扫码登录（aTrust 反代不透传用户身份，改用企微 OAuth 实现免密登录）
var (
	WeChatWorkAuthEnabled  = false
	WeChatWorkCorpID       = "" // 企业 ID
	WeChatWorkAgentID      = "" // 自建应用 AgentId
	WeChatWorkSecret       = "" // 自建应用 Secret
	WeChatWorkAutoRegister = false // 未匹配到已有账号时是否自动创建
)
var WorkerUrl = ""
var WorkerValidKey = ""
var WorkerAllowHttpImageRequestEnabled = false

func EnableWorker() bool {
	return WorkerUrl != ""
}
