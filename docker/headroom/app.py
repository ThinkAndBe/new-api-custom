"""
Headroom /v1/compress 包装服务（纯压缩模式 + Kompress ML）
=============================================================

设计决策：
  1. 纯压缩模式：只做正向压缩，不启用 CCR（可逆压缩检索）。
  2. Kompress ML：启用 ONNX 模型压缩（模型预下载到 /models 目录，完全离线）。
  3. 离线保障：
     a. tiktoken：复用 litellm 自带的编码文件
     b. Kompress ONNX：从 /models/kompress-base 加载，不联网
     c. CCR 标记：关闭 inject_retrieval_marker
     d. Magika ML 检测：关闭，用 regex 回退

请求（POST /v1/compress，Content-Type: application/json）：
  { "model": "gpt-4o", "messages": [ {...}, ... ] }

响应：
  { "messages": [...], "tokens_before": N, "tokens_after": N,
    "tokens_saved": N, "compression_ratio": 0.xx, "transforms_applied": [...] }
"""

from __future__ import annotations

import logging
import os
import shutil
from typing import Any

_logger = logging.getLogger("headroom-compress")

# -------------------------------------------------------------------
# 1. tiktoken 离线：复用 litellm 自带的编码文件
# -------------------------------------------------------------------
try:
    import litellm
    _tiktoken_dir = os.path.join(
        os.path.dirname(litellm.__file__),
        "litellm_core_utils", "tokenizers",
    )
    if os.path.isdir(_tiktoken_dir):
        os.environ["TIKTOKEN_CACHE_DIR"] = _tiktoken_dir
        _logger.info("TIKTOKEN_CACHE_DIR set to litellm bundled dir: %s", _tiktoken_dir)
except Exception as _e:
    _logger.warning("failed to set TIKTOKEN_CACHE_DIR: %s", _e)

# -------------------------------------------------------------------
# 2. Kompress ML 离线：patch hf_hub_download 和 AutoTokenizer
#    让 Headroom 从本地 /models/kompress-base 加载模型，不联网。
#    模型文件需预先下载到 /models/kompress-base/ 和 /models/modernbert-base/
# -------------------------------------------------------------------
KOMPRESS_LOCAL_DIR = os.getenv("KOMPRESS_LOCAL_DIR", "/models/kompress-base")
MODERNBERT_LOCAL_DIR = os.getenv("MODERNBERT_LOCAL_DIR", "/models/modernbert-base")

# Kompress ONNX 模型必须配套 ModernBERT tokenizer（用于分词），
# 二者缺一不可，否则 Kompress 会被判定为不可用并自动禁用。
# 支持两种模型文件名：kompress-int8.onnx (base) 和 kompress-fp32.onnx (v2-base)
_kompress_onnx_path = None
for _fname in ("kompress-int8.onnx", "kompress-fp32.onnx"):
    _p = os.path.join(KOMPRESS_LOCAL_DIR, "onnx", _fname)
    if os.path.isfile(_p):
        _kompress_onnx_path = _p
        break
_kompress_available = (
    _kompress_onnx_path is not None
    and os.path.isfile(os.path.join(MODERNBERT_LOCAL_DIR, "tokenizer.json"))
)

if _kompress_available:
    _logger.info("Kompress ML model found locally at %s", KOMPRESS_LOCAL_DIR)

    # Patch hf_hub_download to return local file paths
    import huggingface_hub
    _orig_hf_hub_download = huggingface_hub.hf_hub_download

    def _local_hub_download(repo_id, filename, **kwargs):
        # Kompress ONNX model (支持 kompress-base / kompress-v2-base，兼容不带前缀的 repo_id)
        if repo_id in ("chopratejas/kompress-base", "chopratejas/kompress-v2-base", "kompress-base", "kompress-v2-base"):
            local_path = os.path.join(KOMPRESS_LOCAL_DIR, filename)
            if os.path.isfile(local_path):
                _logger.debug("hf_hub_download(local): %s/%s -> %s", repo_id, filename, local_path)
                return local_path
            # v2-base 的 onnx 文件名可能不同，尝试两种
            if filename == "onnx/kompress-int8.onnx":
                alt_path = os.path.join(KOMPRESS_LOCAL_DIR, "onnx", "kompress-fp32.onnx")
                if os.path.isfile(alt_path):
                    _logger.debug("hf_hub_download(alt): %s/%s -> %s", repo_id, filename, alt_path)
                    return alt_path
        # ModernBERT tokenizer
        if repo_id == "answerdotai/ModernBERT-base":
            local_path = os.path.join(MODERNBERT_LOCAL_DIR, filename)
            if os.path.isfile(local_path):
                _logger.debug("hf_hub_download(local): %s/%s -> %s", repo_id, filename, local_path)
                return local_path
        # Fallback to original (will fail offline, but that's expected)
        _logger.warning("hf_hub_download: file not found locally: %s/%s", repo_id, filename)
        return _orig_hf_hub_download(repo_id, filename, **kwargs)

    huggingface_hub.hf_hub_download = _local_hub_download

    # Also patch the import in kompress_compressor (it imports directly)
    import headroom.transforms.kompress_compressor as _kc
    _kc.hf_hub_download = _local_hub_download

    # Patch AutoTokenizer.from_pretrained for ModernBERT
    import transformers
    _orig_from_pretrained = transformers.AutoTokenizer.from_pretrained

    @classmethod
    def _local_from_pretrained(cls, pretrained_model_name_or_path, *args, **kwargs):
        if pretrained_model_name_or_path == "answerdotai/ModernBERT-base":
            _logger.debug("AutoTokenizer.from_pretrained(local): %s -> %s",
                         pretrained_model_name_or_path, MODERNBERT_LOCAL_DIR)
            return _orig_from_pretrained.__func__(cls, MODERNBERT_LOCAL_DIR, *args, **kwargs)
        return _orig_from_pretrained.__func__(cls, pretrained_model_name_or_path, *args, **kwargs)

    transformers.AutoTokenizer.from_pretrained = _local_from_pretrained

    # Set offline mode to prevent any network attempts
    os.environ["HF_HUB_OFFLINE"] = "1"
    os.environ["TRANSFORMERS_OFFLINE"] = "1"
else:
    _logger.warning(
        "Kompress ML model NOT found at %s. Kompress will be disabled. "
        "Pre-download model files to enable ML compression.",
        KOMPRESS_LOCAL_DIR,
    )

# -------------------------------------------------------------------
# 3. import headroom 的 compress 函数
# -------------------------------------------------------------------
from headroom import compress as headroom_compress

# -------------------------------------------------------------------
# 4. 替换 singleton pipeline 的 ContentRouter，禁用 CCR 标记注入
# -------------------------------------------------------------------
import importlib as _importlib

from headroom.transforms.pipeline import TransformPipeline as _TP
from headroom.transforms.content_router import (
    ContentRouter as _ContentRouter,
    ContentRouterConfig as _ContentRouterConfig,
)

_pipeline = _TP()
for _i, _t in enumerate(_pipeline.transforms):
    if isinstance(_t, _ContentRouter):
        _pipeline.transforms[_i] = _ContentRouter(
            config=_ContentRouterConfig(
                ccr_enabled=False,
                ccr_inject_marker=False,
            )
        )
        break

_importlib.import_module("headroom.compress")._pipeline = _pipeline

# -------------------------------------------------------------------
# 5. 禁用 Magika ML 内容检测（用 regex 回退）
# -------------------------------------------------------------------
try:
    import headroom.transforms.content_router as _cr
    _cr._magika_status = False
except Exception:
    pass

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse

# -------------------------------------------------------------------
# 配置
# -------------------------------------------------------------------
LOG_LEVEL = os.getenv("HEADROOM_LOG_LEVEL", "INFO").upper()
PROTECT_RECENT = int(os.getenv("HEADROOM_PROTECT_RECENT", "4"))
COMPRESS_USER_MESSAGES = os.getenv("HEADROOM_COMPRESS_USER_MESSAGES", "false").lower() == "true"
MIN_TOKENS_TO_COMPRESS = int(os.getenv("HEADROOM_MIN_TOKENS_TO_COMPRESS", "250"))
# 如果本地模型可用，默认启用 Kompress；否则 disabled
# 注意：传 None 给 headroom.compress.CompressConfig(kompress_model=None) 时，
# headroom 库会解读为"不压缩"，因此默认值必须显式传模型名 "kompress-base"。
_DEFAULT_KOMPRESS_MODEL = "kompress-base"
if _kompress_available:
    KOMPRESS_MODEL = os.getenv("HEADROOM_KOMPRESS_MODEL", "") or _DEFAULT_KOMPRESS_MODEL
else:
    KOMPRESS_MODEL = os.getenv("HEADROOM_KOMPRESS_MODEL", "disabled") or None
COMPRESS_SYSTEM_MESSAGES = os.getenv("HEADROOM_COMPRESS_SYSTEM_MESSAGES", "true").lower() == "true"

logging.basicConfig(
    level=LOG_LEVEL,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
)
logger = logging.getLogger("headroom-compress-service")

logger.info(
    "config: protect_recent=%d compress_user=%s compress_system=%s min_tokens=%d kompress=%s ccr=disabled magika=disabled",
    PROTECT_RECENT, COMPRESS_USER_MESSAGES, COMPRESS_SYSTEM_MESSAGES,
    MIN_TOKENS_TO_COMPRESS, KOMPRESS_MODEL,
)

# -------------------------------------------------------------------
# FastAPI 应用
# -------------------------------------------------------------------
app = FastAPI(
    title="Headroom Compress Service",
    description="无状态上下文压缩 HTTP 服务（纯压缩模式，无 CCR）",
    version="2.0.0",
)


@app.get("/livez")
async def livez() -> dict[str, Any]:
    return {"status": "ok", "service": "headroom-compress"}


@app.get("/readyz")
async def readyz() -> dict[str, Any]:
    return {"status": "ready", "service": "headroom-compress"}


@app.get("/health")
async def health() -> dict[str, Any]:
    return {
        "status": "ok",
        "service": "headroom-compress",
        "version": "2.0.0",
        "config": {
            "protect_recent": PROTECT_RECENT,
            "compress_user_messages": COMPRESS_USER_MESSAGES,
            "compress_system_messages": COMPRESS_SYSTEM_MESSAGES,
            "min_tokens_to_compress": MIN_TOKENS_TO_COMPRESS,
            "kompress_model": KOMPRESS_MODEL,
            "ccr_enabled": False,
            "magika_enabled": False,
        },
    }


@app.post("/v1/compress")
async def compress_endpoint(request: Request) -> JSONResponse:
    """压缩 messages 的主端点。"""
    try:
        body = await request.json()
    except Exception as exc:
        return JSONResponse(status_code=400, content={"error": "invalid JSON body", "detail": str(exc)})

    messages = body.get("messages")
    if not isinstance(messages, list) or not messages:
        return JSONResponse(status_code=400, content={"error": "field 'messages' must be a non-empty list"})

    model = body.get("model", "gpt-4o")

    logger.info("compress request: model=%s messages=%d chars=%d", model, len(messages), sum(len(str(m)) for m in messages))

    try:
        from headroom.compress import CompressConfig

        cfg = CompressConfig(
            compress_user_messages=COMPRESS_USER_MESSAGES,
            compress_system_messages=COMPRESS_SYSTEM_MESSAGES,
            protect_recent=PROTECT_RECENT,
            min_tokens_to_compress=MIN_TOKENS_TO_COMPRESS,
            kompress_model=KOMPRESS_MODEL,
        )
        result = headroom_compress(messages, model=model, config=cfg)
    except Exception as exc:
        logger.exception("compress failed: %s", exc)
        return JSONResponse(status_code=500, content={"error": "compression failed", "detail": str(exc)})

    logger.info(
        "compress done: tokens %d -> %d (saved %d, %.1f%%)",
        result.tokens_before, result.tokens_after,
        result.tokens_saved, result.compression_ratio * 100,
    )

    return JSONResponse(status_code=200, content={
        "messages": result.messages,
        "tokens_before": result.tokens_before,
        "tokens_after": result.tokens_after,
        "tokens_saved": result.tokens_saved,
        "compression_ratio": result.compression_ratio,
        "transforms_applied": getattr(result, "transforms_applied", []),
    })


@app.get("/")
async def root() -> dict[str, str]:
    return {
        "service": "headroom-compress",
        "version": "2.0.0",
        "mode": "compress-only (no CCR, no Magika, no Kompress ML)",
        "endpoints": "/v1/compress, /livez, /readyz, /health, /docs",
    }


if __name__ == "__main__":
    import uvicorn

    host = os.getenv("HEADROOM_HOST", "0.0.0.0")
    port = int(os.getenv("HEADROOM_PORT", "8787"))
    workers = int(os.getenv("HEADROOM_WORKERS", "2"))

    logger.info("starting: host=%s port=%d workers=%d", host, port, workers)

    uvicorn.run(
        "app:app",
        host=host,
        port=port,
        workers=workers,
        log_level=LOG_LEVEL.lower(),
        limit_concurrency=int(os.getenv("HEADROOM_MAX_CONCURRENCY", "500")),
        backlog=int(os.getenv("HEADROOM_BACKLOG", "8192")),
        timeout_keep_alive=int(os.getenv("HEADROOM_KEEPALIVE", "30")),
    )
