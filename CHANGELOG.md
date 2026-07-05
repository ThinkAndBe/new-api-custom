# 变更记录 / Changelog

本项目所有重要变更记录于此文件。格式参考 [Keep a Changelog](https://keepachangelog.com/)。

---

## [Unreleased] - 2026-07-05

### Headroom 压缩系统集成

#### 新增
- **Headroom sidecar 容器**（`docker/headroom/`）：基于 headroom-ai 0.10.0 的 FastAPI 包装器
  - Kompress ML（ONNX 模型）压缩模式
  - tiktoken 离线（litellm 内置）
  - CCR 禁用、Magika 禁用
  - protect_recent=3, min_tokens=100
- **Headroom 看板**（`controller/headroom_dashboard.go` + `web/classic/src/pages/Headroom/`）
  - 总节省/原输入/实际发送/平均节省率/压缩请求数 五项 KPI
  - 按模型/渠道/用户节省排行（横向柱状图）
  - 历史趋势报表（堆叠面积图，按日/周/月自动切换粒度，绕过留存限制）
  - 月度/年度汇总表（带合计行）
  - 压缩记录明细表（分页）
  - CSV 导出（明细/按模型/按渠道/按月/按年）
- **Headroom 配置项**
  - `HeadroomGlobalEnabled`（全局开关，默认 true）
  - `HeadroomRetentionDays`（留存天数，默认 30）
  - 渠道级 `headroom_enabled` / `headroom_url` 设置
- **Docker Compose 编排**（`docker-compose.headroom.yml`）：方案 B sidecar 部署

#### 修复
- Headroom `compression_ratio` 语义修正：保留率 → 节省率（`saved/input`）
- Headroom 压缩超时 30s → 5s，避免客户端长时间等待
- 压缩失败日志级别 LogDebug → LogInfo
- Token 计数准确性：
  - `text_quota.go` 统一在计费汇总点修正 prompt_tokens（覆盖 OpenAI/Claude→OpenAI/原生 Claude 所有路径）
  - `OaiStreamHandler` 补全 `completion_tokens=0` 时本地估算
  - 历史 `headroom_ratio` 数据批量重算

### doubao-agent-plan 渠道支持

#### 修复
- `volcengine/adaptor.go`：无 ClaudeBaseURL 时 fallback 到 OpenAIBaseURL
- `ConvertClaudeRequest` / `DoResponse`：无 ClaudeBaseURL 时走 openai.Adaptor
- `openai/helper.go` `processTokenData`：新增 default 分支处理 RelayModeUnknown（Claude→OpenAI 转换路径）
- `relay/helper/valid_request.go`：新增 `normalizeClaudeContentBlocks`，将 Claude 风格 tool_use/tool_result 转为 OpenAI 格式

### 渠道并发控制

#### 新增
- `dto/channel_settings.go`：新增 `MaxConcurrency` 字段（0=不限）
- `service/channel_concurrency.go`：每渠道信号量管理器（sync.Map + LoadOrStore）
  - `TryAcquireChannel` / `ReleaseChannel` / `GetChannelInflight` / `CleanupChannelSemaphores`
- `middleware/distributor.go`：选渠道后检查并发，满则返回 503，defer Release
- `controller/relay.go`：重试循环中也检查并发，满则跳过该渠道
- 渠道编辑页新增"最大并发"配置（Form.InputNumber，0-1000）

#### 变更
- `setting/operation_setting/status_code_ranges.go`：自动禁用默认范围从 {401} 扩展为 {401, 429, 503}

### 官方价格同步

#### 修复
- `service/ratio_auto_sync.go`：
  - 数据源从 basellm.github.io（已失效）切换到 models.dev
  - 新增国产厂商白名单（zhipuai, alibaba-cn, moonshotai, minimax, xiaomi 等）
  - 新增 `OfficialRatioRMBCorrection = 0.78`（USD → RMB 系数）
  - 新增 `OfficialRatioOverwriteExisting` 开关
  - 新增 `http.ProxyFromEnvironment` 代理支持

### 使用日志增强

#### 新增
- `controller/log.go`：`GetLogsStat` / `GetLogsSelfStat` 返回 `total_tokens` / `prompt_tokens` / `completion_tokens` / `request_count`
- `model/log.go`：`Stat` 结构体扩展 token 统计字段
- 前端 UsageLogsActions 新增"总 Tokens"和"请求数"标签

### 渠道健康检查

#### 新增
- `controller/channel_health.go` / `controller/channel_health_check.go`：渠道健康检查机制
- `service/channel_schedule_pause.go`：渠道定时暂停

### 其它

#### 新增
- `web/classic/src/components/auth/ForceChangePasswordModal.jsx`：强制改密弹窗
- `web/classic/src/components/table/users/modals/AddUserModal.jsx`：用户创建增强
- `web/classic/src/pages/Setting/Operation/SettingsGeneral.jsx`：新增 HeadroomRetentionDays / OfficialRatioOverwriteExisting 配置
- `.gitignore`：排除测试 JSON、日志、DB 备份、headroom 模型文件

---

## 变更分类说明
- **新增**：新功能、新文件
- **修复**：Bug 修复
- **变更**：行为变更
- **移除**：删除的功能/文件
