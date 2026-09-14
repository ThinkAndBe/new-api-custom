package system_setting

var ServerAddress = "http://localhost:3000"

// aTrust 零信任免认证
var (
	ATrustEnabled   = false
	ATrustServer    = "" // 如 https://10.0.0.1:4433
	ATrustAPIId     = ""
	ATrustAPISecret = ""
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
