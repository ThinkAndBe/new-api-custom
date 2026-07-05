package operation_setting

import "strings"

var DemoSiteEnabled = false
var SelfUseModeEnabled = false
var HeadroomGlobalEnabled = true

// HeadroomRetentionDays Headroom 压缩统计数据的留存天数（查询时自动过滤超出此范围的数据）
var HeadroomRetentionDays = 30

// 自用模式 - 自动获取官方价格相关配置
// AutoSyncOfficialRatioEnabled 是否启用自动同步官方价格（仅自用模式下有意义）
var AutoSyncOfficialRatioEnabled = false

// OfficialRatioSyncIntervalHours 自动同步间隔（小时），默认 24 小时
var OfficialRatioSyncIntervalHours = 24

// OfficialRatioSyncSources 价格来源及优先级，逗号分隔
// official = basellm.github.io（官方倍率预设），modelsdev = models.dev
// 默认 modelsdev 优先（国内可直接访问），official 作为兜底
var OfficialRatioSyncSources = "modelsdev,official"

// OfficialRatioOverwriteExisting 是否覆盖本地已有的模型价格（false=仅填充缺失，true=覆盖已有）
var OfficialRatioOverwriteExisting = false

// OfficialRatioRMBCorrection 国产模型 RMB 修正系数。
// models.dev 的价格是 USD 国际版定价，国内 RMB 官方定价通常更低。
// 实际 ratio = models.dev_ratio × RMBCorrection
// 例如 GLM-5.2: models.dev $1.4/1M → ratio=0.7，国内 ¥8/1M → ratio=0.548，修正系数≈0.78
var OfficialRatioRMBCorrection = 0.78

// OfficialRatioLastSyncTime 上次成功同步的 Unix 时间戳（仅用于展示，不持久化到默认值）
var OfficialRatioLastSyncTime int64 = 0

// OfficialRatioLastSyncCount 上次成功同步的模型数量
var OfficialRatioLastSyncCount int = 0

var AutomaticDisableKeywords = []string{
	"Your credit balance is too low",
	"This organization has been disabled.",
	"You exceeded your current quota",
	"Permission denied",
	"The security token included in the request is invalid",
	"Operation not allowed",
	"Your account is not authorized",
}

func AutomaticDisableKeywordsToString() string {
	return strings.Join(AutomaticDisableKeywords, "\n")
}

func AutomaticDisableKeywordsFromString(s string) {
	AutomaticDisableKeywords = []string{}
	ak := strings.Split(s, "\n")
	for _, k := range ak {
		k = strings.TrimSpace(k)
		k = strings.ToLower(k)
		if k != "" {
			AutomaticDisableKeywords = append(AutomaticDisableKeywords, k)
		}
	}
}
