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
# 2. Kompress ML 离线：patch huggingface_hub.hf_hub_download
#    让 Headroom 从本地 /models 目录加载模型，不联网。
#    0.33.0 用 kompress-v2-base（repo_id: chopratejas/kompress-v2-base），
#    兼容旧 kompress-base；ONNX 文件名未变（onnx/kompress-int8-wo.onnx 等），
#    v2 额外需要 model.safetensors（PyTorch 权重）。
# -------------------------------------------------------------------
KOMPRESS_LOCAL_DIR = os.getenv("KOMPRESS_LOCAL_DIR", "/models/kompress-base")
MODERNBERT_LOCAL_DIR = os.getenv("MODERNBERT_LOCAL_DIR", "/models/modernbert-base")

# kompress-v2-base 的 ONNX 文件名与 v1 相同；额外有 model.safetensors
_kompress_onnx_path = None
for _fname in ("kompress-int8-wo.onnx", "kompress-fp32.onnx", "kompress-int8.onnx"):
    _p = os.path.join(KOMPRESS_LOCAL_DIR, "onnx", _fname)
    if os.path.isfile(_p):
        _kompress_onnx_path = _p
        break
_kompress_safetensors = os.path.join(KOMPRESS_LOCAL_DIR, "model.safetensors")
_kompress_available = (
    _kompress_onnx_path is not None
    and os.path.isfile(os.path.join(MODERNBERT_LOCAL_DIR, "tokenizer.json"))
)

if _kompress_available:
    _logger.info("Kompress ML model found locally at %s", KOMPRESS_LOCAL_DIR)

    # Patch huggingface_hub.hf_hub_download to return local file paths.
    # 0.33.0 的 headroom.onnx_runtime.hf_hub_download_local_first 内部仍调此函数，
    # 且会传 revision kwarg（_resolve_revision 给已知 repo pin SHA），所以签名要兼容。
    import huggingface_hub
    _orig_hf_hub_download = huggingface_hub.hf_hub_download

    # 兼容所有 kompress repo_id 写法（v1/v2，带不带用户前缀）
    _KOMPRESS_REPOS = {
        "chopratejas/kompress-base", "chopratejas/kompress-v2-base",
        "kompress-base", "kompress-v2-base",
    }

    def _local_hub_download(repo_id, filename, **kwargs):
        # Kompress ONNX/权重
        if repo_id in _KOMPRESS_REPOS:
            # model.safetensors（v2 PyTorch 权重）
            if filename == "model.safetensors":
                if os.path.isfile(_kompress_safetensors):
                    _logger.debug("hf_hub_download(local): %s/%s -> %s", repo_id, filename, _kompress_safetensors)
                    return _kompress_safetensors
            # ONNX 文件：按候选名回退
            if filename.startswith("onnx/"):
                _base = filename.split("/", 1)[1]
                # 先精确，再候选
                _exact = os.path.join(KOMPRESS_LOCAL_DIR, "onnx", _base)
                if os.path.isfile(_exact):
                    return _exact
                for alt in ("kompress-int8-wo.onnx", "kompress-fp32.onnx", "kompress-int8.onnx"):
                    _alt = os.path.join(KOMPRESS_LOCAL_DIR, "onnx", alt)
                    if os.path.isfile(_alt):
                        _logger.debug("hf_hub_download(alt): %s/%s -> %s", repo_id, filename, _alt)
                        return _alt
            # 其它文件（config.json 等）直接映射根目录
            _root_file = os.path.join(KOMPRESS_LOCAL_DIR, filename)
            if os.path.isfile(_root_file):
                return _root_file
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

    # 0.33.0：kompress_compressor 从 ..onnx_runtime import hf_hub_download_local_first，
    # 该函数内部调 huggingface_hub.hf_hub_download（已 patch），无需单独 patch。
    # 但保险起见也 patch onnx_runtime 模块的引用，防止未来版本改用直接 import。
    try:
        import headroom.onnx_runtime as _ort_mod
        _ort_mod.hf_hub_download_local_first = lambda repo_id, filename, **kw: _local_hub_download(repo_id, filename, **{k: v for k, v in kw.items() if k != 'allow_network'})
    except Exception as _e:
        _logger.debug("onnx_runtime patch skipped: %s", _e)

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
# 5. Magika ML 内容检测：0.33.0 起保留启用
#    0.10.0 时禁用 Magika 是因为旧版它不稳定且需联网模型；0.33.0 的 Magika
#    已成熟，且 ContentRouter 依赖它做精准内容分类（JSON/代码/日志/文本），
#    启用后路由更准、压缩率更高。若想强制用 regex 回退，取消下面注释：
# -------------------------------------------------------------------
# try:
#     import sys
#     sys.modules["magika"] = None  # 让 import magika 失败
# except Exception:
#     pass

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
# 0.33.0：kompress-v2-base（repo_id: chopratejas/kompress-v2-base）
# 注意：传 None 给 CompressConfig(kompress_model=None) 时 headroom 库会解读为"不压缩"，
# 因此默认值必须显式传模型名。
_DEFAULT_KOMPRESS_MODEL = "kompress-v2-base"
if _kompress_available:
    KOMPRESS_MODEL = os.getenv("HEADROOM_KOMPRESS_MODEL", "") or _DEFAULT_KOMPRESS_MODEL
else:
    KOMPRESS_MODEL = os.getenv("HEADROOM_KOMPRESS_MODEL", "disabled") or None
COMPRESS_SYSTEM_MESSAGES = os.getenv("HEADROOM_COMPRESS_SYSTEM_MESSAGES", "true").lower() == "true"
# 0.30.0+：强制保留比例。None=模型自决定，0.3=保留30%（激进），0.5=保留50%（安全）
_target_ratio_str = os.getenv("HEADROOM_TARGET_RATIO", "")
TARGET_RATIO = float(_target_ratio_str) if _target_ratio_str else None

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

        cfg_kwargs = dict(
            compress_user_messages=COMPRESS_USER_MESSAGES,
            compress_system_messages=COMPRESS_SYSTEM_MESSAGES,
            protect_recent=PROTECT_RECENT,
            min_tokens_to_compress=MIN_TOKENS_TO_COMPRESS,
            kompress_model=KOMPRESS_MODEL,
        )
        if TARGET_RATIO is not None:
            cfg_kwargs["target_ratio"] = TARGET_RATIO
        cfg = CompressConfig(**cfg_kwargs)
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
        "mode": "compress-only (no CCR, no Magika; Kompress-v2 when model present)",
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
