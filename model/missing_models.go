package model

// GetMissingModels returns model names that are referenced in the system
// but do not have corresponding records in the models meta table.
// This includes:
// 1. Models from enabled channels (GetEnabledModels)
// 2. Models from ALL channels (including disabled) — so newly added models
//    in disabled channels also appear in model management for configuration
func GetMissingModels() ([]string, error) {
	// 1. 获取所有渠道的模型（包括禁用渠道，确保新增模型即使渠道禁用也能被发现）
	var allChannelModels []string
	if err := DB.Model(&Channel{}).Where("status <> ?", 4).Pluck("models", &allChannelModels).Error; err != nil {
		return nil, err
	}

	// 解析逗号分隔的模型列表
	modelSet := make(map[string]struct{})
	for _, modelsStr := range allChannelModels {
		for _, m := range splitModels(modelsStr) {
			if m != "" {
				modelSet[m] = struct{}{}
			}
		}
	}

	if len(modelSet) == 0 {
		return []string{}, nil
	}

	// 2. 查询已有的元数据模型名
	var existing []string
	modelNames := make([]string, 0, len(modelSet))
	for m := range modelSet {
		modelNames = append(modelNames, m)
	}
	if err := DB.Model(&Model{}).Where("model_name IN ?", modelNames).Pluck("model_name", &existing).Error; err != nil {
		return nil, err
	}

	existingSet := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		existingSet[e] = struct{}{}
	}

	// 3. 收集缺失模型
	var missing []string
	for name := range modelSet {
		if _, ok := existingSet[name]; !ok {
			missing = append(missing, name)
		}
	}
	return missing, nil
}

// splitModels 按逗号分割模型字符串并清理空白
func splitModels(s string) []string {
	var result []string
	current := ""
	for _, c := range s {
		if c == ',' {
			if trimmed := trimSpace(current); trimmed != "" {
				result = append(result, trimmed)
			}
			current = ""
		} else {
			current += string(c)
		}
	}
	if trimmed := trimSpace(current); trimmed != "" {
		result = append(result, trimmed)
	}
	return result
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
