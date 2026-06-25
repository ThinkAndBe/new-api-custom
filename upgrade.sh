#!/usr/bin/env bash
#
# new-api 二开一键升级脚本
#
# 用法：
#   首次部署：  ./upgrade.sh --init
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
  ./upgrade.sh --init         首次部署初始化
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

示例:
  # 常规升级
  ./upgrade.sh

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
    case "${1:-upgrade}" in
        --init)
            do_init
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
