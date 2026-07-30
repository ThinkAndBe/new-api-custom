package controller

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// mcpJsonRpcRequest 构造 JSON-RPC 请求体
func mcpJsonRpcRequest(id int, method string, params string) string {
	if params == "" {
		params = "{}"
	}
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"%s","params":%s}`, id, method, params)
}

// mcpDoRpc 向 MCP 渠道上游发一次 JSON-RPC 调用，返回响应 body。
// 鉴权注入逻辑与 RelayMcp 保持一致：Authorization Bearer + x-api-key + HeaderOverride。
func mcpDoRpc(channel *model.Channel, body string) ([]byte, error) {
	baseURL := strings.TrimSuffix(channel.GetBaseURL(), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("MCP 渠道未配置 Base URL")
	}
	req, err := http.NewRequest(http.MethodPost, baseURL, bytes.NewReader([]byte(body)))
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %s", err.Error())
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	channelKey, _, keyErr := channel.GetNextEnabledKey()
	if keyErr == nil && channelKey != "" {
		req.Header.Set("Authorization", "Bearer "+channelKey)
		req.Header.Set("x-api-key", channelKey)
	}
	for k, v := range channel.GetHeaderOverride() {
		if strVal, ok := v.(string); ok {
			strVal = strings.ReplaceAll(strVal, "{api_key}", channelKey)
			req.Header.Set(k, strVal)
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("上游请求失败: %s", err.Error())
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %s", err.Error())
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("上游返回 HTTP %d: %s", resp.StatusCode, string(respBody[:min(len(respBody), 300)]))
	}
	return respBody, nil
}

// mcpParseToolsResponse 从 tools/list 响应中提取 tools 数组（兼容 SSE data: 包装）
func mcpParseToolsResponse(body []byte) ([]interface{}, error) {
	// 处理 SSE 响应：取最后一个 data: 行
	bodyStr := string(body)
	if strings.Contains(bodyStr, "data:") {
		var lastData string
		for _, line := range strings.Split(bodyStr, "\n") {
			if strings.HasPrefix(line, "data:") {
				lastData = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
		if lastData != "" {
			body = []byte(lastData)
		}
	}
	var parsed struct {
		Result struct {
			Tools []interface{} `json:"tools"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := common.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("解析 JSON-RPC 响应失败: %s", err.Error())
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("JSON-RPC 错误 %d: %s", parsed.Error.Code, parsed.Error.Message)
	}
	return parsed.Result.Tools, nil
}

// TestMcpChannel 对 MCP 渠道执行 initialize → notifications/initialized → tools/list 完整握手，
// 返回工具清单。成功时把工具清单缓存到渠道 other_info.mcp_tools 供用户侧展示。
func TestMcpChannel(channel *model.Channel) testResult {
	// 1. initialize
	initBody := mcpJsonRpcRequest(1, "initialize", `{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"new-api-channel-test","version":"1.0.0"}}`)
	if _, err := mcpDoRpc(channel, initBody); err != nil {
		return testResult{localErr: fmt.Errorf("MCP initialize 失败: %s", err.Error())}
	}
	// 2. notifications/initialized（部分 server 要求，失败不致命）
	notifBody := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	_, _ = mcpDoRpc(channel, notifBody)
	// 3. tools/list
	toolsBody := mcpJsonRpcRequest(2, "tools/list", "")
	respBody, err := mcpDoRpc(channel, toolsBody)
	if err != nil {
		return testResult{localErr: fmt.Errorf("MCP tools/list 失败: %s", err.Error())}
	}
	tools, err := mcpParseToolsResponse(respBody)
	if err != nil {
		return testResult{localErr: fmt.Errorf("MCP tools/list 解析失败: %s", err.Error())}
	}
	channel.SetMcpTools(tools)
	return testResult{localErr: nil}
}

// GetMcpChannelTools 管理员接口：测试 MCP 渠道并返回工具清单。
// GET /api/channel/:id/mcp_tools
func GetMcpChannelTools(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	channel, err := model.GetChannelById(channelId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !isMcpChannel(channel) {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "该渠道不是 MCP 类型"})
		return
	}
	tik := time.Now()
	result := TestMcpChannel(channel)
	consumed := float64(time.Since(tik).Milliseconds()) / 1000.0
	if result.localErr != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": result.localErr.Error(), "time": consumed})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"time":    consumed,
		"data": gin.H{
			"tools": channel.GetMcpTools(),
		},
	})
}

// GetUserMcpServers 用户接口：返回当前用户分组下可用的 MCP 渠道列表（含缓存的工具清单）。
// 不暴露 base_url 和 key——用户通过 /mcp 网关入口 + 自己的令牌访问。
// GET /api/user/mcp_servers
func GetUserMcpServers(c *gin.Context) {
	id := c.GetInt("id")
	user, err := model.GetUserCache(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var groups map[string]string
	if model.IsAdmin(id) {
		groups = service.GetUserUsableGroups(user.Group)
	} else {
		groups = service.GetUserOwnedGroups(user.Group)
	}

	var channels []*model.Channel
	// 兼容：type=58（标准 MCP）+ type=8 且 models 含 mcp（迁移前的历史"假 MCP"渠道）
	if err := model.DB.Where(
		"type = ? OR (type = ? AND (',' || REPLACE(models, ' ', '') || ',') LIKE ?)",
		constant.ChannelTypeMCP, constant.ChannelTypeCustom, "%,mcp,%",
	).Where("status = ?", common.ChannelStatusEnabled).
		Find(&channels).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	type mcpServerItem struct {
		Id          int           `json:"id"`
		Name        string        `json:"name"`
		ServiceName string        `json:"service_name"`
		Description string        `json:"description"`
		Tools       []interface{} `json:"tools"`
	}
	items := make([]mcpServerItem, 0)
	for _, ch := range channels {
		// 渠道分组与用户分组求交集
		matched := false
		for _, g := range strings.Split(ch.Group, ",") {
			if _, ok := groups[strings.TrimSpace(g)]; ok {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		tools := ch.GetMcpTools()
		if tools == nil {
			tools = []interface{}{}
		}
		desc := ""
		if ch.Remark != nil {
			desc = *ch.Remark
		}
		sn := strings.TrimSpace(ch.MCPServiceName)
		if sn == "" {
			sn = "mcp"
		}
		items = append(items, mcpServerItem{
			Id:          ch.Id,
			Name:        ch.Name,
			ServiceName: sn,
			Description: desc,
			Tools:       tools,
		})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": items})
}
