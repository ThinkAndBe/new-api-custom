package controller

import (
	"bufio"
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

// RelayMcp 将 MCP（Model Context Protocol）请求透明代理到管理员配置的 MCP 渠道。
//
// 使用方式：
//  1. 管理员创建类型为 Custom(8) 的渠道，Base URL 填 MCP 服务器地址（如 https://mcp.example.com/mcp），
//     密钥填平台侧真实的 API Key，模型列表填 mcp（或用参数覆盖指定任意模型名）。
//  2. 用户只需使用 new-api 的令牌（sk-xxx），通过 Authorization: Bearer 或 x-api-key 头访问
//     POST /mcp 或 GET /mcp（SSE），请求会被代理到该渠道对应的 MCP 服务器，
//     平台密钥由网关注入，用户无需也无法接触。
//
// 渠道选择：支持在 key 后加渠道 id（sk-xxx:123）指定 MCP 渠道，否则自动选择
// 令牌分组下提供 "mcp" 模型的第一个启用渠道。
func RelayMcp(c *gin.Context) {
	channel, err := getMcpChannel(c)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"jsonrpc": "2.0",
			"error": gin.H{
				"code":    -32000,
				"message": err.Error(),
			},
			"id": nil,
		})
		return
	}

	// 目标 URL：渠道 BaseURL + 子路径（如 /mcp/sse -> {base}/sse）
	baseURL := strings.TrimSuffix(channel.GetBaseURL(), "/")
	subPath := strings.TrimPrefix(c.Param("path"), "/")
	targetURL := baseURL
	if subPath != "" {
		targetURL = baseURL + "/" + subPath
	}
	if c.Request.URL.RawQuery != "" {
		targetURL += "?" + c.Request.URL.RawQuery
	}

	var bodyBytes []byte
	if c.Request.Body != nil {
		bodyBytes, err = io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
			return
		}
	}

	proxyReq, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build proxy request: " + err.Error()})
		return
	}

	// 透传客户端请求头（跳过鉴权与 hop-by-hop 头，鉴权由网关注入）
	skipHeaders := map[string]bool{
		"Authorization":       true,
		"X-Api-Key":           true,
		"X-Goog-Api-Key":      true,
		"Host":                true,
		"Content-Length":      true,
		"Connection":          true,
		"Keep-Alive":          true,
		"Proxy-Authenticate":  true,
		"Proxy-Authorization": true,
		"Te":                  true,
		"Trailer":             true,
		"Transfer-Encoding":   true,
		"Upgrade":             true,
	}
	for k, values := range c.Request.Header {
		if skipHeaders[http.CanonicalHeaderKey(k)] {
			continue
		}
		for _, v := range values {
			proxyReq.Header.Add(k, v)
		}
	}
	if len(bodyBytes) > 0 && proxyReq.Header.Get("Content-Type") == "" {
		proxyReq.Header.Set("Content-Type", "application/json")
	}

	// 注入平台侧 MCP 服务器密钥
	channelKey, _, keyErr := channel.GetNextEnabledKey()
	if keyErr == nil && channelKey != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+channelKey)
		proxyReq.Header.Set("x-api-key", channelKey)
	}

	// 渠道级请求头覆盖（HeaderOverride 可自定义鉴权方式或附加头）
	for k, v := range channel.GetHeaderOverride() {
		if strVal, ok := v.(string); ok {
			strVal = strings.ReplaceAll(strVal, "{api_key}", channelKey)
			proxyReq.Header.Set(k, strVal)
		}
	}

	client := &http.Client{
		// MCP 的 SSE 长连接不能设整体超时，由请求 Context 控制取消
		Transport: &http.Transport{
			ResponseHeaderTimeout: 60 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
	}
	resp, err := client.Do(proxyReq)
	if err != nil {
		common.SysError(fmt.Sprintf("MCP proxy request failed (channel #%d): %s", channel.Id, err.Error()))
		c.JSON(http.StatusBadGateway, gin.H{
			"jsonrpc": "2.0",
			"error": gin.H{
				"code":    -32001,
				"message": "MCP upstream request failed: " + err.Error(),
			},
			"id": nil,
		})
		return
	}
	defer resp.Body.Close()

	// 透传响应头
	for k, values := range resp.Header {
		if skipHeaders[http.CanonicalHeaderKey(k)] {
			continue
		}
		for _, v := range values {
			c.Writer.Header().Add(k, v)
		}
	}
	c.Writer.WriteHeader(resp.StatusCode)

	// SSE 流式响应需要逐行 flush
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		flusher, ok := c.Writer.(http.Flusher)
		if !ok {
			io.Copy(c.Writer, resp.Body)
			return
		}
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				c.Writer.Write(line)
				flusher.Flush()
			}
			if err != nil {
				break
			}
		}
		return
	}
	io.Copy(c.Writer, resp.Body)
}

// getMcpChannel 选择用于 MCP 代理的渠道
func getMcpChannel(c *gin.Context) (*model.Channel, error) {
	// 优先：key 后缀指定渠道 id（sk-xxx:123，仅管理员）
	if channelId, ok := c.Get("specific_channel_id"); ok {
		if id, err := strconv.Atoi(fmt.Sprintf("%v", channelId)); err == nil && id > 0 {
			ch, err := model.GetChannelById(id, true)
			if err != nil {
				return nil, fmt.Errorf("指定的 MCP 渠道 #%d 不存在", id)
			}
			if ch.Status != common.ChannelStatusEnabled {
				return nil, fmt.Errorf("指定的 MCP 渠道 #%d 未启用", id)
			}
			return ch, nil
		}
	}

	// 自动选择：令牌分组下提供 "mcp" 模型的启用渠道
	channel, _, selectErr := service.CacheGetRandomSatisfiedChannel(&service.RetryParam{
		Ctx:        c,
		TokenGroup: common.GetContextKeyString(c, constant.ContextKeyTokenGroup),
		ModelName:  "mcp",
	})
	if selectErr != nil {
		return nil, fmt.Errorf("当前分组下没有可用的 MCP 渠道（请在渠道模型列表中添加 mcp 模型）: %s", selectErr.Error())
	}
	if channel == nil {
		return nil, fmt.Errorf("当前分组下没有可用的 MCP 渠道（请在渠道模型列表中添加 mcp 模型）")
	}
	// 缓存中的渠道对象可能缺少部分字段（如 base_url），重新完整加载
	fullChannel, err := model.GetChannelById(channel.Id, true)
	if err != nil {
		return nil, fmt.Errorf("加载 MCP 渠道 #%d 失败: %s", channel.Id, err.Error())
	}
	return fullChannel, nil
}
