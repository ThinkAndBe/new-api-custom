# 完整升级步骤：原版 new-api (Docker Compose + PostgreSQL) → 二开版本

> **目标**：保留原有用户、渠道、令牌、配置，新增 Headroom 压缩 + 看板 + 渠道并发控制等功能

---

## 第 0 步：打虚拟机快照

```
在虚拟机管理平台上对目标机器打快照，命名为 "升级前-原版newapi"
```

---

## 第 1 步：备份现有数据

```bash
# 进入原版 new-api 部署目录
cd /path/to/new-api

# 1.1 备份 PostgreSQL 数据库
docker compose exec -T postgres pg_dump -U root new-api > backup_$(date +%Y%m%d_%H%M%S).sql

# 1.2 备份 data 目录（包含日志、上传文件等）
cp -r ./data ./data.backup.$(date +%Y%m%d)

# 1.3 备份 docker-compose.yml（保留你的密码配置）
cp docker-compose.yml docker-compose.yml.backup

# 1.4 备份 .env 文件（如果有）
[ -f .env ] && cp .env .env.backup

# 1.5 确认备份文件
ls -lh backup_*.sql docker-compose.yml.backup data.backup.*
```

---

## 第 2 步：停止原版服务

```bash
# 停止 new-api 容器（保留 postgres 和 redis 不停，避免连接中断）
docker compose stop new-api

# 确认 new-api 已停止
docker compose ps
# new-api 应该显示 Exited，postgres 和 redis 仍在运行
```

---

## 第 3 步：拉取二开代码

```bash
# 3.1 如果原部署目录不是 git 仓库，先初始化
cd /path/to/new-api
git init
git remote add origin https://github.com/QuantumNous/new-api.git

# 3.2 添加二开远程
git remote add myfork https://github.com/bugking2493/new-api.git

# 3.3 拉取二开分支
git fetch myfork
git checkout feature/custom-export-and-price-sync
git pull myfork feature/custom-export-and-price-sync

# 如果有冲突（原配置文件等），保留你的本地修改：
# git stash → git checkout → git stash pop
```

---

## 第 4 步：修改 docker-compose.yml

编辑 `docker-compose.yml`，做以下 3 处修改：

### 4.1 改为本地构建（不用官方镜像）

```yaml
services:
  new-api:
    # 删除这行：image: calciumion/new-api:latest
    # 改为：
    build:
      context: .
      dockerfile: Dockerfile
    container_name: new-api
```

### 4.2 添加 Headroom 环境变量

在 `new-api` 的 `environment` 部分添加：

```yaml
    environment:
      # === 原有配置保留不动 ===
      - SQL_DSN=postgresql://root:你的密码@postgres:5432/new-api
      - REDIS_CONN_STRING=redis://:你的密码@redis:6379
      - TZ=Asia/Shanghai
      - ERROR_LOG_ENABLED=true
      - BATCH_UPDATE_ENABLED=true
      - NODE_NAME=new-api-node-1
      
      # === 新增：Headroom 压缩 ===
      - HEADROOM_URL=http://headroom:8787
      - HEADROOM_GLOBAL_ENABLED=true
```

### 4.3 添加 Headroom 服务定义

在 `docker-compose.yml` 末尾的 `services` 下追加（与 redis/postgres 同级）：

```yaml
  # Headroom 压缩服务
  headroom:
    build:
      context: ./docker/headroom
      dockerfile: Dockerfile
    image: new-api-headroom:latest
    container_name: headroom
    restart: always
    environment:
      - HEADROOM_HOST=0.0.0.0
      - HEADROOM_PORT=8787
      - HEADROOM_WORKERS=1
      - HEADROOM_MAX_CONCURRENCY=50
      - HEADROOM_FORCE_MODEL=new-api-generic
      - HEADROOM_KOMPRESS_MODEL=
      - HEADROOM_PROTECT_RECENT=2
      - HEADROOM_MIN_TOKENS_TO_COMPRESS=100
      - HEADROOM_COMPRESS_USER_MESSAGES=true
      - HF_HOME=/models
      - HF_ENDPOINT=https://hf-mirror.com
      - HEADROOM_LOG_LEVEL=INFO
      - TZ=Asia/Shanghai
    volumes:
      - ./headroom-models:/models
      - ./headroom-logs:/app/logs
    ports:
      - "8788:8787"
    networks:
      - new-api-network
    healthcheck:
      test: ["CMD-SHELL", "python -c \"import urllib.request; urllib.request.urlopen('http://127.0.0.1:8787/livez', timeout=5)\" || exit 1"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 90s
```

在 `new-api` 的 `depends_on` 里加上 headroom：

```yaml
    depends_on:
      - redis
      - postgres
      - headroom
```

---

## 第 5 步：构建镜像

```bash
# 5.1 构建 headroom 镜像（先构建，因为 new-api 依赖它）
docker compose build headroom

# 5.2 构建 new-api 二开镜像
docker compose build new-api

# 构建过程约 5-10 分钟（Go 编译 + 前端打包 + Python 依赖安装）
```

---

## 第 6 步：启动 Headroom 服务

```bash
# 先单独启动 headroom（首次需要下载 Kompress ML 模型，约 2-5 分钟）
docker compose up -d headroom

# 观察启动日志，等待模型下载完成
docker compose logs -f headroom

# 看到 "Application startup complete" 表示就绪
# Ctrl+C 退出日志查看
```

---

## 第 7 步：启动 new-api

```bash
# 启动 new-api（使用新构建的二开镜像）
docker compose up -d new-api

# 查看启动日志
docker compose logs -f new-api

# 看到 "server started" 或 "listening on :3000" 表示成功
```

---

## 第 8 步：验证服务

```bash
# 8.1 检查所有容器状态
docker compose ps
# new-api: Up (healthy)
# headroom: Up (healthy)
# postgres: Up
# redis: Up

# 8.2 API 可用性
curl -s http://localhost:3000/api/status | python3 -m json.tool

# 8.3 Headroom 可用性
curl -s http://localhost:8788/livez

# 8.4 浏览器访问
# http://你的服务器IP:3000 → 用原管理员账号登录

# 8.5 验证原有数据
# - 用户管理 → 确认原有用户都在
# - 渠道管理 → 确认原有渠道都在
# - 令牌管理 → 确认原有令牌都在
# - 使用日志 → 确认原有日志都在
```

---

## 第 9 步：管理后台配置

登录管理后台，做以下配置：

### 9.1 运营设置

进入 **设置 → 运营设置**：

| 配置项 | 值 | 原因 |
|--------|-----|------|
| 重试次数 (RetryTimes) | `3` | 429 时自动重试到其他渠道 |
| CPU 监控阈值 | `99` | 避免压缩时 CPU 飙升误杀请求 |
| Headroom 留存天数 | `30` | 压缩日志保留 30 天 |

### 9.2 渠道配置（每个渠道都要改）

进入 **渠道管理 → 编辑 → 扩展设置**，对每个渠道：

| 配置项 | 值 |
|--------|-----|
| 启用 Headroom 压缩 | ✅ 打开 |
| Headroom 地址 | `http://headroom:8787` |
| 最大并发 | `0`（不限，靠重试机制处理溢出） |

> **注意**：Headroom 地址在 Docker 网络内用服务名 `headroom`，不是 `127.0.0.1`

### 9.3 验证 Headroom 看板

进入 **侧边栏 → Headroom 看板**：
- 此时应该没有数据（刚部署）
- 发一个测试请求后刷新，应该出现 1 条记录

### 9.4 发测试请求

```bash
curl -s http://localhost:3000/v1/chat/completions \
  -H "Authorization: Bearer sk-你的令牌" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "glm-5.2",
    "messages": [{"role": "user", "content": "hello"}],
    "max_tokens": 10
  }'

# 发完后检查 Headroom 看板，应该有压缩记录
# 检查 Headroom 日志
docker compose logs headroom --tail 5
```

---

## 第 10 步：清理旧资源（可选，确认稳定后执行）

```bash
# 10.1 删除旧镜像
docker image prune -f

# 10.2 删除备份的 data 目录（保留 sql 备份）
# rm -rf ./data.backup.*

# 10.3 删除临时文件
# rm -f backup_*.sql
```

---

## 回滚方案

### 方案 A：虚拟机快照恢复（推荐）
直接在虚拟机平台恢复快照。

### 方案 B：手动回滚

```bash
# 1. 停止所有服务
docker compose down

# 2. 恢复原版 docker-compose.yml
cp docker-compose.yml.backup docker-compose.yml

# 3. 恢复原版镜像
docker pull calciumion/new-api:latest

# 4. 启动（不含 headroom）
docker compose up -d

# 5. 如果数据库被改坏，恢复 SQL
docker compose exec -T postgres psql -U root new-api < backup_YYYYMMDD.sql
```

---

## 升级后检查清单

| 检查项 | 方法 | 预期结果 |
|--------|------|---------|
| 原有用户 | 管理后台 → 用户管理 | 用户列表完整 |
| 原有渠道 | 管理后台 → 渠道管理 | 渠道列表完整 |
| 原有令牌 | 管理后台 → 令牌管理 | 令牌列表完整 |
| 原有日志 | 管理后台 → 使用日志 | 历史日志可查 |
| 原有设置 | 管理后台 → 设置 | 比率、限制等配置保留 |
| API 可用 | curl 测试请求 | 返回正常 |
| Headroom | curl /livez | {"status":"ok"} |
| 压缩生效 | 发请求后看日志 | "compress done: tokens X -> Y" |
| 看板有数据 | Headroom 看板 | 至少 1 条记录 |
| 渠道测试 | 渠道管理 → 测试 | 渠道正常响应 |

---

## 常见问题

### Q: 升级后原有用户/渠道不见了？
A: PostgreSQL 数据在 `pg_data` volume 里，不会因为重建容器丢失。检查 `docker volume ls` 确认 volume 存在。

### Q: Headroom 启动失败？
A: 首次启动需要下载模型，确保网络能访问 `hf-mirror.com`。查看日志：`docker compose logs headroom`

### Q: 渠道测试报 "unsupported protocol scheme"？
A: Headroom 地址填错了。Docker 网络内用 `http://headroom:8787`，不是 `http://127.0.0.1:8787`。

### Q: CPU 100% 报错？
A: 管理后台 → 运营设置 → CPU 监控阈值改为 `99`。压缩时 CPU 会短暂飙升，属于正常现象。

### Q: 429 频繁？
A: 确认重试次数=3。4 个渠道全部启用。重试机制会自动将 429 的请求转发到其他渠道。
