package operation_setting

import "strings"

var DemoSiteEnabled = false
var SelfUseModeEnabled = false

// 自用模式 - 自动获取官方价格相关配置
// AutoSyncOfficialRatioEnabled 是否启用自动同步官方价格（仅自用模式下有意义）
var AutoSyncOfficialRatioEnabled = false
// OfficialRatioSyncIntervalHours 自动同步间隔（小时），默认 24 小时
var OfficialRatioSyncIntervalHours = 24
// OfficialRatioSyncSources 价格来源及优先级，逗号分隔
// official = basellm.github.io（官方倍率预设），modelsdev = models.dev
var OfficialRatioSyncSources = "official,modelsdev"
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
