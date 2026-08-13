#!/usr/bin/env python3
"""
glm-4.6v 视觉理解 MCP Server（stdio 模式）

独立脚本，用户本地运行后配置到 AI 客户端（zcode / CodeBuddy / WorkBuddy），
当客户端需要"看图/OCR/截图分析"时通过 MCP 工具调用 tokenhub 的 glm-4.6v。

依赖：pip install mcp requests

配置示例（zcode / CodeBuddy 的 MCP 配置）：
{
  "mcpServers": {
    "glm46v-vision": {
      "command": "python",
      "args": ["<本文件路径>/mcp_server.py"],
      "env": {
        "GLM46V_API_URL": "https://tokenhub.erke.com:3000/v1/chat/completions",
        "GLM46V_API_KEY": "GLM46V_API_KEY_PLACEHOLDER"
      }
    }
  }
}
"""

import base64
import json
import os
import sys
import urllib.request
import urllib.error
from pathlib import Path

from mcp.server.fastmcp import FastMCP

API_URL = os.environ.get(
    "GLM46V_API_URL", "https://tokenhub.erke.com:3000/v1/chat/completions"
)
API_KEY = os.environ.get(
    "GLM46V_API_KEY", "GLM46V_API_KEY_PLACEHOLDER"
)
MODEL = os.environ.get("GLM46V_MODEL", "glm-4.6v")
MAX_TOKENS = int(os.environ.get("GLM46V_MAX_TOKENS", "1024"))

mcp = FastMCP("glm46v-vision")

# 支持的图片 MIME 类型
IMAGE_MIMES = {
    ".png": "image/png",
    ".jpg": "image/jpeg",
    ".jpeg": "image/jpeg",
    ".webp": "image/webp",
    ".gif": "image/gif",
    ".bmp": "image/bmp",
}


def _call_glm46v(image_urls: list[str], question: str, max_tokens: int | None = None) -> str:
    """调用 glm-4.6v。image_urls 支持 data URL 或公网 URL。"""
    content = []
    for url in image_urls:
        content.append({"type": "image_url", "image_url": {"url": url}})
    content.append({"type": "text", "text": question})

    payload = {
        "model": MODEL,
        "messages": [{"role": "user", "content": content}],
        "max_tokens": max_tokens or MAX_TOKENS,
    }
    req = urllib.request.Request(
        API_URL,
        data=json.dumps(payload).encode("utf-8"),
        headers={
            "Authorization": f"Bearer {API_KEY}",
            "Content-Type": "application/json",
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            data = json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", errors="replace")[:500]
        return f"视觉模型调用失败（HTTP {e.code}）：{body}"
    except Exception as e:  # noqa: BLE001
        return f"视觉模型调用异常：{e}"

    try:
        msg = data["choices"][0]["message"]
        answer = msg.get("content") or ""
        # glm-4.6v 默认思考模式：content 可能为空（max_tokens 被思考耗尽）
        if not answer.strip():
            return (
                "模型返回空内容（可能是思考模式耗尽了 max_tokens）。"
                "请重试，并把 max_tokens 参数调大（≥1024）。"
            )
        return answer
    except (KeyError, IndexError, TypeError):
        return f"响应格式异常：{json.dumps(data, ensure_ascii=False)[:500]}"


def _file_to_data_url(path: str) -> str:
    """本地图片文件转 data URL。"""
    p = Path(path).expanduser()
    if not p.exists():
        raise ValueError(f"文件不存在：{path}")
    ext = p.suffix.lower()
    if ext not in IMAGE_MIMES:
        raise ValueError(f"不支持的图片格式：{ext}（支持 png/jpg/jpeg/webp/gif/bmp）")
    b64 = base64.b64encode(p.read_bytes()).decode()
    return f"data:{IMAGE_MIMES[ext]};base64,{b64}"


@mcp.tool()
def understand_image(image_path: str, question: str, max_tokens: int = 1024) -> str:
    """理解本地图片内容。用户提供图片文件路径和问题，返回 glm-4.6v 的回答。

    Args:
        image_path: 图片文件路径（支持 png/jpg/jpeg/webp/gif/bmp，>5MB 建议先压缩）
        question: 要问的问题，例如"这张图里有什么？"、"把图中的文字提取出来"
        max_tokens: 最大输出 token（默认 1024；思考模式会占用输出额度，不要低于 512）

    Returns:
        模型的文字回答
    """
    try:
        url = _file_to_data_url(image_path)
    except ValueError as e:
        return str(e)
    return _call_glm46v([url], question, max_tokens)


@mcp.tool()
def understand_image_url(image_url: str, question: str, max_tokens: int = 1024) -> str:
    """理解公网图片 URL 内容。适合用户发送的图片链接。

    Args:
        image_url: 图片的公网 URL（https://...，必须是可直接访问的直链）
        question: 要问的问题
        max_tokens: 最大输出 token（默认 1024）

    Returns:
        模型的文字回答
    """
    if not image_url.startswith(("http://", "https://")):
        return "image_url 必须是 http/https 开头的公网直链"
    return _call_glm46v([image_url], question, max_tokens)


@mcp.tool()
def compare_images(image_paths: str, question: str, max_tokens: int = 1024) -> str:
    """对比多张图片。图片路径用逗号分隔，按顺序传给模型。

    Args:
        image_paths: 多个图片文件路径，英文逗号分隔，如 "/a.png,/b.png"
        question: 对比问题，如"两张图有什么不同？"
        max_tokens: 最大输出 token（默认 1024）

    Returns:
        模型的文字回答
    """
    urls = []
    for part in image_paths.split(","):
        part = part.strip()
        if not part:
            continue
        try:
            urls.append(_file_to_data_url(part))
        except ValueError as e:
            return str(e)
    if not urls:
        return "未提供有效的图片路径"
    return _call_glm46v(urls, question, max_tokens)


if __name__ == "__main__":
    mcp.run()
