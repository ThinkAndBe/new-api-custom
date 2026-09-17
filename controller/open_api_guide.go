package controller

// open_api_guide.go — 面向 AI 智能体的使用说明（数据开放接口）。
//
// 说明文本随接口一起发布，并注入「当前密钥」的真实数据范围，顾问只要把密钥和本文档
// 一起交给智能体，智能体即可自助完成：拉数据 → 交叉核对 → 产出合规审查结论。
//   GET /api/open/guide          本文档（text/markdown，format=json 返回结构化）
//   GET /api/open/openapi.json   五个查询端点的 OpenAPI 3.0 描述（供支持工具导入的智能体）
//
// 实现提示：文档正文用反引号包裹的原生字符串承载，其中的 markdown 反引号统一写成 ~~
// 占位符，最后 ReplaceAll 还原——否则会提前结束 Go 字符串。

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

func timeNowMinusDays(days int) string {
	return time.Now().AddDate(0, 0, -days).Format("2006-01-02")
}

func timeNowDate() string { return time.Now().Format("2006-01-02") }

// openBaseURL 依据反向代理头还原站点基地址（aTrust/nginx 场景下 Host 与协议可能被改）
func openBaseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if p := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); p != "" {
		scheme = strings.Split(p, ",")[0]
	}
	host := c.Request.Host
	if h := strings.TrimSpace(c.GetHeader("X-Forwarded-Host")); h != "" {
		host = strings.Split(h, ",")[0]
	}
	return scheme + "://" + host
}

// openScopeDesc 把密钥数据范围翻译成人话，供智能体判断自己能查谁
func openScopeDesc(scope *model.OpenKeyScope, allowed []string) string {
	if allowed == nil {
		return "全部用户（该密钥未限定分组/用户）"
	}
	names := append([]string(nil), allowed...)
	sort.Strings(names)
	if len(names) > 50 {
		return fmt.Sprintf("共 %d 个授权用户（示例：%s …）", len(names), strings.Join(names[:50], "、"))
	}
	return fmt.Sprintf("共 %d 个授权用户：%s", len(names), strings.Join(names, "、"))
}

// buildAgentGuide 生成面向 AI 智能体的完整使用说明（中文）。
func buildAgentGuide(c *gin.Context, scope *model.OpenKeyScope, allowed []string) string {
	base := openBaseURL(c)

	contentRule := "可以获取对话内容（密钥已开放 include_content）"
	if !scope.IncludeContent {
		contentRule = "**不可获取对话内容**（该密钥 include_content=false）：/api/open/contents 会直接报错，" +
			"report 也不会带内容样本。请只基于用量数据下结论，并在结论中注明「本次未提供内容，无法判断用途本身是否合规」"
	}

	guide := `# TOKENHUB 数据开放接口 · AI 智能体使用说明

## 一、你的角色与目标
你是「用户 AI 使用合规审查助手」。你通过 TOKENHUB 的只读数据开放接口，获取指定用户在指定
时间范围内的模型调用情况与调用内容，然后为顾问产出结论：每个用户在用什么模型、花了多少、
是否存在与工作无关的用途、是否存在资源浪费（超长上下文、重复提问、模型选择不当）、是否涉及
敏感信息，以及是否可以继续授权。

## 二、硬性约束（必须遵守）
1. 所有接口都是**只读**的；不要尝试任何写操作，也不要请求本说明以外的接口。
2. 只使用接口返回的数据。不要编造用户、模型、金额、时间或对话内容；数据缺失就写「数据缺失」。
3. 用户范围受密钥限制，~~users~~ 只能填下方「你的数据范围」里的用户，越权请求会被拒绝。
4. 时间跨度受密钥「最多回看天数」限制，超出部分会被自动裁剪，请在结论里写明实际覆盖的区间。
5. 金额单位是人民币元（¥），接口已直接给出（~~cost_cny~~ / ~~used_cny~~ 等），不要自行换算。
6. 引用对话内容作为证据时，只引用接口返回的原文片段，不要扩写、也不要推测用户动机。

## 三、鉴权
所有请求携带同一个请求头（密钥由顾问另行提供）：

    Authorization: Bearer sk-open-xxxx

## 四、你的数据范围（本密钥）
- 可访问用户：__SCOPE__
- 内容权限：__CONTENT__
- 最多回看天数：__MAXDAYS__ 天
- 站点地址：__BASE__

## 五、接口清单

### 1. 一站式报告（首选，一次调用拿到大部分材料）
    GET __BASE__/api/open/report?start=YYYY-MM-DD&end=YYYY-MM-DD&top_users=20&samples=3&max_chars=1200&format=text

参数：~~start~~/~~end~~（默认最近 7 天）、~~users~~（逗号分隔，缺省=范围内全部）、~~model~~、~~group~~、
~~top_users~~（默认 20，按费用降序取前 N 人）、~~samples~~（每人最近几条内容样本，默认 3，0=不带内容）、
~~max_chars~~（每条内容截断长度，默认 1200）、~~format=json|text~~（text 是可直接阅读/喂给大模型的
纯文本，末尾自带审查提示词）。
返回：~~totals~~ 合计；~~users[]~~ 每人 ~~requests~~ / ~~total_tokens~~ / ~~prompt_tokens~~ /
~~completion_tokens~~ / ~~cost_cny~~ / ~~first_at~~ / ~~last_at~~；~~models[]~~ 该人各模型明细；~~samples[]~~ 内容样本。

### 2. 模型调用情况（只需要数字时用）
    GET __BASE__/api/open/usage?start=YYYY-MM-DD&end=YYYY-MM-DD&group_by=user_model&format=json

~~group_by~~：~~user~~（默认，按人）/ ~~model~~ / ~~user_model~~（人×模型）；~~limit~~ 默认 200、最大 1000；
~~format=csv|text~~ 可导出报表或纯文本。
每行字段：~~username~~ / ~~model_name~~ / ~~group~~ / ~~requests~~ / ~~prompt_tokens~~ / ~~completion_tokens~~ /
~~total_tokens~~ / ~~quota~~ / ~~cost_cny~~ / ~~first_at~~ / ~~last_at~~。

### 3. 用户额度概览（回答「给了多少、用了多少」）
    GET __BASE__/api/open/users?format=json

**注意：额度字段是账户累计值，不随时间范围变化。** 字段：~~user_id~~ / ~~username~~ / ~~display_name~~ /
~~employee_id~~ / ~~group~~ / ~~status~~ / ~~quota~~ / ~~used_quota~~ / ~~remaining_quota~~ / ~~quota_cny~~ /
~~used_cny~~ / ~~remaining_cny~~ / ~~request_count~~ / ~~created_at~~ / ~~last_login_at~~。
典型用法：用本接口的累计已用跟接口 2 的区间费用对照，回答「这段时间的消耗占其总额度的多少」。

### 4. 调用内容（要看原文时用）
    GET __BASE__/api/open/contents?users=张三&start=YYYY-MM-DD&end=YYYY-MM-DD&limit=50&max_chars=2000&format=text

~~limit~~ 默认 50、最大 200，按时间**从新到旧**返回；~~max_chars~~ 控制截断（0=完整原文）。
字段：~~time~~ / ~~username~~ / ~~model~~ / ~~group~~ / ~~prompt_tokens~~ / ~~completion_tokens~~ /
~~request~~ / ~~response~~ / ~~truncated~~。

### 5. 对话日志明细与导出报表（字段与后台「对话日志 → 导出CSV」完全一致）
    GET __BASE__/api/open/chat_logs?start=YYYY-MM-DD&end=YYYY-MM-DD&format=csv
    GET __BASE__/api/open/chat_logs?start=...&end=...&stats=1                  # 按人汇总条数与 token
    GET __BASE__/api/open/chat_logs?start=...&end=...&page=1&page_size=50      # 明细分页

导出报表列（顺序固定）：~~日志ID~~、~~时间~~、~~用户ID~~、~~用户名~~、~~令牌~~、~~渠道ID~~、~~模型~~、
~~分组~~、~~请求ID~~、~~流式~~、~~请求内容~~。时间为东八区本地时间，格式 ~~YYYY-MM-DD HH:mm:ss~~；
仅当密钥开放内容权限时才有「请求内容」列。

## 六、字段字典（明细 / 汇总 / 额度）
| 字段 | 含义 | 出现位置 |
| --- | --- | --- |
| 日志ID（id） | 一次调用的唯一编号 | 明细 |
| 时间（time / created_at） | 调用发生时间（本地时区） | 明细；usage/report 的 first_at、last_at |
| 用户ID（user_id） | 用户编号 | 明细、users |
| 用户名（username） | 登录用户名，审查报告以它为单位 | 全部 |
| 令牌（token_name） | 调用所用令牌名 | 明细 |
| 渠道ID（channel_id） | 命中的上游渠道编号 | 明细 |
| 模型（model_name / model） | 模型名 | 全部 |
| 分组（group） | 计费分组（用户分组或被令牌覆盖后的实际分组） | 全部 |
| 请求ID（request_id） | 上游请求追踪号 | 明细 |
| 流式（is_stream） | 是否流式返回 | 明细 |
| 请求内容（request / 请求内容） | 用户提问原文（受 max_chars 截断） | 明细、contents、report.samples |
| 回复内容（response） | 模型回复原文（同上） | contents、report.samples |
| tokens | 输入 prompt_tokens / 输出 completion_tokens / 合计 total_tokens | 明细、usage、report |
| 费用（cost_cny） | 该次或该组调用的费用（元） | usage、report |
| 额度（quota / used_quota / remaining_quota） | 账户总额度、累计已用、剩余（元，累计值） | users |

## 七、推荐工作流
1. 先调**接口 3**（users）确认范围内有哪些人、各自额度与累计消耗，建立基线。
2. 再调**接口 1**（report，format=text）拿到区间内的费用排序、模型明细与内容样本。
3. 对「费用异常高」或「模型选择可疑」的用户，用**接口 4**（contents，users=该人、max_chars 调大、
   必要时分页）取更完整的原文核对。
4. 需要和顾问已有表格对齐时，用**接口 5**的 ~~format=csv~~ 导出报表（列与后台导出完全一致）。
5. 组织结论时写清区间、人数、总费用、每人结论与证据；数据不足就写「数据不足」。

## 八、输出格式要求
按用户逐条给出，每条包含：
- **结论**：合规 / 需关注 / 不建议继续授权（三选一）
- **用量事实**：调用次数、tokens、费用、主要使用的模型、活跃时段
- **判断依据**：为什么给出该结论（对照标准：与工作无关的用途、明显浪费、敏感信息）
- **证据**：直接引用接口返回的内容片段，并注明时间与模型
最后给一段整体小结（范围覆盖度、共性问题、建议的额度或授权调整）。

## 九、错误处理
返回体统一为 ~~{"success":false,"message":"..."}~~，常见 message 与处置：
- ~~缺少 Authorization: Bearer <开放密钥>~~：没带密钥。
- ~~密钥无效~~ / ~~密钥已过期~~ / ~~密钥已停用~~：联系管理员换发。
- ~~用户「X」不在该密钥的数据范围内~~：越权请求，改用授权范围内的用户。
- ~~该密钥未开放对话内容（include_content=false）~~：无内容权限，改用用量类接口。
- ~~start 时间格式应为 YYYY-MM-DD 或 YYYY-MM-DD HH:mm:ss~~：时间参数写错。
- ~~对话日志功能未启用~~：站点未开启对话日志，请管理员开启。

## 十、示例
    # 1) 最近 30 天一览（费用降序、每人 3 条内容样本）
    curl -H "Authorization: Bearer $KEY" \
      "__BASE__/api/open/report?start=__START__&end=__END__&top_users=20&samples=3&format=text"

    # 2) 看某人是否在浪费：先看用量，再看内容
    curl -H "Authorization: Bearer $KEY" "__BASE__/api/open/usage?users=张三&group_by=user_model&start=__START__&end=__END__"
    curl -H "Authorization: Bearer $KEY" "__BASE__/api/open/contents?users=张三&limit=50&max_chars=2000&start=__START__&end=__END__&format=text"

    # 3) 与后台导出报表同列的 CSV
    curl -H "Authorization: Bearer $KEY" -o chat_logs.csv \
      "__BASE__/api/open/chat_logs?start=__START__&end=__END__&format=csv"
`

	start := timeNowMinusDays(30)
	end := timeNowDate()
	repl := map[string]string{
		"~~":          "`",
		"__SCOPE__":   openScopeDesc(scope, allowed),
		"__CONTENT__": contentRule,
		"__MAXDAYS__": fmt.Sprintf("%d", scope.MaxDays),
		"__BASE__":    base,
		"__START__":   start,
		"__END__":     end,
	}
	for k, v := range repl {
		guide = strings.ReplaceAll(guide, k, v)
	}
	return guide
}

// OpenAgentGuide GET /api/open/guide
func OpenAgentGuide(c *gin.Context) {
	scope := c.MustGet("open_scope").(*model.OpenKeyScope)
	allowed, err := model.ScopeAllowedUsernames(scope)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "权限解析失败: " + err.Error()})
		return
	}
	guide := buildAgentGuide(c, scope, allowed)

	if c.Query("format") == "json" {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
			"base_url":          openBaseURL(c),
			"scope":             scope,
			"allowed_usernames": allowed,
			"content_available": scope.IncludeContent,
			"max_days":          scope.MaxDays,
			"guide":             guide,
			"endpoints": []string{
				"/api/open/report", "/api/open/usage", "/api/open/users",
				"/api/open/contents", "/api/open/chat_logs",
			},
		}})
		return
	}
	c.Data(http.StatusOK, "text/markdown; charset=utf-8", []byte(guide))
}

// OpenAPISpec GET /api/open/openapi.json — 查询端点的 OpenAPI 3.0 描述
func OpenAPISpec(c *gin.Context) {
	base := openBaseURL(c)
	strParam := func(name, desc string) gin.H {
		return gin.H{"name": name, "in": "query", "required": false,
			"schema": gin.H{"type": "string"}, "description": desc}
	}
	op := func(summary string, params []gin.H) gin.H {
		common := []gin.H{
			strParam("start", "开始时间 YYYY-MM-DD（或 YYYY-MM-DD HH:mm:ss），默认最近 7 天"),
			strParam("end", "结束时间，默认当前时间"),
			strParam("users", "用户名，逗号分隔；缺省=密钥授权范围内全部"),
			strParam("model", "模型名，模糊匹配"),
			strParam("group", "计费分组，精确匹配"),
			strParam("format", "返回格式：json | text | csv"),
		}
		return gin.H{"get": gin.H{
			"summary":    summary,
			"responses":  gin.H{"200": gin.H{"description": "OK"}},
			"parameters": append(common, params...),
		}}
	}
	spec := gin.H{
		"openapi": "3.0.1",
		"info": gin.H{
			"title":   "TOKENHUB 数据开放接口",
			"version": "1.0.0",
			"description": "用户模型调用情况与调用内容（只读）。鉴权：Authorization: Bearer sk-open-xxxx。" +
				"面向 AI 智能体的完整说明见 /api/open/guide。",
		},
		"servers": []gin.H{{"url": base}},
		"components": gin.H{"securitySchemes": gin.H{"OpenKey": gin.H{
			"type": "http", "scheme": "bearer", "description": "数据开放密钥 sk-open-xxxx"}}},
		"security": []gin.H{{"OpenKey": []string{}}},
		"paths": gin.H{
			"/api/open/report": op("一站式报告：用量汇总 + 各用户模型明细 + 内容样本", []gin.H{
				{"name": "top_users", "in": "query", "schema": gin.H{"type": "integer", "default": 20}, "description": "按费用降序取前 N 人，最大 50"},
				{"name": "samples", "in": "query", "schema": gin.H{"type": "integer", "default": 3}, "description": "每人内容样本条数，0=不带内容，最大 20"},
				{"name": "max_chars", "in": "query", "schema": gin.H{"type": "integer", "default": 1200}, "description": "每条内容截断长度，0=不截断"}}),
			"/api/open/usage": op("模型调用情况：次数 / token / 费用 / 首末调用时间", []gin.H{
				{"name": "group_by", "in": "query", "schema": gin.H{"type": "string", "enum": []string{"user", "model", "user_model"}, "default": "user"}},
				{"name": "limit", "in": "query", "schema": gin.H{"type": "integer", "default": 200}, "description": "最大 1000"}}),
			"/api/open/users": op("用户额度概览（累计总额度/已用/剩余 + 累计调用次数）", nil),
			"/api/open/contents": op("调用内容：提问与回复原文", []gin.H{
				{"name": "limit", "in": "query", "schema": gin.H{"type": "integer", "default": 50}, "description": "按时间从新到旧，最大 200"},
				{"name": "max_chars", "in": "query", "schema": gin.H{"type": "integer", "default": 0}, "description": "每条内容截断长度，0=不截断"}}),
			"/api/open/chat_logs": op("对话日志明细（字段与后台「导出CSV」一致）", []gin.H{
				{"name": "stats", "in": "query", "schema": gin.H{"type": "string"}, "description": "stats=1 按用户汇总"},
				{"name": "page", "in": "query", "schema": gin.H{"type": "integer", "default": 1}},
				{"name": "page_size", "in": "query", "schema": gin.H{"type": "integer", "default": 50}, "description": "最大 100"},
				{"name": "token_name", "in": "query", "schema": gin.H{"type": "string"}}}),
			"/api/open/guide": op("本接口的智能体使用说明（markdown）", nil),
		},
	}
	c.JSON(http.StatusOK, spec)
}
