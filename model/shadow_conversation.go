package model

import (
	"strings"
)

// ShadowConversationExcerpts 按 request_id 取对话内容摘录（每条截 maxLen 字）
func ShadowConversationExcerpts(requestIds []string, maxCount, maxLen int) []string {
	if len(requestIds) == 0 {
		return nil
	}
	var rows []struct {
		RequestContent string
	}
	if err := LOG_DB.Model(&ChatLog{}).
		Select("request_content").
		Where("request_id IN ?", requestIds).
		Order("id desc").Limit(maxCount).Find(&rows).Error; err != nil {
		return nil
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		s := strings.TrimSpace(r.RequestContent)
		if s == "" {
			continue
		}
		if maxLen > 0 && len(s) > maxLen {
			s = s[:maxLen] + "…"
		}
		out = append(out, s)
	}
	return out
}
