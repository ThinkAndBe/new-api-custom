package controller

// open_api_guide.go — 面向 AI 智能体的操作手册（数据开放接口）。
//
// 文档随接口一起发布，并注入「当前密钥」的真实数据范围；顾问把密钥和本文档交给智能体后，
// 智能体即可自助完成：拉权限内数据 → 交叉核对 → 产出合规审查结论。
//   GET /api/open/guide          本文档（text/markdown；format=json 返回结构化）
//   GET /api/open/openapi.json   查询端点的 OpenAPI 3.0 描述（供支持工具导入的智能体）
//
// 实现提示：正文用反引号原生字符串承载，其中 markdown 反引号统一写 ~~ 占位，最后 ReplaceAll 还原。

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

// buildAgentGuide 生成面向 AI 智能体的操作手册
func buildAgentGuide(c *gin.Context, scope *model.OpenKeyScope, allowed []string) string {
	base := openBaseURL(c)

	contentRule := "可以读取用户输入内容（include_content=true）"
	if !scope.IncludeContent {
		contentRule = "**无内容权限**（include_content=false）：/api/open/contents 与 /api/open/contents/export 会报错，" +
			"report 也不带内容。只能基于用量数据下结论，并注明「本次未提供内容，无法判断用途」"
	}
	scopeGroups := "（未按分组限定）"
	if len(scope.Groups) > 0 {
		scopeGroups = strings.Join(scope.Groups, "、")
	}

	guide := `# TOKENHUB 数据开放接口 · AI 智能体操作手册

## 0. 你的任务
你是「用户 AI 使用合规审查助手」。基于只读的数据开放接口，回答两类问题并给出结论：
- 任务 A：每个用户的**调用次数、按时间节点的 token 消耗总量、消耗金额**
- 任务 B：**所有相关分组的全部用户对话内容**（用于判断用途是否合规、是否浪费资源）

## 1. 最高优先级约束（违反即为错误输出）
1. **权限边界**：你只能访问本密钥范围内的数据（见 §3）。接口返回的数据必定已按范围过滤；
   你若传 ~~users~~ / ~~groups~~ 只能**收窄**，越权会直接报错——不要枚举或猜测范围外的用户。
2. **只读**：所有接口都是 GET，不要尝试任何写操作。
3. **不编造**：只使用接口返回的数据。缺失就写「数据缺失」，不要推测用户动机、不要补数据。
4. **内容默认只有用户输入**：内容接口默认返回用户提问（~~request~~），模型回复默认不返回
   （需要时显式 ~~with_response=1~~）。
5. **金额单位是人民币元**，接口已给出 ~~cost_cny~~ / ~~used_cny~~，不要自行换算。
6. 时间参数只写日期（~~YYYY-MM-DD~~）时按**整天**处理；分桶时间统一为**东八区**。

## 2. 鉴权
所有请求带同一个请求头：

    Authorization: Bearer <本密钥>

## 3. 你的数据范围（由本密钥决定，不可扩大）
- 授权分组：__SCOPE_GROUPS__
- 可访问用户：__SCOPE_USERS__
- 内容权限：__CONTENT__
- 最多回看天数：__MAXDAYS__ 天
- 站点地址：__BASE__

## 4. 任务 A：调用次数 / 按时间节点的 token / 金额

    GET __BASE__/api/open/usage?start=2026-09-01&end=2026-09-30&granularity=day&group_by=user&format=json

参数组合（按需选一种）：
| 目标 | 参数 |
| --- | --- |
| 每个用户 × 每天的次数/token/金额 | ~~granularity=day&group_by=user~~ |
| 每个用户 × 每小时（排查突发） | ~~granularity=hour&group_by=user~~ |
| 每个用户 × 每模型 × 每天 | ~~granularity=day&group_by=user_model~~ |
| 每个分组 × 每天（先看大盘） | ~~granularity=day&group_by=group~~ |
| 全体 × 每天 | ~~granularity=day&group_by=all~~ |
| 只要区间合计（不分时间） | 去掉 ~~granularity~~ |

返回（json）：
    {"success":true,"data":{
      "range":{"start":"2026-09-01 00:00:00","end":"2026-09-30 23:59:59","days":30},
      "group_by":"user","granularity":"day",
      "totals":{"requests":0,"prompt_tokens":0,"completion_tokens":0,"total_tokens":0,"quota":0,"cost_cny":0},
      "items":[{"username":"张三","model_name":"","group":"WK第一批","bucket":"2026-09-16",
                "requests":120,"prompt_tokens":3500000,"completion_tokens":45000,"total_tokens":3545000,
                "quota":1776000,"cost_cny":3.552,"first_at":"...","last_at":"..."}]}}

字段语义：
- ~~requests~~ 调用次数；~~prompt_tokens~~ 输入 token；~~completion_tokens~~ 输出 token；~~total_tokens~~ 两者之和
- ~~quota~~ 原始额度单位；~~cost_cny~~ = quota / 500000，单位为元（**报金额用这个**）
- ~~bucket~~ 分桶标签（东八区）：~~granularity=day~~ 为 ~~YYYY-MM-DD~~，~~hour~~ 为 ~~YYYY-MM-DD HH:00~~
- ~~first_at~~ / ~~last_at~~ 仅在不分桶时有意义（该组内首末调用时间）
- ~~limit~~ 默认 200、最大 5000；超限时按 ~~bucket~~ 升序 + 金额降序截断

## 5. 任务 B：所有相关分组的全部用户对话内容

**用游标循环拉取，不要用 ~~page~~ 深翻页**（慢且易漏）。默认只返回用户输入。

    GET __BASE__/api/open/contents/export?start=2026-09-16&end=2026-09-16&limit=2000&format=jsonl&after_id=0

响应头（每次都要读）：
- ~~X-Next-After-Id~~：下一页的 ~~after_id~~（填进下一次请求）
- ~~X-Has-More~~：~~1~~ 还有数据，~~0~~ 已拉完
- ~~X-Count~~：本页条数

循环（伪代码，务必按此实现）：
    after = 0
    loop:
      resp = GET /api/open/contents/export?start=..&end=..&limit=2000&format=jsonl&after_id={after}
      consume(resp.body)              # jsonl：一行一条 JSON
      if resp.headers[X-Has-More] == "0": break
      after = resp.headers[X-Next-After-Id]

其它参数：
| 参数 | 说明 |
| --- | --- |
| ~~format=jsonl~~ | 默认；一行一条 JSON，最适合直接消费 |
| ~~format=csv~~ | 列与后台「对话日志 → 导出CSV」完全一致，适合交付 Excel |
| ~~format=text~~ | 纯文本，适合人读 |
| ~~users=张三,李四~~ | 只取指定用户（必须在授权范围内） |
| ~~groups=WK第一批~~ | 只取指定分组（必须是本密钥授权分组的子集） |
| ~~model=glm-5.3~~ | 只取指定模型（模糊匹配） |
| ~~max_chars=2000~~ | 每条内容截断长度，0=原文；长上下文建议 2000~8000 |
| ~~with_response=1~~ | 附带模型回复（默认不返回） |
| ~~limit~~ | 默认 2000、最大 5000 |

jsonl 每行字段：
    {"id":123,"time":"2026-09-16 18:07:50","user_id":11,"username":"肖仕泉","token_name":"tok",
     "channel_id":3,"model":"glm-5.3","group":"WK第二批","request_id":"...","is_stream":false,
     "prompt_tokens":6879,"completion_tokens":702,"request":"<用户输入原文>","truncated":false}

> 数据量大时建议**按天切片**循环（每天各自 after_id 循环），便于断点续传与分批交给模型。

## 6. 其它端点

| 端点 | 用途 | 关键参数 |
| --- | --- | --- |
| ~~/api/open/users~~ | 每人**累计**总额度/已用/剩余（元）+ 累计调用次数 + 分组/状态/工号 | ~~format=json\|text\|csv~~ |
| ~~/api/open/report~~ | 一次拿到：用量汇总 + 每人模型明细 + 每人最近 N 条输入内容 | ~~top_users≤50~~、~~samples≤200~~、~~max_chars~~、~~with_response~~、~~format=json\|text~~ |
| ~~/api/open/contents~~ | 单页取内容（不想写游标循环时用，单次上限 200 条） | ~~users~~、~~groups~~、~~limit≤200~~、~~max_chars~~、~~with_response~~、~~format=json\|text~~ |
| ~~/api/open/chat_logs~~ | 明细/报表：~~stats=1~~ 按人汇总、~~format=csv~~ 导出报表、~~page/page_size≤100~~ 分页 | ~~token_name~~、~~model_name~~、~~group~~ |
| ~~/api/open/guide~~ | 本手册（~~format=json~~ 返回结构化 + 范围字段） | — |

额度说明：~~/api/open/users~~ 的额度是**账户累计值**，不随时间窗变化；与 ~~/usage~~ 的区间金额对照即可回答
「这段时间的消耗占其总额度的多少」。

## 7. 推荐执行顺序
1. ~~GET /api/open/users~~ → 建基线：范围内有哪些人、各自额度与累计消耗（发现"授权了没用"/"额度给太多"）。
2. ~~GET /api/open/usage?granularity=day&group_by=user~~ → 任务 A 的答案：每人每日次数/token/金额矩阵。
3. ~~GET /api/open/contents/export~~ 游标循环拉完 → 任务 B 的答案：范围内全部用户输入内容。
4. 交叉核对：对「金额靠前」或「模型选择可疑」的用户，用 ~~users=~~ 单独再拉一次细看。
5. 输出结论（见 §8）。

## 8. 输出格式（严格遵守）
按用户逐条：
- **结论**：合规 / 需关注 / 不建议继续授权（三选一）
- **用量事实**：区间内调用次数、输入/输出 token、金额（元）、主要模型、按天分布要点
- **判断依据**：对照标准——①与工作无关的用途；②资源浪费（超长上下文、重复提问、模型选择不当）；
  ③敏感信息（客户数据、代码密钥、个人信息）
- **证据**：直接引用返回内容中的原文片段，注明时间与模型
最后给整体小结：覆盖区间、人数、总金额、共性问题、额度或授权调整建议。
若范围内无数据或内容权限缺失，明确写出，不要用推测填充。

## 9. 错误处理
返回体统一 ~~{"success":false,"message":"..."}~~（HTTP 200）：
| message | 含义 / 处置 |
| --- | --- |
| 缺少 Authorization: Bearer <开放密钥> | 请求头没带密钥 |
| 密钥无效 / 密钥已过期 / 密钥已停用 | 联系管理员换发 |
| 用户「X」不在该密钥的数据范围内 | 越权（只能查授权用户），改用范围内用户 |
| 分组「X」不在该密钥的数据范围内 | 越权；或该密钥未按分组授权，去掉 ~~groups~~ |
| 该密钥未开放对话内容（include_content=false） | 无内容权限，改用用量类接口 |
| 该密钥的数据范围内当前没有… | 范围解析为空（如分组下无人），属正常空结果 |
| start/end 时间格式应为 YYYY-MM-DD 或 YYYY-MM-DD HH:mm:ss | 时间参数写错 |
| 对话日志功能未启用 | 站点未开启对话日志，请管理员开启 |

## 10. 完整示例
    KEY=sk-open-xxxx
    BASE=__BASE__

    # 任务 A：每人每天的次数/token/金额
    curl -H "Authorization: Bearer $KEY" \
      "$BASE/api/open/usage?start=__START__&end=__END__&granularity=day&group_by=user&format=json"

    # 任务 A：分组大盘
    curl -H "Authorization: Bearer $KEY" \
      "$BASE/api/open/usage?start=__START__&end=__END__&granularity=day&group_by=group&format=text"

    # 任务 B：游标循环拉取全部用户输入内容（jsonl）
    after=0
    while :; do
      curl -s -D headers.txt -H "Authorization: Bearer $KEY" \
        "$BASE/api/open/contents/export?start=__START__&end=__END__&limit=2000&format=jsonl&after_id=$after" >> contents.jsonl
      grep -qi "X-Has-More: 0" headers.txt && break
      after=$(grep -i "X-Next-After-Id:" headers.txt | tr -d '\r' | awk '{print $2}')
    done

    # 与后台导出报表同列（交付 Excel）
    curl -H "Authorization: Bearer $KEY" -o chat_logs.csv \
      "$BASE/api/open/chat_logs?start=__START__&end=__END__&format=csv"
`

	repl := map[string]string{
		"~~":               "`",
		"__SCOPE_GROUPS__": scopeGroups,
		"__SCOPE_USERS__":  openScopeDesc(scope, allowed),
		"__CONTENT__":      contentRule,
		"__MAXDAYS__":      fmt.Sprintf("%d", scope.MaxDays),
		"__BASE__":         base,
		"__START__":        timeNowMinusDays(30),
		"__END__":          timeNowDate(),
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
			"content_fields":    "默认仅用户输入（request）；with_response=1 才含模型回复",
			"max_days":          scope.MaxDays,
			"guide":             guide,
			"endpoints": []string{
				"/api/open/report", "/api/open/usage", "/api/open/users",
				"/api/open/contents", "/api/open/contents/export", "/api/open/chat_logs",
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
	commonParams := func() []gin.H {
		return []gin.H{
			strParam("start", "开始时间 YYYY-MM-DD（按整天）或 YYYY-MM-DD HH:mm:ss，默认最近 7 天"),
			strParam("end", "结束时间，默认当前时间"),
			strParam("users", "用户名，逗号分隔；只能填密钥授权范围内的用户"),
			strParam("groups", "分组，逗号分隔；必须是密钥授权分组的子集"),
			strParam("model", "模型名，模糊匹配"),
			strParam("format", "返回格式：json | text | csv | jsonl（导出）"),
		}
	}
	op := func(summary string, params []gin.H) gin.H {
		return gin.H{"get": gin.H{
			"summary":    summary,
			"responses":  gin.H{"200": gin.H{"description": "OK"}},
			"parameters": append(commonParams(), params...),
		}}
	}
	spec := gin.H{
		"openapi": "3.0.1",
		"info": gin.H{
			"title":   "TOKENHUB 数据开放接口",
			"version": "1.1.0",
			"description": "用户模型调用情况与用户输入内容（只读，按密钥范围强制过滤）。" +
				"鉴权：Authorization: Bearer sk-open-xxxx。面向 AI 智能体的操作手册见 /api/open/guide。",
		},
		"servers": []gin.H{{"url": base}},
		"components": gin.H{"securitySchemes": gin.H{"OpenKey": gin.H{
			"type": "http", "scheme": "bearer", "description": "数据开放密钥 sk-open-xxxx"}}},
		"security": []gin.H{{"OpenKey": []string{}}},
		"paths": gin.H{
			"/api/open/usage": op("调用次数 / token / 金额汇总（支持按天、按小时分桶）", []gin.H{
				{"name": "group_by", "in": "query", "schema": gin.H{"type": "string",
					"enum": []string{"user", "model", "user_model", "group", "all"}, "default": "user"}},
				{"name": "granularity", "in": "query", "schema": gin.H{"type": "string",
					"enum": []string{"day", "hour"}, "description": "东八区分桶；不传=区间合计"}},
				{"name": "limit", "in": "query", "schema": gin.H{"type": "integer", "default": 200}, "description": "最大 5000"}}),
			"/api/open/users": op("用户额度概览（累计总额度/已用/剩余 + 累计调用次数）", nil),
			"/api/open/contents": op("对话内容单页（默认仅用户输入）", []gin.H{
				{"name": "limit", "in": "query", "schema": gin.H{"type": "integer", "default": 50}, "description": "最大 200"},
				{"name": "max_chars", "in": "query", "schema": gin.H{"type": "integer", "default": 0}, "description": "每条截断长度，0=原文"},
				{"name": "with_response", "in": "query", "schema": gin.H{"type": "string", "enum": []string{"0", "1"}, "default": "0"}, "description": "1=附带模型回复"}}),
			"/api/open/contents/export": op("对话内容批量导出（游标翻页，适合拉全量）", []gin.H{
				{"name": "after_id", "in": "query", "schema": gin.H{"type": "integer", "default": 0}, "description": "游标：仅取 id 大于该值的数据"},
				{"name": "limit", "in": "query", "schema": gin.H{"type": "integer", "default": 2000}, "description": "每页条数，最大 5000"},
				{"name": "max_chars", "in": "query", "schema": gin.H{"type": "integer", "default": 0}},
				{"name": "with_response", "in": "query", "schema": gin.H{"type": "string", "enum": []string{"0", "1"}, "default": "0"}}}),
			"/api/open/report": op("一站式报告：用量汇总 + 各用户模型明细 + 每人最近 N 条输入内容", []gin.H{
				{"name": "top_users", "in": "query", "schema": gin.H{"type": "integer", "default": 20}, "description": "按金额降序取前 N 人，最大 50"},
				{"name": "samples", "in": "query", "schema": gin.H{"type": "integer", "default": 3}, "description": "每人内容条数，0=不带内容，最大 200"},
				{"name": "max_chars", "in": "query", "schema": gin.H{"type": "integer", "default": 1200}},
				{"name": "with_response", "in": "query", "schema": gin.H{"type": "string", "enum": []string{"0", "1"}, "default": "0"}}}),
			"/api/open/chat_logs": op("对话日志明细 / 报表导出（列与后台导出CSV一致）", []gin.H{
				{"name": "stats", "in": "query", "schema": gin.H{"type": "string"}, "description": "stats=1 按用户汇总"},
				{"name": "page", "in": "query", "schema": gin.H{"type": "integer", "default": 1}},
				{"name": "page_size", "in": "query", "schema": gin.H{"type": "integer", "default": 50}, "description": "最大 100"},
				{"name": "token_name", "in": "query", "schema": gin.H{"type": "string"}},
				{"name": "model_name", "in": "query", "schema": gin.H{"type": "string"}}}),
			"/api/open/guide": op("本手册（markdown；format=json 返回结构化含范围）", nil),
		},
	}
	c.JSON(http.StatusOK, spec)
}
