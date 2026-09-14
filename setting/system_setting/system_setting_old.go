package system_setting

var ServerAddress = "http://localhost:3000"

// aTrust 零信任免认证
var (
	ATrustEnabled   = false
	ATrustServer    = "" // 如 https://10.0.0.1:4433
	ATrustAPIId     = ""
	ATrustAPISecret = ""
)
var WorkerUrl = ""
var WorkerValidKey = ""
var WorkerAllowHttpImageRequestEnabled = false

func EnableWorker() bool {
	return WorkerUrl != ""
}
