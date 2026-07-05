# Headroom 上下文压缩集成

## 架构：纯压缩模式

NEW API 采用 **纯压缩模式** 集成 Headroom：

```
Client → NEW API (Go) → [Headroom /v1/compress] → 上游 LLM
                          (Python sidecar)
```

- **无 CCR**：不启用可逆压缩检索（需要跨轮状态和工具注入，与网关架构冲突）
- **无 Magika ML**：用 regex 内容检测回退（避免首次加载延迟）
- **无 Kompress ML**：默认禁用 ML 文本压缩模型（避免下载 HuggingFace 模型）
- **tiktoken 离线**：复用 litellm 自带的编码文件，完全离线工作

### 为什么不用 Headroom Proxy 模式？

Headroom 原生支持 proxy 模式（拦截全部 LLM 流量），但与 NEW API 网关架构冲突：
- NEW API 已有路由/重试/计费/多渠道
- Proxy 模式需要 Headroom 知道上游凭证
- CCR 可逆压缩需要跨轮状态和工具注入，无状态 HTTP 端点无法实现

因此用 `headroom.compress()` 函数封装成 `/v1/compress` HTTP 端点，NEW API 在转发前调用压缩。

## 快速部署

### 方式 1：在已有 docker-compose.yml 上启用

```bash
docker compose --profile headroom up -d --build
```

NEW API 容器已注入 `HEADROOM_URL=http://headroom:8787`，渠道编辑页的 `Headroom URL` 留空即可。

### 方式 2：全新自用部署

```bash
docker compose -f docker-compose.headroom.yml up -d --build
```

### 方式 3：独立运行 Headroom

```bash
cd docker/headroom
docker build -t new-api-headroom:latest .
docker run -d --name headroom -p 8787:8787 new-api-headroom:latest
```

## 配置项

### NEW API 端

| 配置位置 | 字段 | 默认值 | 说明 |
|----------|------|--------|------|
| 系统设置 → 运营设置 | `HeadroomGlobalEnabled` | `true` | 全局总开关 |
| 环境变量 | `HEADROOM_URL` | `http://127.0.0.1:8787` | Headroom 地址兜底值 |
| 渠道 → 编辑 → 扩展设置 | `Headroom URL` | 空 | 单渠道地址，留空则回退到 `HEADROOM_URL` |
| 渠道 → 编辑 → 扩展设置 | `Headroom 开关` | 关闭 | 单渠道开关 |

优先级：**渠道 URL > 环境变量 `HEADROOM_URL` > 默认值**

### Headroom 端

| 变量 | 默认 | 说明 |
|------|------|------|
| `HEADROOM_PORT` | `8787` | 监听端口 |
| `HEADROOM_WORKERS` | `2` | worker 进程数 |
| `HEADROOM_KOMPRESS_MODEL` | `disabled` | 禁用 ML 压缩；置空启用（需联网下载模型） |
| `HEADROOM_COMPRESS_USER_MESSAGES` | `false` | 不压缩用户消息 |
| `HEADROOM_COMPRESS_SYSTEM_MESSAGES` | `true` | 允许压缩系统消息 |
| `HEADROOM_PROTECT_RECENT` | `4` | 保护最近 4 条不压缩 |
| `HEADROOM_MIN_TOKENS_TO_COMPRESS` | `250` | 小于 250 token 的消息不压缩 |
| `TIKTOKEN_CACHE_DIR` | litellm 目录 | tiktoken 编码文件缓存（已内置） |

## 三大离线保障

### 1. tiktoken 离线

tiktoken 在遇到 OpenAI 模型名时会从 `openaipublic.blob.core.windows.net` 下载编码文件。解决方案：litellm 包自带了这些文件，Dockerfile 设置 `TIKTOKEN_CACHE_DIR` 指向它：

```dockerfile
ENV TIKTOKEN_CACHE_DIR=/usr/local/lib/python3.11/site-packages/litellm/litellm_core_utils/tokenizers
```

### 2. CCR 标记禁用

`compress()` 默认开启 CCR 标记注入（`[N items compressed to M. Retrieve more: hash=xxx.]`），但纯压缩模式下没有 retrieve 工具。app.py 在启动时替换 pipeline 的 ContentRouter：

```python
ContentRouter(config=ContentRouterConfig(
    ccr_enabled=False,
    ccr_inject_marker=False,
))
```

### 3. Magika ML 检测禁用

Magika 首次加载耗时 100-200ms，高并发下堆积。app.py 强制关闭：

```python
import headroom.transforms.content_router as _cr
_cr._magika_status = False  # 用 regex 回退
```

## 熔断与降级

NEW API 的 `doRequest()` 内置熔断：
- Headroom 调用超时（30s）、返回非 200、压缩失败时，**自动回退原始请求体**
- 仅在 Chat Completions 模式下触发
- 失败原因写入 debug 日志

## 压缩效果

保守模式下，以下场景会产生实际压缩：
- 超长 system prompt（> 250 tokens）
- 大型工具输出 / JSON 数组（SmartCrusher 去重）
- 多轮对话历史（protect_recent=4 之前的消息）

普通短对话不会压缩（`min_tokens_to_compress=250`），这是预期行为。

### 启用 ML 压缩（可选）

如果服务器能稳定访问 HuggingFace，可启用 Kompress ML 压缩获得更高压缩率：

```yaml
environment:
  - HEADROOM_KOMPRESS_MODEL=    # 置空启用默认模型
  - HF_ENDPOINT=https://hf-mirror.com
```

启用后首次压缩会下载 `chopratejas/kompress-base`（约数百 MB），建议挂载 `./headroom-models:/models` 持久化。

## 停用 / 卸载

```bash
docker compose --profile headroom stop headroom
docker compose --profile headroom rm -sf headroom
rm -rf ./headroom-models
```

停用后 NEW API 自动走降级路径（透传原始请求），业务不受影响。
