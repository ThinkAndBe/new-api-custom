# Headroom Compress Service（纯压缩模式）
#
# 用 PyPI 上的 headroom-ai 包，配合本地 FastAPI 包装器（app.py），
# 暴露 NEW API 需要的 POST /v1/compress 端点。
#
# 架构：
#   - 纯压缩模式：无 CCR、无 Magika ML、无 Kompress ML
#   - tiktoken 离线：复用 litellm 自带的编码文件
#   - stateless：每个请求独立压缩，无跨用户状态泄漏
#   - NEW API 端有熔断降级：本服务不可用时自动透传原请求
#
# 构建：
#   docker build -t new-api-headroom:latest -f docker/headroom/Dockerfile docker/headroom

# ========== Stage 1: 安装 Python 依赖 ==========
FROM python:3.11-slim AS builder

ENV PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PYTHONUNBUFFERED=1

# 容器内 DNS 受限：换用国内 Debian 镜像源（清华）和 PyPI 镜像源
RUN sed -i 's|http://deb.debian.org|https://mirrors.tuna.tsinghua.edu.cn|g' /etc/apt/sources.list.d/debian.sources 2>/dev/null \
    ; sed -i 's|http://deb.debian.org|https://mirrors.tuna.tsinghua.edu.cn|g' /etc/apt/sources.list 2>/dev/null \
    ; true

RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

RUN pip config set global.index-url https://pypi.tuna.tsinghua.edu.cn/simple

# headroom-ai[all] 包含 litellm（自带 tiktoken 编码文件）、transformers 等
RUN pip install --prefix=/install "headroom-ai[all]==0.10.0"

# FastAPI + Uvicorn（app.py 运行时依赖）
RUN pip install --prefix=/install "fastapi>=0.110" "uvicorn[standard]>=0.29"

# ========== Stage 2: 运行时镜像 ==========
FROM python:3.11-slim

ENV PYTHONUNBUFFERED=1 \
    HEADROOM_HOST=0.0.0.0 \
    HEADROOM_PORT=8787 \
    HEADROOM_WORKERS=1 \
    HF_ENDPOINT=https://hf-mirror.com \
    PATH="/usr/local/bin:$PATH"

WORKDIR /app

# 换源到清华 Debian 镜像
RUN sed -i 's|http://deb.debian.org|https://mirrors.tuna.tsinghua.edu.cn|g' /etc/apt/sources.list.d/debian.sources 2>/dev/null \
    ; sed -i 's|http://deb.debian.org|https://mirrors.tuna.tsinghua.edu.cn|g' /etc/apt/sources.list 2>/dev/null \
    ; true

# 运行时系统依赖：libgomp1（torch/numpy），libstdc++（onnxruntime）
RUN apt-get update \
    && apt-get install -y --no-install-recommends libgomp1 libstdc++6 \
    && rm -rf /var/lib/apt/lists/*

# 拷贝 Python 包（含 litellm 自带的 tiktoken 编码文件）
COPY --from=builder /install /usr/local

# 设置 tiktoken 缓存目录指向 litellm 自带的编码文件
# 这样 tiktoken.get_encoding() 完全离线，不会访问 openaipublic.blob.core.windows.net
ENV TIKTOKEN_CACHE_DIR=/usr/local/lib/python3.11/site-packages/litellm/litellm_core_utils/tokenizers

# 拷贝 app.py
COPY app.py /app/app.py

RUN mkdir -p /models
VOLUME ["/models"]

EXPOSE 8787

HEALTHCHECK --interval=30s --timeout=10s --retries=3 --start-period=30s \
    CMD python -c "import os, urllib.request; urllib.request.urlopen('http://127.0.0.1:%s/livez' % os.getenv('HEADROOM_PORT', '8787'), timeout=5)" || exit 1

CMD ["python", "app.py"]
