#!/bin/bash
# 一键部署脚本 —— 拉代码 → 构建新镜像 → 重建容器 → 清理旧镜像 → 验证
#
# 用法（生产服务器）：
#   bash deploy.sh          # 首次使用前先 git pull 一次拿到本脚本
#
# 说明：
# - 构建阶段不影响运行中的服务，只有第 3 步重建容器会中断数秒
# - 只清理本项目构建产生的悬空镜像，不动其他镜像和数据卷
# - 任何一步失败立即停止（set -e），不会出现「代码更新了容器还是旧的」的半更新状态

set -e
cd "$(dirname "$0")"

echo "==> [1/6] 拉取最新代码"
git pull

echo "==> [2/6] 构建新镜像（后台构建，不影响线上服务）"
docker compose build --pull

echo "==> [3/6] 重建容器（唯一短暂中断的步骤，数秒）"
docker compose up -d

echo "==> [4/6] 清理悬空旧镜像"
docker image prune -f

echo "==> [5/6] 等待启动并验证"
sleep 8
docker compose ps
CID=$(docker compose ps -q | head -1)
if [ -n "$CID" ] && docker logs --tail 300 "$CID" 2>&1 | grep -qE "syncing options|任务进度轮询|Server"; then
  echo "✔ 容器日志出现启动标记"
else
  echo "⚠ 未识别到启动标记，请人工检查：docker logs --tail 100 $CID"
fi
OK=""
for url in https://127.0.0.1:3000/api/status http://127.0.0.1:3000/api/status; do
  if curl -sk -m 5 -o /dev/null "$url"; then OK="$url"; break; fi
done
if [ -n "$OK" ]; then
  echo "✔ 服务响应正常: $OK"
else
  echo "⚠ 本机 3000 未响应，请确认端口映射：docker compose ps"
fi

echo "==> [6/6] 部署完成，剩余镜像："
docker images | head -6
