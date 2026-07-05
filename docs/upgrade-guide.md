# 升级部署指南

> 从原版 new-api 升级到二开版本（含 Headroom 压缩 + 渠道并发控制 + 看板等）

## 前置条件

- 已安装 Docker + Docker Compose + Git
- 原版 new-api 正在运行（docker compose 方式）
- 已打虚拟机快照（ rollback 保障）

---

## 一、备份现有环境

```bash
# 进入原版 new-api 部署目录
cd /path/to/new-api

# 1. 备份数据库
cp -r ./data ./data.backup.$(date +%Y%m%d)

# 2. 备份配置
cp docker-compose.yml docker-compose.yml.backup

# 3. 导出数据库（如果用 PostgreSQL）
docker compose exec -T postgres pg_dump -U root new-api > backup.sql

# 4. 打虚拟机快照（此时）
```

---

## 二、拉取二开代码

```bash
# 在部署目录操作（或新建目录）
cd /path/to/new-api

# 添加二开远程仓库
git remote add myfork https://github.com/bugking2493/new-api.git

# 拉取二开分支
git fetch myfork
git checkout feature/custom-export-and-price-sync
git pull myfork feature/custom-export-and-price-sync
```

---

## 三、构建新镜像

### 3.1 构建 new-api 镜像

```bash
# 修改 docker-compose.yml，使用 build 而非官方镜像
# 将 image: calciumion/new-api:latest 改为：
#   build:
#     context: .
#     dockerfile: Dockerfile

# 构建
docker compose build new-api
```

### 3.2 构建 Headroom 镜像

```bash
# 构建 headroom 压缩服务镜像
docker compose -f docker-compose.headroom.yml build headroom
```

---

## 四、配置 docker-compose

### 4.1 使用一体化编排文件

如果原部署用的是 `docker-compose.yml`（含 PostgreSQL），保持不变，只需额外启动 headroom：

```bash
# 单独启动 headroom 服务
docker compose -f docker-compose.headroom.yml up -d headroom
```

如果原部署用的是 SQLite 单机模式，直接用一体化编排：

```bash
# 停止旧服务
docker compose down

# 用一体化编排启动
docker compose -f docker-compose.headroom.yml up -d
```

### 4.2 new-api 环境变量（在 docker-compose.yml 中添加）

```yaml
services:
  new-api:
    environment:
      # ... 原有配置保留 ...
      
      # Headroom 压缩服务地址（docker 网络内通过服务名访问）
      - HEADROOM_URL=http://headroom:8787
      # 全局开启 Headroom（渠道里仍需单独打开开关）
      - HEADROOM_GLOBAL_ENABLED=true
```

### 4.3 Headroom 服务配置

`docker-compose.headroom.yml` 已包含全部优化配置，关键参数：

```yaml
headroom:
  environment:
    - HEADROOM_WORKERS=1                    # worker 数（Docker 内建议 1）
    - HEADROOM_MAX_CONCURRENCY=50           # 最大并发压缩数
    - HEADROOM_KOMPRESS_MODEL=              # 空=启用本地 Kompress ML
    - HEADROOM_PROTECT_RECENT=2             # 保护最近 2 轮不压缩
    - HEADROOM_MIN_TOKENS_TO_COMPRESS=100   # 最小压缩阈值
    - HEADROOM_COMPRESS_USER_MESSAGES=true  # 用户消息参与压缩
    - HF_HOME=/models                       # 模型存储路径
    - HF_ENDPOINT=https://hf-mirror.com     # 国内 HuggingFace 镜像
  volumes:
    - ./headroom-models:/models             # 模型持久化
    - ./headroom-logs:/app/logs             # 日志持久化
  ports:
    - "8788:8787"                           # 宿主机调试用（可选）
```

---

## 五、启动服务

```bash
# 1. 启动 headroom（首次需要下载模型，约 2-5 分钟）
docker compose -f docker-compose.headroom.yml up -d headroom

# 等待 headroom 健康
docker compose -f docker-compose.headroom.yml logs -f headroom
# 看到 "Application startup complete" 表示就绪

# 2. 重启 new-api（加载新镜像）
docker compose restart new-api
# 或如果用了 headroom 编排：
docker compose -f docker-compose.headroom.yml restart new-api

# 3. 验证
curl http://localhost:3000/api/status
curl http://localhost:8788/livez
```

---

## 六、数据库配置（管理后台操作）

启动后，登录管理后台做以下配置：

### 6.1 系统设置

进入 **设置 → 运营设置**：

| 配置项 | 值 | 说明 |
|--------|-----|------|
| RetryTimes | `3` | 429 时自动重试到其他渠道 |
| CPU 监控阈值 | `99` | 避免误杀正常请求 |
| Headroom 留存天数 | `30` | 压缩日志保留天数 |

### 6.2 渠道配置

进入 **渠道管理 → 编辑每个渠道**：

| 配置项 | 值 | 说明 |
|--------|-----|------|
| 启用 Headroom 压缩 | ✅ 开 | 开启压缩 |
| Headroom 地址 | `http://headroom:8787` | docker 网络内用服务名 |
| 最大并发 | `0` | 不限（靠 retry 机制溢出） |

**所有渠道都启用，地址统一填 `http://headroom:8787`**

### 6.3 Headroom 看板

进入 **侧边栏 → Headroom 看板**：
- 确认 KPI 卡片有数据
- 确认月度/年度汇总表正常
- 确认趋势图正常

---

## 七、使用一键升级脚本（推荐）

如果之前已部署过二开版本，后续升级用脚本：

```bash
# 普通升级（拉代码 + 构建镜像 + 滚动重启 + 健康检查）
./upgrade.sh

# 回滚上次升级
./upgrade.sh --rollback
```

脚本会自动：
1. 备份当前镜像和数据
2. git pull 拉取最新代码
3. docker compose build 构建新镜像
4. 滚动重启服务
5. 健康检查（失败自动回滚）

---

## 八、验证清单

升级后逐项验证：

```bash
# 1. 服务状态
docker compose ps
# new-api: Up (healthy)
# headroom: Up (healthy)

# 2. API 可用
curl -s http://localhost:3000/api/status | grep success

# 3. Headroom 可用
curl -s http://localhost:8788/livez

# 4. 发一个测试请求
curl -s http://localhost:3000/v1/chat/completions \
  -H "Authorization: Bearer sk-YOUR-TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"model":"glm-5.2","messages":[{"role":"user","content":"hi"}],"max_tokens":5}'

# 5. 检查 headroom 压缩日志
docker compose -f docker-compose.headroom.yml logs headroom --tail 5

# 6. 检查 Headroom 看板有数据
# 浏览器访问 http://localhost:3000 → 侧边栏 → Headroom 看板
```

---

## 九、本次升级包含的全部变更

### 新功能
- **Headroom 压缩集成**：Kompress ML 压缩，290ms 处理 314K token
- **Headroom 看板**：KPI/排行/趋势/月年汇总/明细/CSV 导出
- **渠道并发控制**：`max_concurrency` 字段 + 满时智能 fallback
- **doubao-agent-plan 支持**：Claude→OpenAI 协议转换
- **使用日志增强**：total_tokens / request_count 统计
- **官方价格同步**：models.dev 数据源 + 国产厂商白名单
- **渠道健康检查**：定时探测 + 自动禁用/启用

### Bug 修复
- Headroom `compression_ratio` 语义修正（保留率→节省率）
- Token 计数准确性（统一 prompt_tokens 修正 + completion_tokens 补全）
- 压缩超时 30s→5s
- 429 不再自动禁用渠道（改为 retry）
- Claude 风格 tool_use/tool_result 转 OpenAI 格式

### 配置变更
- `RetryTimes` = 3
- `monitor_cpu_threshold` = 99
- 429 从自动禁用范围中移除
- Headroom 参数调优（protect_recent=2, min_tokens=100, compress_user=true）

---

## 十、回滚方案

### 方案 A：虚拟机快照回滚
直接恢复快照（最简单可靠）。

### 方案 B：脚本回滚
```bash
./upgrade.sh --rollback
```

### 方案 C：手动回滚
```bash
# 停止服务
docker compose down

# 恢复备份的 docker-compose.yml
cp docker-compose.yml.backup docker-compose.yml

# 恢复数据
cp -r ./data.backup.* ./data

# 用旧镜像启动
docker compose up -d
```

---

## 十一、性能基准（10 人并发）

| 场景 | 成功率 | avg 延迟 | CPU |
|------|--------|---------|-----|
| 10 人中等上下文（57K tokens） | 100% | 2.7s | 35% |
| 15 人中等上下文 + 0.5s 错峰 | 100% | 3.7s | 39% |
| 压缩 314K token（单次） | - | 0.29s | - |

**10 人并发完全够用，压缩不是瓶颈。**
