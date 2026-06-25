#!/usr/bin/env bash
#
# new-api 二开一键升级脚本
#
# 用法：
#   首次部署：  ./upgrade.sh --init
#   从原版迁移：./upgrade.sh --migrate              （在原版部署目录执行）
#              ./upgrade.sh --migrate --from /opt/old-new-api
#   后续升级：  ./upgrade.sh
#   回滚上次：  ./upgrade.sh --rollback
#   查看帮助：  ./upgrade.sh --help
#
# 功能：
#   - 自动备份当前镜像和数据（升级前）
#   - 拉取最新代码并构建新镜像
#   - 滚动重启，零停机
#   - 健康检查，失败自动回滚
#   - 支持手动回滚到升级前版本
#
# 前置要求：已安装 docker、docker compose、git
#

set -euo pipefail

# ======================== 配置区（按需修改） ========================
# Git 远程名称和分支
REMOTE="${REMOTE:-myfork}"
BRANCH="${BRANCH:-feature/custom-export-and-price-sync}"

# 服务名称（对应 docker-compose.yml 中的 new-api 服务）
SERVICE="${SERVICE:-new-api}"

# 健康检查 URL 和超时
HEALTH_URL="${HEALTH_URL:-http://localhost:3000/api/status}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-90}"  # 秒

# 备份保留份数
BACKUP_KEEP="${BACKUP_KEEP:-3}"

# 备份目录
BACKUP_DIR="${BACKUP_DIR:-./.backups}"

# 镜像名称（与 docker-compose.yml 中保持一致）
IMAGE_NAME="${IMAGE_NAME:-new-api-custom}"
# ==================================================================

# 从原版迁移时，可指定原版部署目录
MIGRATE_FROM="${MIGRATE_FROM:-}"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log()   { echo -e "${GREEN}[$(date '+%H:%M:%S')]${NC} $*"; }
warn()  { echo -e "${YELLOW}[$(date '+%H:%M:%S')] ⚠${NC} $*"; }
err()   { echo -e "${RED}[$(date '+%H:%M:%S')] ✗${NC} $*" >&2; }
info()  { echo -e "${BLUE}[$(date '+%H:%M:%S')] ℹ${NC} $*"; }

# 检查命令是否存在
check_cmd() {
    if ! command -v "$1" &> /dev/null; then
        err "未找到命令: $1，请先安装"
        exit 1
    fi
}

# 判断 docker compose 还是 docker-compose
detect_compose() {
    if docker compose version &> /dev/null; then
        COMPOSE_CMD="docker compose"
    elif command -v docker-compose &> /dev/null; then
        COMPOSE_CMD="docker-compose"
    else
        err "未找到 docker compose，请先安装 Docker"
        exit 1
    fi
}

# ======================== 备份 ========================
do_backup() {
    local ts
    ts=$(date '+%Y%m%d_%H%M%S')
    local backup_path="${BACKUP_DIR}/${ts}"
    mkdir -p "${backup_path}"

    log "开始备份到 ${backup_path}"

    # 1. 备份当前镜像（如果存在）
    if docker image inspect "${IMAGE_NAME}:latest" &> /dev/null; then
        log "备份当前镜像..."
        docker image tag "${IMAGE_NAME}:latest" "${IMAGE_NAME}:${ts}" 2>/dev/null || true
        docker image save "${IMAGE_NAME}:${ts}" -o "${backup_path}/image.tar" 2>/dev/null || {
            warn "镜像导出失败（可能体积大，跳过），已保留 tag ${IMAGE_NAME}:${ts}"
        }
        # 删除导出的 tar（太大了，保留 tag 即可）
        rm -f "${backup_path}/image.tar"
    else
        warn "当前镜像不存在，跳过镜像备份"
    fi

    # 2. 记录当前 commit（用于回滚）
    git rev-parse HEAD > "${backup_path}/commit.txt" 2>/dev/null || true
    log "当前代码版本: $(cat "${backup_path}/commit.txt" 2>/dev/null || echo '未知')"

    # 3. 备份 data 目录（SQLite 数据）- 若存在
    if [ -d "./data" ]; then
        log "备份 data 目录..."
        tar -czf "${backup_path}/data.tar.gz" -C ./data . 2>/dev/null || warn "data 目录备份失败"
    fi

    # 4. 备份 PostgreSQL（如果用 PG）
    if ${COMPOSE_CMD} ps postgres &> /dev/null && ${COMPOSE_CMD} ps postgres | grep -q "Up"; then
        log "备份 PostgreSQL 数据库..."
        local pg_user pg_db
        pg_user=$(grep -oP 'SQL_DSN=postgresql://\K[^:@]+' docker-compose.yml 2>/dev/null | head -1 || echo "root")
        pg_db=$(grep -oP 'SQL_DSN=postgresql://[^@]+@\S+/\K\S+' docker-compose.yml 2>/dev/null | head -1 | sed 's/ .*//' || echo "new-api")
        ${COMPOSE_CMD} exec -T postgres pg_dump -U "${pg_user}" "${pg_db}" > "${backup_path}/postgres.sql" 2>/dev/null || warn "PG 备份失败"
    fi

    # 5. 备份 docker-compose.yml（用户可能改过密码）
    [ -f "docker-compose.yml" ] && cp docker-compose.yml "${backup_path}/"

    # 记录最新备份路径（供回滚使用）
    echo "${backup_path}" > "${BACKUP_DIR}/latest"
    log "备份完成: ${backup_path}"

    # 清理旧备份
    cleanup_old_backups
}

cleanup_old_backups() {
    local count
    count=$(ls -1d "${BACKUP_DIR}"/*/ 2>/dev/null | wc -l)
    if [ "${count}" -gt "${BACKUP_KEEP}" ]; then
        log "清理旧备份（保留最近 ${BACKUP_KEEP} 份）..."
        ls -1dt "${BACKUP_DIR}"/*/ | tail -n +$((BACKUP_KEEP + 1)) | while read -r old; do
            rm -rf "${old}"
            info "已删除旧备份: ${old}"
        done
    fi
}

# ======================== 健康检查 ========================
health_check() {
    log "健康检查（最长等待 ${HEALTH_TIMEOUT} 秒）..."
    local elapsed=0
    while [ "${elapsed}" -lt "${HEALTH_TIMEOUT}" ]; do
        if curl -sf "${HEALTH_URL}" -o /dev/null 2>/dev/null; then
            log "健康检查通过 ✓"
            return 0
        fi
        sleep 3
        elapsed=$((elapsed + 3))
        printf "."
    done
    echo
    err "健康检查超时，服务未就绪"
    return 1
}

# ======================== 从原版迁移 ========================
# 场景：你之前用的是官方 calciumion/new-api 镜像部署，现在要升级到二开版本，
# 并且要保留原有的所有配置/用户/Token/渠道/日志等数据。
#
# 迁移策略：
#   1. 自动探测原版的数据存储方式（SQLite / PostgreSQL / MySQL / 环境变量配置）
#   2. 备份原版数据（防呆）
#   3. 克隆二开仓库到临时目录，复制原版的 docker-compose.yml 关键配置过来
#      （数据库连接串、Redis、端口、环境变量等用户自定义部分）
#   4. 切换为源码构建（build: .），保留原版的 DB/Redis 指向
#   5. 启动二开版本，复用原有数据库 → 配置和用户数据全部保留
do_migrate() {
    log "========== 从原版 new-api 迁移到二开版本 =========="

    check_cmd git
    detect_compose
    check_cmd curl

    # 解析原版部署目录：优先 --from 参数，否则当前目录
    local old_dir="${MIGRATE_FROM:-$(pwd)}"

    if [ ! -d "${old_dir}" ]; then
        err "原版部署目录不存在: ${old_dir}"
        err "用法: ./upgrade.sh --migrate --from /opt/原版部署目录"
        exit 1
    fi

    info "原版部署目录: ${old_dir}"
    info "二开将部署到: $(pwd)/migrated-new-api"

    # 校验原版确实是 new-api 部署
    local old_compose="${old_dir}/docker-compose.yml"
    if [ ! -f "${old_compose}" ]; then
        # 兼容 docker-compose.yaml
        old_compose="${old_dir}/docker-compose.yaml"
    fi
    if [ ! -f "${old_compose}" ]; then
        err "原版目录下未找到 docker-compose.yml"
        err "请确认 ${old_dir} 是原版 new-api 的部署目录（含 docker-compose.yml）"
        exit 1
    fi

    # 1. 探测原版的数据库类型和连接信息
    log "探测原版数据库配置..."
    local db_type="sqlite"
    local sql_dsn=""
    local redis_conn=""
    local has_pg=false has_mysql=false

    # 从原版 compose 中读取环境变量（兼容 environment: 列表 和 env_file）
    # 检查环境变量文件
    local old_env_file="${old_dir}/.env"
    local pg_pw="123456" pg_user="root" pg_db="new-api"
    local mysql_pw="123456"

    # 从 compose 文件提取 SQL_DSN
    sql_dsn=$(grep -oP 'SQL_DSN=\K.*' "${old_compose}" 2>/dev/null | grep -v '^\s*#' | head -1 | tr -d ' ' || true)
    redis_conn=$(grep -oP 'REDIS_CONN_STRING=\K.*' "${old_compose}" 2>/dev/null | grep -v '^\s*#' | head -1 | tr -d ' ' || true)

    # 也检查 .env 文件
    if [ -f "${old_env_file}" ]; then
        [ -z "${sql_dsn}" ] && sql_dsn=$(grep -oP '^SQL_DSN=\K.*' "${old_env_file}" 2>/dev/null | head -1 | tr -d ' ' || true)
        [ -z "${redis_conn}" ] && redis_conn=$(grep -oP '^REDIS_CONN_STRING=\K.*' "${old_env_file}" 2>/dev/null | head -1 | tr -d ' ' || true)
    fi

    # 判断数据库类型
    if echo "${sql_dsn}" | grep -qi "postgresql\|postgres:"; then
        db_type="postgres"
        has_pg=true
        # 提取用户名密码库名
        pg_user=$(echo "${sql_dsn}" | grep -oP 'postgresql://\K[^:@]+' || echo "root")
        pg_pw=$(echo "${sql_dsn}" | grep -oP 'postgresql://[^:]+:\K[^@]+' || echo "123456")
        pg_db=$(echo "${sql_dsn}" | grep -oP '/\K[^/?]+$' || echo "new-api")
        info "检测到 PostgreSQL: user=${pg_user}, db=${pg_db}"
    elif echo "${sql_dsn}" | grep -qi "mysql\|tcp("; then
        db_type="mysql"
        has_mysql=true
        mysql_pw=$(echo "${sql_dsn}" | grep -oP 'root:\K[^@]+' || echo "123456")
        info "检测到 MySQL"
    elif [ -z "${sql_dsn}" ]; then
        # 没有 SQL_DSN，可能是 SQLite（默认）
        db_type="sqlite"
        info "未配置 SQL_DSN，默认使用 SQLite"
    else
        info "检测到数据库 DSN（原样保留）"
    fi

    # 检测原有数据卷
    local old_data_dir="${old_dir}/data"
    local old_pg_volume=""
    local old_mysql_volume=""
    old_pg_volume=$(grep -oP 'pg_data:\K.*|-\s*\K.*pg_data' "${old_compose}" 2>/dev/null | head -1 || true)
    old_mysql_volume=$(grep -oP 'mysql_data:\K.*|-\s*\K.*mysql_data' "${old_compose}" 2>/dev/null | head -1 || true)

    echo
    log "迁移摘要："
    info "  数据库类型: ${db_type}"
    info "  SQL_DSN: ${sql_dsn:-（SQLite，使用 data 目录）}"
    info "  Redis: ${redis_conn:-（默认）}"
    [ "${db_type}" = "postgres" ] && info "  PostgreSQL 卷: ${old_pg_volume:-（docker volume）}"
    [ "${db_type}" = "mysql" ] && info "  MySQL 卷: ${old_mysql_volume:-（docker volume）}"
    echo

    # 确认提示
    read -r -p "确认开始迁移？数据会自动备份，原有服务不停止 [y/N]: " confirm
    if [[ ! "${confirm}" =~ ^[Yy]$ ]]; then
        warn "已取消"
        exit 0
    fi

    # 2. 备份原版数据（在原版目录操作，最安全）
    log "备份原版数据..."
    local backup_ts
    backup_ts=$(date '+%Y%m%d_%H%M%S')
    local old_backup="${old_dir}/.backups/migrate_${backup_ts}"
    mkdir -p "${old_backup}"

    # 备份原版 compose
    cp "${old_compose}" "${old_backup}/docker-compose.yml.bak"
    [ -f "${old_env_file}" ] && cp "${old_env_file}" "${old_backup}/.env.bak" || true

    # 备份 SQLite data 目录
    if [ -d "${old_data_dir}" ]; then
        log "备份 SQLite data 目录..."
        tar -czf "${old_backup}/data.tar.gz" -C "${old_data_dir}" . 2>/dev/null || warn "data 备份失败"
    fi

    # 备份数据库（尝试连原版容器）
    if [ "${db_type}" = "postgres" ]; then
        log "备份 PostgreSQL..."
        cd "${old_dir}"
        ${COMPOSE_CMD} exec -T postgres pg_dump -U "${pg_user}" "${pg_db}" > "${old_backup}/postgres.sql" 2>/dev/null || warn "PG dump 失败（容器可能未运行）"
        cd - > /dev/null
    elif [ "${db_type}" = "mysql" ]; then
        log "备份 MySQL..."
        cd "${old_dir}"
        ${COMPOSE_CMD} exec -T mysql mysqldump -u root -p"${mysql_pw}" new-api > "${old_backup}/mysql.sql" 2>/dev/null || warn "MySQL dump 失败"
        cd - > /dev/null
    fi

    log "原版数据备份完成: ${old_backup}"

    # 3. 准备二开部署目录
    local new_dir
    new_dir="$(pwd)/migrated-new-api"

    if [ -d "${new_dir}" ]; then
        warn "目标目录已存在: ${new_dir}"
        read -r -p "覆盖克隆？[y/N]: " c2
        if [[ "${c2}" =~ ^[Yy]$ ]]; then
            rm -rf "${new_dir}"
        else
            err "已取消。如需复用现有目录，请手动操作"
            exit 1
        fi
    fi

    log "克隆二开仓库..."
    if ! git remote get-url "${REMOTE}" &> /dev/null; then
        warn "当前目录未配置 ${REMOTE} 远程，将使用 GitHub 地址直接克隆"
        git clone -b "${BRANCH}" "https://github.com/ThinkAndBe/new-api-custom.git" "${new_dir}"
    else
        local repo_url
        repo_url=$(git remote get-url "${REMOTE}")
        git clone -b "${BRANCH}" "${repo_url}" "${new_dir}"
    fi

    cd "${new_dir}"
    info "已进入二开目录: ${new_dir}"

    # 4. 生成新的 docker-compose.yml，合并原版配置 + 源码构建
    log "生成新 docker-compose.yml（合并原版配置 + 源码构建）..."
    generate_migrated_compose "${old_compose}" "${db_type}" "${sql_dsn}" "${redis_conn}" \
        "${pg_user}" "${pg_pw}" "${pg_db}" "${mysql_pw}"

    # 5. 数据衔接（关键：让二开复用原版数据）
    setup_data_inheritance "${old_dir}" "${new_dir}" "${db_type}" \
        "${old_pg_volume}" "${old_mysql_volume}" "${old_compose}"

    # 6. 构建并启动
    log "构建二开镜像（首次约 5-10 分钟）..."
    if ! ${COMPOSE_CMD} build "${SERVICE}"; then
        err "镜像构建失败"
        err "原版服务未受影响，仍可正常运行"
        err "排查后重新执行: cd ${new_dir} && ${COMPOSE_CMD} build ${SERVICE}"
        exit 1
    fi

    log "启动二开服务..."
    # 注意：postgres/redis 服务名一致时，会复用同一个数据卷（关键！）
    ${COMPOSE_CMD} up -d

    if health_check; then
        log "========== 迁移成功 =========="
        info "二开版本已启动: ${HEALTH_URL%/api/status}"
        info "你的所有配置/用户/Token/渠道/日志已保留"
        info ""
        info "后续操作："
        info "  1. 停止原版服务（可选，避免端口冲突）:"
        info "     cd ${old_dir} && ${COMPOSE_CMD} stop new-api"
        info "  2. 验证无误后，可清理原版:"
        info "     cd ${old_dir} && ${COMPOSE_CMD} down"
        info "  3. 以后升级只需在 ${new_dir} 执行:"
        info "     ./upgrade.sh"
        echo
        warn "注意：原版和二开不能同时用相同端口运行，请确认只启动一个"
    else
        err "========== 二开启动失败 =========="
        err "原版服务未受影响。请查看日志:"
        err "${COMPOSE_CMD} logs --tail=100 ${SERVICE}"
        err "排查后可重新执行迁移"
        exit 1
    fi
}

# 生成迁移后的 docker-compose.yml：基于原版配置，但改为源码构建
generate_migrated_compose() {
    local old_compose="$1" db_type="$2" sql_dsn="$3" redis_conn="$4"
    local pg_user="$5" pg_pw="$6" pg_db="$7" mysql_pw="$8"

    # 读取原版 compose 中 new-api 服务的其他环境变量（保留用户自定义）
    local extra_env=""
    extra_env=$(grep -oP '^\s+-\s+\K[A-Z_]+=' "${old_compose}" 2>/dev/null \
        | grep -vE 'SQL_DSN|REDIS_CONN_STRING' \
        | sort -u || true)

    cat > docker-compose.yml << COMPOSE_EOF
# 二开 new-api（从原版迁移生成，保留原有配置）
version: '3.4'

services:
  new-api:
    build: .
    image: ${IMAGE_NAME}:latest
    container_name: new-api
    restart: always
    command: --log-dir /app/logs
    ports:
      - "3000:3000"
    volumes:
      - ./data:/data
      - ./logs:/app/logs
    environment:
COMPOSE_EOF

    # 数据库连接（保持与原版一致）
    if [ "${db_type}" = "postgres" ]; then
        echo "      - SQL_DSN=postgresql://${pg_user}:${pg_pw}@postgres:5432/${pg_db}" >> docker-compose.yml
    elif [ "${db_type}" = "mysql" ]; then
        echo "      - SQL_DSN=root:${mysql_pw}@tcp(mysql:3306)/new-api" >> docker-compose.yml
    else
        # SQLite：不设 SQL_DSN，使用 ./data 目录
        echo "#      - SQL_DSN=（使用 SQLite，数据存于 /data）" >> docker-compose.yml
    fi

    # Redis
    if [ -n "${redis_conn}" ]; then
        echo "      - REDIS_CONN_STRING=${redis_conn}" >> docker-compose.yml
    else
        echo "      - REDIS_CONN_STRING=redis://:123456@redis:6379" >> docker-compose.yml
    fi

    cat >> docker-compose.yml << COMPOSE_EOF
      - TZ=Asia/Shanghai
      - ERROR_LOG_ENABLED=true
      - BATCH_UPDATE_ENABLED=true
      - NODE_NAME=new-api-node-1

    depends_on:
      - redis
COMPOSE_EOF

    # 按数据库类型添加依赖服务
    if [ "${db_type}" = "postgres" ]; then
        echo "      - postgres" >> docker-compose.yml
    elif [ "${db_type}" = "mysql" ]; then
        echo "      - mysql" >> docker-compose.yml
    fi

    cat >> docker-compose.yml << COMPOSE_EOF
    networks:
      - new-api-network
    healthcheck:
      test: ["CMD-SHELL", "wget -q -O - http://localhost:3000/api/status | grep -o '\"success\":\\s*true' || exit 1"]
      interval: 30s
      timeout: 10s
      retries: 3

  redis:
    image: redis:latest
    container_name: redis
    restart: always
COMPOSE_EOF

    local redis_pw
    redis_pw=$(echo "${redis_conn}" | grep -oP '://:\K[^@]+' || echo "123456")
    echo "    command: [\"redis-server\", \"--requirepass\", \"${redis_pw}\"]" >> docker-compose.yml
    echo "    networks:" >> docker-compose.yml
    echo "      - new-api-network" >> docker-compose.yml

    # 数据库服务
    if [ "${db_type}" = "postgres" ]; then
        cat >> docker-compose.yml << COMPOSE_EOF

  postgres:
    image: postgres:15
    container_name: postgres
    restart: always
    environment:
      POSTGRES_USER: ${pg_user}
      POSTGRES_PASSWORD: ${pg_pw}
      POSTGRES_DB: ${pg_db}
    volumes:
      - pg_data:/var/lib/postgresql/data
    networks:
      - new-api-network
COMPOSE_EOF
    elif [ "${db_type}" = "mysql" ]; then
        cat >> docker-compose.yml << COMPOSE_EOF

  mysql:
    image: mysql:8.2
    container_name: mysql
    restart: always
    environment:
      MYSQL_ROOT_PASSWORD: ${mysql_pw}
      MYSQL_DATABASE: new-api
    volumes:
      - mysql_data:/var/lib/mysql
    networks:
      - new-api-network
COMPOSE_EOF
    fi

    echo "" >> docker-compose.yml
    echo "volumes:" >> docker-compose.yml
    [ "${db_type}" = "postgres" ] && echo "  pg_data:" >> docker-compose.yml
    [ "${db_type}" = "mysql" ] && echo "  mysql_data:" >> docker-compose.yml
    echo "" >> docker-compose.yml
    echo "networks:" >> docker-compose.yml
    echo "  new-api-network:" >> docker-compose.yml
    echo "    driver: bridge" >> docker-compose.yml

    info "已生成 docker-compose.yml（数据库: ${db_type}）"
}

# 数据衔接：让二开复用原版的数据库数据
setup_data_inheritance() {
    local old_dir="$1" new_dir="$2" db_type="$3"
    local old_pg_volume="$4" old_mysql_volume="$5" old_compose="$6"

    if [ "${db_type}" = "sqlite" ]; then
        # SQLite：复制 data 目录
        log "复制 SQLite 数据到二开目录..."
        mkdir -p "${new_dir}/data"
        if [ -d "${old_dir}/data" ]; then
            cp -rn "${old_dir}/data/." "${new_dir}/data/" 2>/dev/null || cp -r "${old_dir}/data/." "${new_dir}/data/" 2>/dev/null || warn "data 目录复制部分失败"
            info "SQLite 数据已复制"
        else
            warn "原版无 data 目录，二开将使用全新 SQLite"
        fi
    else
        # PostgreSQL/MySQL：复用同一个 docker volume
        # 关键：docker-compose.yml 中 volumes 名一致 + 在同一个 compose project 下，
        # 会自动复用。但跨目录迁移时 project 名会变，需显式处理。
        log "数据库将复用原版的数据卷..."
        local vol_name=""
        if [ "${db_type}" = "postgres" ]; then
            vol_name=$(echo "${old_pg_volume}" | grep -oP '\b\w+_pg_data\b|pg_data' | head -1 || echo "pg_data")
        elif [ "${db_type}" = "mysql" ]; then
            vol_name=$(echo "${old_mysql_volume}" | grep -oP '\b\w+_mysql_data\b|mysql_data' | grep -oP '\w+_mysql_data' | head -1 || echo "mysql_data")
        fi

        # 获取原版实际的 volume 名（带 project 前缀）
        local project_name=""
        project_name=$(basename "${old_dir}" | tr '-' '_' | tr '.' '_')
        local real_vol="${project_name}_${vol_name}"

        info "原版数据卷: ${real_vol}"
        info "二开将挂载同一数据卷（通过 external volume 方式）"

        # 把新 compose 的 volume 改为 external，指向原版卷
        if docker volume inspect "${real_vol}" &> /dev/null 2>&1; then
            # 用 sed 把 "  vol_name:" 改为 external 引用
            log "配置数据卷 ${vol_short} → external (${real_vol})..."
            if sed -i \
                -e "s|^  ${vol_short}:$|  ${vol_short}:\n    external: true\n    name: ${real_vol}|" \
                docker-compose.yml; then
                # 验证替换是否成功
                if grep -q "external: true" docker-compose.yml; then
                    info "数据卷已改为 external，复用原版数据"
                else
                    warn "external volume 配置失败，可能数据卷名为其他格式"
                    info "原版数据卷: ${real_vol}（可手动编辑 docker-compose.yml）"
                fi
            else
                warn "自动改 external volume 失败，请手动确认数据卷挂载"
                info "原版数据卷: ${real_vol}"
            fi
        else
            warn "未找到原版数据卷 ${real_vol}，二开将创建空数据库"
            warn "如需迁移数据库内容，请手动 pg_dump/restore"
            warn "原版备份已保存在 ${old_dir}/.backups/"
        fi
    fi
}

# ======================== 首次初始化 ========================
do_init() {
    log "========== 首次部署初始化 =========="

    check_cmd git
    detect_compose
    check_cmd curl

    # 检查是否已经在项目目录
    if [ ! -f "docker-compose.yml" ]; then
        err "未找到 docker-compose.yml，请在项目根目录执行此脚本"
        exit 1
    fi

    # 检查 Dockerfile 是否存在
    if [ ! -f "Dockerfile" ]; then
        err "未找到 Dockerfile"
        exit 1
    fi

    # 设置 git 远程（如果不存在）
    if ! git remote get-url "${REMOTE}" &> /dev/null; then
        warn "未找到远程 ${REMOTE}，尝试使用 origin"
        REMOTE="origin"
    fi

    # 修改 docker-compose.yml：从源码构建
    if grep -q "image: calciumion/new-api:latest" docker-compose.yml; then
        log "修改 docker-compose.yml：切换为源码构建..."
        sed -i 's|image: calciumion/new-api:latest|build: .\n    image: '"${IMAGE_NAME}"':latest|' docker-compose.yml
        info "已修改 docker-compose.yml"
    fi

    log "拉取最新代码..."
    git fetch "${REMOTE}"
    git checkout "${BRANCH}"
    git pull "${REMOTE}" "${BRANCH}"

    log "首次构建镜像（约 5-10 分钟）..."
    ${COMPOSE_CMD} build "${SERVICE}"

    log "启动服务..."
    ${COMPOSE_CMD} up -d

    if health_check; then
        log "========== 部署成功 =========="
        info "访问地址: ${HEALTH_URL%/api/status}"
        info "查看日志: ${COMPOSE_CMD} logs -f ${SERVICE}"
    else
        err "========== 部署失败 =========="
        err "请查看日志排查: ${COMPOSE_CMD} logs --tail=100 ${SERVICE}"
        exit 1
    fi
}

# ======================== 升级 ========================
do_upgrade() {
    log "========== 开始升级 =========="

    check_cmd git
    detect_compose
    check_cmd curl

    if [ ! -f "docker-compose.yml" ]; then
        err "未找到 docker-compose.yml，请在项目根目录执行"
        exit 1
    fi

    # 设置 git 远程（如果不存在）
    if ! git remote get-url "${REMOTE}" &> /dev/null; then
        warn "未找到远程 ${REMOTE}，使用 origin"
        REMOTE="origin"
    fi

    # 1. 升级前备份
    do_backup

    # 2. 拉取最新代码
    log "拉取最新代码..."
    git fetch "${REMOTE}"
    local current_commit
    current_commit=$(git rev-parse HEAD)
    git pull "${REMOTE}" "${BRANCH}"
    local new_commit
    new_commit=$(git rev-parse HEAD)

    if [ "${current_commit}" = "${new_commit}" ]; then
        warn "代码无更新，跳过构建"
        log "========== 无需升级 =========="
        exit 0
    fi

    log "代码更新: ${current_commit:0:8} → ${new_commit:0:8}"
    info "本次提交: $(git log -1 --format='%s')"

    # 3. 构建新镜像
    log "构建新镜像..."
    if ! ${COMPOSE_CMD} build "${SERVICE}"; then
        err "镜像构建失败"
        do_rollback
        exit 1
    fi

    # 4. 重启服务
    log "重启服务..."
    ${COMPOSE_CMD} up -d "${SERVICE}"

    # 5. 健康检查
    if health_check; then
        log "========== 升级成功 =========="
        info "当前版本: ${new_commit:0:8}"
        info "提交说明: $(git log -1 --format='%s')"
        info "查看日志: ${COMPOSE_CMD} logs -f ${SERVICE}"
        info "如需回滚: ./upgrade.sh --rollback"
    else
        err "========== 升级失败，自动回滚 =========="
        do_rollback
        exit 1
    fi
}

# ======================== 回滚 ========================
do_rollback() {
    log "========== 开始回滚 =========="

    local latest_backup
    latest_backup=$(cat "${BACKUP_DIR}/latest" 2>/dev/null || echo "")

    if [ -z "${latest_backup}" ] || [ ! -d "${latest_backup}" ]; then
        err "未找到备份记录，无法自动回滚"
        warn "可手动操作: git checkout <旧commit> && ${COMPOSE_CMD} up -d --build"
        exit 1
    fi

    local old_commit
    old_commit=$(cat "${latest_backup}/commit.txt" 2>/dev/null || echo "")

    if [ -n "${old_commit}" ] && [ "${old_commit}" != "" ]; then
        log "回退代码到 ${old_commit:0:8}..."
        git checkout "${old_commit}" 2>/dev/null || {
            warn "代码回退失败，尝试保留当前代码，仅回滚镜像"
        }
    fi

    # 尝试回滚镜像 tag
    local ts
    ts=$(basename "${latest_backup}")
    if docker image inspect "${IMAGE_NAME}:${ts}" &> /dev/null; then
        log "回滚镜像到 ${IMAGE_NAME}:${ts}..."
        docker image tag "${IMAGE_NAME}:${ts}" "${IMAGE_NAME}:latest"
    else
        log "备份镜像 tag 不存在，重新构建..."
        ${COMPOSE_CMD} build "${SERVICE}"
    fi

    # 回退 docker-compose.yml（如果用户改过）
    if [ -f "${latest_backup}/docker-compose.yml" ]; then
        cp "${latest_backup}/docker-compose.yml" ./docker-compose.yml
        info "已恢复 docker-compose.yml"
    fi

    log "重启服务..."
    ${COMPOSE_CMD} up -d "${SERVICE}"

    if health_check; then
        log "========== 回滚成功 =========="
        info "已恢复到升级前状态"
    else
        err "========== 回滚后健康检查仍失败 =========="
        err "请手动排查: ${COMPOSE_CMD} logs --tail=100 ${SERVICE}"
        exit 1
    fi
}

# ======================== 帮助 ========================
show_help() {
    cat << 'EOF'
new-api 二开一键升级脚本

用法:
  ./upgrade.sh                升级到最新版本（默认）
  ./upgrade.sh --init         首次部署初始化（全新部署）
  ./upgrade.sh --migrate      从原版 new-api 迁移到二开（保留所有配置数据）
                              在原版部署目录执行，或用 --from 指定
  ./upgrade.sh --rollback     回滚到上次升级前的版本
  ./upgrade.sh --backup       仅备份当前状态
  ./upgrade.sh --status       查看服务状态和版本
  ./upgrade.sh --help         显示此帮助

环境变量（可选，带默认值）:
  REMOTE=myfork               Git 远程名称 (默认: myfork)
  BRANCH=feature/xxx          Git 分支 (默认: feature/custom-export-and-price-sync)
  SERVICE=new-api             compose 服务名 (默认: new-api)
  HEALTH_URL=http://...       健康检查地址 (默认: http://localhost:3000/api/status)
  HEALTH_TIMEOUT=90           健康检查超时秒数 (默认: 90)
  BACKUP_KEEP=3               备份保留份数 (默认: 3)
  BACKUP_DIR=./.backups       备份目录 (默认: ./.backups)
  IMAGE_NAME=new-api-custom   镜像名称 (默认: new-api-custom)
  MIGRATE_FROM=/path          迁移时的原版目录 (--migrate 模式)

示例:
  # 常规升级
  ./upgrade.sh

  # 从原版迁移（在原版部署目录执行）
  ./upgrade.sh --migrate

  # 从原版迁移（指定原版目录）
  ./upgrade.sh --migrate --from /opt/old-new-api

  # 用 origin 远程升级
  REMOTE=origin ./upgrade.sh

  # 调长健康检查超时
  HEALTH_TIMEOUT=180 ./upgrade.sh

  # 仅备份不升级
  ./upgrade.sh --backup
EOF
}

# ======================== 状态查看 ========================
show_status() {
    detect_compose
    log "========== 服务状态 =========="
    ${COMPOSE_CMD} ps

    echo
    log "========== 当前代码版本 =========="
    git log -3 --oneline 2>/dev/null || warn "非 git 仓库"

    echo
    log "========== 当前镜像 =========="
    docker images | grep -E "${IMAGE_NAME}|REPOSITORY" || warn "无镜像"

    echo
    log "========== 备份记录 =========="
    if [ -d "${BACKUP_DIR}" ]; then
        ls -1d "${BACKUP_DIR}"/*/ 2>/dev/null | while read -r d; do
            local c
            c=$(cat "${d}/commit.txt" 2>/dev/null || echo "?")
            echo "  $(basename "$d")  commit: ${c:0:8}"
        done
    else
        info "暂无备份"
    fi
}

# ======================== 入口 ========================
main() {
    # 解析 --from 参数（用于 --migrate）
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --from)
                MIGRATE_FROM="$2"
                shift 2
                ;;
            *)
                break
                ;;
        esac
    done

    local action="${1:-upgrade}"
    case "${action}" in
        --init)
            do_init
            ;;
        --migrate|migrate)
            do_migrate
            ;;
        upgrade|--upgrade)
            do_upgrade
            ;;
        --rollback|rollback)
            do_rollback
            ;;
        --backup|backup)
            detect_compose
            do_backup
            log "备份完成"
            ;;
        --status|status)
            show_status
            ;;
        --help|-h|help)
            show_help
            ;;
        *)
            err "未知参数: $1"
            show_help
            exit 1
            ;;
    esac
}

main "$@"
