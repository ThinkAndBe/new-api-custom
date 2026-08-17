---
name: glm46v-vision
description: 视觉理解技能。当用户发送图片、截图、照片、图表，或要求"看这张图"、"图片里是什么"、"OCR识别"、"读图"、"分析截图"、"对比两张图"等需要图像理解的任务时触发。本技能调用 glm-4.6v 视觉模型（tokenhub 专用识图令牌），补齐国内文本大模型（glm-5.2/deepseek 等）没有的视觉能力。
---

# 视觉理解（glm-4.6v）

当用户提供图片需要视觉理解时，调用 glm-4.6v 处理。**不要用当前对话模型回答图片问题**——当前模型没有视觉能力。

## 接口信息

- API 地址：`https://tokenhub.erke.com:3000/v1/chat/completions`（OpenAI 兼容格式）
- 模型名：`glm-4.6v`
- 令牌：从管理员处获取识图专用令牌（环境变量 `GLM46V_API_KEY`）
- 认证：`Authorization: Bearer $GLM46V_API_KEY`

## 调用流程

### 1. 读取图片为 base64

支持 png / jpg / jpeg / webp / gif。超过 5MB 的图片先压缩再传。

```bash
# 读取本地图片并转 base64（macOS/Linux）
base64 -w 0 图片路径.png > /tmp/img.b64
```

```powershell
# Windows PowerShell
[Convert]::ToBase64String([IO.File]::ReadAllBytes("C:\图片路径.png"))
```

### 2. 构造请求（OpenAI 兼容 vision 格式）

```bash
curl -s https://tokenhub.erke.com:3000/v1/chat/completions \
  -H "Authorization: Bearer $GLM46V_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "glm-4.6v",
    "messages": [{
      "role": "user",
      "content": [
        {"type": "image_url", "image_url": {"url": "data:image/png;base64,<BASE64>"}},
        {"type": "text", "text": "<用户的问题>"}
      ]
    }],
    "max_tokens": 1024
  }'
```

**多图**：在 content 数组里放多个 image_url 对象。

**图片 URL**：也可以直接传公网图片链接（`{"type":"image_url","image_url":{"url":"https://..."}}`），不必转 base64。

### 3. 解析响应

```json
{
  "choices": [{
    "message": {
      "role": "assistant",
      "content": "最终回答内容",
      "reasoning_content": "思考过程（可选字段）"
    }
  }],
  "usage": {"prompt_tokens": 31, "completion_tokens": 200, "total_tokens": 231}
}
```

- `content` 是给用户的最终回答，**只把它展示给用户**
- `reasoning_content` 是模型思考过程，默认不展示
- 若 `content` 为空但 `reasoning_content` 有内容：说明 max_tokens 太小，思考过程耗尽了额度，重试时把 max_tokens 加大到 1024+

## 注意事项

1. **max_tokens 至少 512**：glm-4.6v 默认思考模式，思考过程占用输出额度。max_tokens 太小会得到空 content（实际测试 50 token 时 content 为空，200 token 正常）。
2. **图片顺序**：多图按数组顺序理解，对比图时把基准图放前面。
3. **不支持 PDF/视频**：只支持静态图片格式。
4. **失败重试**：401 表示令牌问题；429 表示额度限流（5 小时窗口），告知用户稍后重试；503 表示渠道不可用，告知用户联系管理员。
5. **隐私**：图片会发送到 tokenhub 网关及智谱上游用于识别，敏感图片（证件、密码截图等）先征得用户同意。
