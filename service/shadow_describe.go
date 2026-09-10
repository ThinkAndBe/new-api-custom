package service

// shadow_describe.go — 影子代码库项目功能描述生成。
//
// 物化器发现项目后，取该项目关联的对话摘录（按抽取记录的 request_id 反查
// chat_logs）与文件清单，通过回环调用网关自身 /v1/chat/completions 用便宜
// 模型生成 2-3 句功能介绍，写入 shadow_projects。24 小时自动刷新，可手动
// 重新生成。

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const shadowDescribeInterval = 24 * 3600

var (
	shadowDescribeModel   string
	shadowDescribeBase    string
	shadowTokenKey        string
	shadowTokenCheckedAt  int64
	shadowDescribeEnvOnce = false
)

func loadShadowDescribeEnv() {
	if shadowDescribeEnvOnce {
		return
	}
	shadowDescribeEnvOnce = true
	shadowDescribeModel = os.Getenv("SHADOW_DESCRIBE_MODEL")
	if shadowDescribeModel == "" {
		shadowDescribeModel = "minimax-m3"
	}
	shadowDescribeBase = os.Getenv("SHADOW_DESCRIBE_BASE")
	if shadowDescribeBase == "" {
		// 生产容器内 127.0.0.1:3000 未必可达（3000 前有 TLS 代理等拓扑差异），
		// 默认走系统配置的对外地址（客户端正在用的稳定通道），回环兜底
		if sa := strings.TrimSpace(system_setting.ServerAddress); sa != "" {
			shadowDescribeBase = sa
		} else {
			shadowDescribeBase = "http://127.0.0.1:3000"
		}
	}
	common.SysLog("shadow: describe base = " + shadowDescribeBase + " model = " + shadowDescribeModel)
}

// ensureShadowInternalToken 确保 root 用户名下有内部令牌（回环调用用）
func ensureShadowInternalToken() (string, error) {
	loadShadowDescribeEnv()
	if shadowTokenKey != "" && time.Now().Unix()-shadowTokenCheckedAt < 600 {
		return shadowTokenKey, nil
	}
	// 找 root
	var root model.User
	if err := model.DB.Where("role = ?", common.RoleRootUser).Order("id asc").First(&root).Error; err != nil {
		return "", fmt.Errorf("root user not found: %w", err)
	}
	const name = "__shadow_internal__"
	// 令牌分组跟随 root 用户分组（空分组会导致路由 503）
	rootGroup := root.Group
	if rootGroup == "" {
		rootGroup = "default"
	}
	var tokens []*model.Token
	if err := model.DB.Where("user_id = ? AND name = ?", root.Id, name).Find(&tokens).Error; err != nil || len(tokens) == 0 {
		tk := &model.Token{
			UserId: root.Id, Name: name, Key: common.GetUUID(),
			CreatedTime: common.GetTimestamp(), AccessedTime: common.GetTimestamp(),
			ExpiredTime: -1, RemainQuota: 0, UnlimitedQuota: true,
			Status: common.TokenStatusEnabled, Group: rootGroup,
		}
		if err := model.DB.Create(tk).Error; err != nil {
			return "", err
		}
		shadowTokenKey = tk.Key
	} else {
		tk := tokens[0]
		if tk.Group == "" {
			model.DB.Model(tk).Update("group", rootGroup)
		}
		shadowTokenKey = tk.Key
	}
	shadowTokenCheckedAt = time.Now().Unix()
	return shadowTokenKey, nil
}

// RedescribeProject 手动重新生成描述（忽略 24h 节流）
func RedescribeProject(userId int, username, project string) error {
	if username == "" {
		username = fmt.Sprintf("user-%d", userId)
	}
	return describeProject(userId, username, project)
}

// MaybeDescribeProject 物化后钩子：缺描述或超 24h 时异步生成（每轮限流 5 个）
func MaybeDescribeProject(userId int, username, project string) {
	meta, err := model.GetShadowProject(userId, project)
	now := common.GetTimestamp()
	if err == nil && meta.DescribedAt > 0 && now-meta.DescribedAt < shadowDescribeInterval {
		return
	}
	go func() {
		if err := describeProject(userId, username, project); err != nil {
			common.SysLog(fmt.Sprintf("shadow: describe %s/%s failed: %s", username, project, err.Error()))
		}
	}()
}

func describeProject(userId int, username, project string) error {
	key, err := ensureShadowInternalToken()
	if err != nil {
		return err
	}
	// 1. 取项目文件清单（前 60 个路径）
	files, err := model.ShadowRepoFiles(userId, project)
	if err != nil {
		return err
	}
	var fileList []string
	for i, f := range files {
		if i >= 60 {
			break
		}
		fileList = append(fileList, f.FilePath)
	}

	// 2. 取关联对话摘录：该项目的 request_id → chat_logs 内容
	reqIds, err := model.ShadowProjectRequestIds(userId, project, 20)
	if err != nil {
		return err
	}
	excerpts := model.ShadowConversationExcerpts(reqIds, 30, 600)

	// 3. 组 prompt
	var sb strings.Builder
	sb.WriteString("以下是一个开发项目的信息，请用中文 2-3 句话概括这个项目的功能与用途（它做什么、给谁用）。只输出概括本身。\n\n")
	sb.WriteString("文件清单：\n")
	for _, f := range fileList {
		sb.WriteString("- " + f + "\n")
	}
	if len(excerpts) > 0 {
		sb.WriteString("\n相关对话摘录：\n")
		for _, e := range excerpts {
			sb.WriteString("- " + e + "\n")
		}
	}

	desc, err := callShadowLLM(key, sb.String())
	if err != nil {
		return err
	}
	desc = strings.TrimSpace(desc)
	if len(desc) > 800 {
		desc = desc[:800]
	}
	model.SaveShadowProjectDescription(userId, project, desc)
	common.SysLog(fmt.Sprintf("shadow: project %s/%s described (%d chars)", username, project, len(desc)))
	return nil
}

// callShadowLLM 回环调用网关 chat/completions
func callShadowLLM(key, prompt string) (string, error) {
	loadShadowDescribeEnv()
	content, err := callShadowLLMOnce(shadowDescribeBase, key, prompt)
	if err == nil {
		return content, nil
	}
	// 对外地址失败时回环兜底（本机直跑/内网部署场景）
	fallback := "http://127.0.0.1:3000"
	if shadowDescribeBase == fallback {
		return "", err
	}
	common.SysLog("shadow: describe via " + shadowDescribeBase + " failed: " + err.Error() + "，尝试回环 " + fallback)
	return callShadowLLMOnce(fallback, key, prompt)
}

func callShadowLLMOnce(base, key, prompt string) (string, error) {
	body := fmt.Sprintf(`{"model":%q,"max_tokens":1200,"messages":[{"role":"user","content":%q}]}`,
		shadowDescribeModel, prompt)
	req, err := http.NewRequest("POST", base+"/v1/chat/completions",
		strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(raw)[:min(200, len(raw))])
	}
	var out map[string]interface{}
	if err := common.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	choices, _ := out["choices"].([]interface{})
	if len(choices) == 0 {
		return "", fmt.Errorf("no choices")
	}
	msg, _ := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	content, _ := msg["content"].(string)
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("empty content")
	}
	return content, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
