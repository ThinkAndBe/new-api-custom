package model

import (
	"github.com/QuantumNous/new-api/common"
)

// modelParamsSeedData 是国产主流模型的参数初始值。
// 数据来源：web/classic/src/pages/UsageGuide/index.jsx 的 MODEL_PARAMS 表（原七牛云 AI 模型广场数据）。
// 用于首次升级时给现有 models 表行补齐能力参数，避免上线后所有模型参数都为 0/默认。
// 注：这些行 ParamsLocked=false，后续仍可被 litellm 刷新覆盖（若 litellm 有更新值）。
var modelParamsSeedData = map[string]struct {
	In       int
	Out      int
	Tools    bool
	Vision   bool
	Reasoning bool
}{
	// ========== 智谱 GLM ==========
	"glm-5.2":         {In: 1000000, Out: 128000, Tools: true, Reasoning: true},
	"glm-5.1":         {In: 200000, Out: 128000, Tools: true},
	"glm-5":           {In: 200000, Out: 128000, Tools: true, Reasoning: true},
	"glm-4.7":         {In: 200000, Out: 200000, Tools: true, Reasoning: true},
	"glm-4.7-flash":   {In: 200000, Out: 200000, Tools: true, Reasoning: true},
	"glm-4.6":         {In: 200000, Out: 200000, Tools: true, Reasoning: true},
	"glm-4.5":         {In: 131072, Out: 98304, Tools: true},
	// ========== DeepSeek ==========
	"deepseek-v4-pro":   {In: 1000000, Out: 384000, Tools: true, Reasoning: true},
	"deepseek-v4-flash": {In: 1000000, Out: 384000, Tools: true, Reasoning: true},
	"deepseek-v3.2":     {In: 128000, Out: 32000, Tools: true},
	"deepseek-v3.1":     {In: 128000, Out: 32000, Tools: true},
	"deepseek-v3":       {In: 128000, Out: 16000, Tools: true},
	"deepseek-r1":       {In: 128000, Out: 32000, Tools: true, Reasoning: true},
	// ========== 阿里 Qwen ==========
	"qwen3.7-max":         {In: 1000000, Out: 65536, Tools: true, Reasoning: true},
	"qwen3.8-max-preview": {In: 1000000, Out: 65536, Tools: true, Reasoning: true},
	"qwen3.6-plus":     {In: 1000000, Out: 65536, Tools: true, Vision: true, Reasoning: true},
	"qwen3.6-27b":      {In: 262100, Out: 262100, Tools: true, Vision: true, Reasoning: true},
	"qwen3.5-plus":     {In: 1000000, Out: 65536, Tools: true, Vision: true, Reasoning: true},
	"qwen3-max":        {In: 262144, Out: 65536, Tools: true, Reasoning: true},
	"qwen3-coder-plus": {In: 262000, Out: 65536, Tools: true, Reasoning: true},
	"qwen3-coder":      {In: 262000, Out: 65536, Tools: true, Reasoning: true},
	"qwen3-plus":       {In: 1000000, Out: 65536, Tools: true, Reasoning: true},
	"qwen-turbo":       {In: 1000000, Out: 8192, Tools: true, Reasoning: true},
	"qwen-plus":        {In: 1000000, Out: 65536, Tools: true, Reasoning: true},
	"qwen3-235b":       {In: 128000, Out: 32000, Tools: true, Reasoning: true},
	"qwen3-32b":        {In: 131072, Out: 32768, Tools: true, Reasoning: true},
	// ========== Kimi ==========
	"kimi-k2.7-code":   {In: 262144, Out: 262144, Tools: true, Vision: true, Reasoning: true},
	"kimi-k2.6":        {In: 262000, Out: 262000, Tools: true, Vision: true, Reasoning: true},
	"kimi-k2.5":        {In: 256000, Out: 256000, Tools: true, Vision: true, Reasoning: true},
	"kimi-k2-thinking": {In: 256000, Out: 100000, Tools: true, Reasoning: true},
	"kimi-k2":          {In: 128000, Out: 128000, Tools: true},
	// ========== MiniMax ==========
	"minimax-m3":   {In: 1000000, Out: 128000, Tools: true, Vision: true, Reasoning: true},
	"minimax-m2.7": {In: 204800, Out: 128000, Tools: true, Reasoning: true},
	"minimax-m2.5": {In: 204800, Out: 128000, Tools: true, Reasoning: true},
	"minimax-m2.1": {In: 204800, Out: 128000, Tools: true, Reasoning: true},
	"minimax-m2":   {In: 200000, Out: 128000, Tools: true, Reasoning: true},
	// ========== 豆包 Doubao ==========
	"doubao-seed-2.0-pro":  {In: 256000, Out: 128000, Tools: true, Vision: true, Reasoning: true},
	"doubao-seed-2.0-code": {In: 256000, Out: 128000, Tools: true, Vision: true, Reasoning: true},
	"doubao-seed-2.0-mini": {In: 256000, Out: 32000, Tools: true, Vision: true, Reasoning: true},
	"doubao-seed-1.6":      {In: 256000, Out: 32000, Tools: true, Vision: true, Reasoning: true},
	"doubao-1.5-vision":    {In: 128000, Out: 16000, Vision: true},
	"doubao-1.5-thinking":  {In: 128000, Out: 16000, Tools: true, Reasoning: true},
	// ========== 其他 ==========
	"hy3-preview":   {In: 262144, Out: 262144, Tools: true, Reasoning: true},
	"longcat-flash": {In: 256000, Out: 320000, Tools: true, Reasoning: true},
}

// SeedModelParams 在启动时（仅 Master 节点）一次性给现有 models 表行补齐能力参数。
// 只更新 max_input_tokens=0 的行（即从未填过参数的模型），避免覆盖已有数据。
// 这是一个幂等的一次性种子：跑过一次后所有目标行 max_input_tokens!=0，不会再被命中。
func SeedModelParams() (int64, error) {
	var totalCount int64
	for modelName, p := range modelParamsSeedData {
		// 仅更新 max_input_tokens=0 且 ParamsLocked=false 的同名行
		result := DB.Model(&Model{}).
			Where("model_name = ? AND max_input_tokens = 0 AND (params_locked = false OR params_locked IS NULL)", modelName).
			Updates(map[string]interface{}{
				"max_input_tokens":     p.In,
				"max_output_tokens":    p.Out,
				"supports_tool_call":   p.Tools,
				"supports_images":      p.Vision,
				"supports_reasoning":   p.Reasoning,
				"params_locked":        false, // 种子数据未锁定，后续可被 litellm 刷新覆盖或管理员手动改
			})
		totalCount += result.RowsAffected
		if result.Error != nil {
			common.SysError("SeedModelParams 更新 " + modelName + " 失败: " + result.Error.Error())
		}
	}
	return totalCount, nil
}
