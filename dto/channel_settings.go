package dto

type ChannelSettings struct {
	ForceFormat            bool   `json:"force_format,omitempty"`
	ThinkingToContent      bool   `json:"thinking_to_content,omitempty"`
	Proxy                  string `json:"proxy"`
	PassThroughBodyEnabled bool   `json:"pass_through_body_enabled,omitempty"`
	SystemPrompt           string `json:"system_prompt,omitempty"`
	SystemPromptOverride   bool   `json:"system_prompt_override,omitempty"`
	HeadroomEnabled        bool   `json:"headroom_enabled,omitempty"`
	HeadroomURL            string `json:"headroom_url,omitempty"`
	// MaxConcurrency 该渠道允许的最大并发请求数，0 表示不限制。
	// 渠道在途请求达到上限时，分发器会跳过该渠道选择其他可用渠道。
	MaxConcurrency int `json:"max_concurrency,omitempty"`
}

type VertexKeyType string

const (
	VertexKeyTypeJSON   VertexKeyType = "json"
	VertexKeyTypeAPIKey VertexKeyType = "api_key"
)

type AwsKeyType string

const (
	AwsKeyTypeAKSK   AwsKeyType = "ak_sk"
	AwsKeyTypeApiKey AwsKeyType = "api_key"
)

type SchedulePauseRule struct {
	Days   []int  `json:"days"`
	Start  string `json:"start"`
	End    string `json:"end"`
	Reason string `json:"reason,omitempty"`
}

type ChannelOtherSettings struct {
	AzureResponsesVersion                 string              `json:"azure_responses_version,omitempty"`
	VertexKeyType                         VertexKeyType       `json:"vertex_key_type,omitempty"`
	OpenRouterEnterprise                  *bool               `json:"openrouter_enterprise,omitempty"`
	ClaudeBetaQuery                       bool                `json:"claude_beta_query,omitempty"`
	AllowServiceTier                      bool                `json:"allow_service_tier,omitempty"`
	AllowInferenceGeo                     bool                `json:"allow_inference_geo,omitempty"`
	AllowSpeed                            bool                `json:"allow_speed,omitempty"`
	AllowSafetyIdentifier                 bool                `json:"allow_safety_identifier,omitempty"`
	DisableStore                          bool                `json:"disable_store,omitempty"`
	AllowIncludeObfuscation               bool                `json:"allow_include_obfuscation,omitempty"`
	AwsKeyType                            AwsKeyType          `json:"aws_key_type,omitempty"`
	UpstreamModelUpdateCheckEnabled       bool                `json:"upstream_model_update_check_enabled,omitempty"`
	UpstreamModelUpdateAutoSyncEnabled    bool                `json:"upstream_model_update_auto_sync_enabled,omitempty"`
	UpstreamModelUpdateLastCheckTime      int64               `json:"upstream_model_update_last_check_time,omitempty"`
	UpstreamModelUpdateLastDetectedModels []string            `json:"upstream_model_update_last_detected_models,omitempty"`
	UpstreamModelUpdateLastRemovedModels  []string            `json:"upstream_model_update_last_removed_models,omitempty"`
	UpstreamModelUpdateIgnoredModels      []string            `json:"upstream_model_update_ignored_models,omitempty"`
	SchedulePauseEnabled                  bool                `json:"schedule_pause_enabled,omitempty"`
	SchedulePauseRules                    []SchedulePauseRule `json:"schedule_pause_rules,omitempty"`
	HealthCheckEnabled                    bool                `json:"health_check_enabled,omitempty"`
	HealthCheckIntervalMinutes            int                 `json:"health_check_interval_minutes,omitempty"`
	HealthCheckFailureThreshold           int                 `json:"health_check_failure_threshold,omitempty"`
	// HealthCheckDisabled opt-out 语义：true=禁止该渠道参与自动恢复探测
	HealthCheckDisabled                   bool                `json:"health_check_disabled,omitempty"`
}

func (s *ChannelOtherSettings) IsOpenRouterEnterprise() bool {
	if s == nil || s.OpenRouterEnterprise == nil {
		return false
	}
	return *s.OpenRouterEnterprise
}
