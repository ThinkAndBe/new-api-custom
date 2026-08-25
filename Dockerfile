FROM oven/bun:1@sha256:0733e50325078969732ebe3b15ce4c4be5082f18c4ac1a0f0ca4839c2e4e42a7 AS builder-classic

WORKDIR /build/web
COPY web/package.json web/bun.lock ./
COPY web/default/package.json ./default/package.json
COPY web/classic/package.json ./classic/package.json
RUN bun install
COPY ./web/classic ./classic
COPY ./VERSION /build/VERSION
RUN cd classic && VITE_REACT_APP_VERSION=$(cat /build/VERSION) bun run build

# default 前端：fork 主要使用 classic 主题，default 仅保留占位
# 如需启用 default 主题，取消下面注释并确保 web/default 依赖完整
RUN mkdir -p /build/web/default/dist && echo '<!DOCTYPE html><html><head><title>New API</title></head><body>Default theme not built. Use classic theme.</body></html>' > /build/web/default/dist/index.html

FROM golang:1.26.1-alpine@sha256:2389ebfa5b7f43eeafbd6be0c3700cc46690ef842ad962f6c5bd6be49ed82039 AS builder2
ENV GO111MODULE=on CGO_ENABLED=0 GOPROXY=https://goproxy.cn,direct

ARG TARGETOS
ARG TARGETARCH
ENV GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64}
ENV GOEXPERIMENT=greenteagc

WORKDIR /build

ADD go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=builder-classic /build/web/classic/dist ./web/classic/dist
COPY --from=builder-classic /build/web/default/dist ./web/default/dist
RUN go build -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=$(cat VERSION)'" -o new-api

FROM debian:bookworm-slim@sha256:f06537653ac770703bc45b4b113475bd402f451e85223f0f2837acbf89ab020a

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata libasan8 wget \
    && rm -rf /var/lib/apt/lists/* \
    && update-ca-certificates

COPY --from=builder2 /build/new-api /
COPY --from=builder2 /build/config-tool/erke-config-tool.exe /config-tool/erke-config-tool.exe
COPY LICENSE NOTICE THIRD-PARTY-LICENSES.md /licenses/
EXPOSE 3000 80
WORKDIR /data
ENTRYPOINT ["/new-api"]
