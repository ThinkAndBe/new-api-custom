package system_setting

var ServerAddress = "http://localhost:3000"

// GuideAPIBase 使用教程/配置工具下发的 API 地址。
// 工具（zcode 等）是非浏览器调用，443 会被零信任拦截，必须走 3000 直连；
// 留空时从 ServerAddress 推导并自动补 :3000 端口。
var GuideAPIBase = ""

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
	ATrustSSOServer       = "" // aTrust 客户端接入地址（浏览器跳转 auth2ssoLogin 用），如 https://atrust.example.com
	ATrustSSOAPIServer    = "" // 换用户信息接口地址（服务端签名调用），留空回落到 ATrustSSOServer；分体部署时填控制中心地址
	ATrustSSOAppId        = "" // 应用开启单点登录后获取的 appid
	ATrustSSOAppSecret    = ""
	ATrustSSOAutoRegister = true // 未匹配到已有账号时自动创建（零信任登录即员工）
	ATrustProxyIPs        = ""   // aTrust 反代出口 IP（逗号分隔），命中才对未登录访客自动跳转 SSO
)

// aTrust 用户目录（工号同步数据源）
var (
	ATrustDirectoryDomain = "wechat33610" // 目录标识（企业微信同步目录；getUserStatus 响应的 domain 字段）
	ATrustSyncRole        = "AI用户"       // 只同步该零信任角色的成员（tokenhub 访问白名单）
	ATrustSyncPaths       = ""            // 组织路径过滤（逗号分隔前缀，如 /鸿星尔克实业/信息管理中心；空=不过滤）
	ATrustSyncEmployeeIds = ""            // 工号白名单（逗号分隔；非空时只同步名单内成员，用于收敛角色继承导致的超范围）

	ATrustSyncGroups      = ""            // 额外限定本地分组（逗号分隔；留空=全部，范围已由角色决定）
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
