#!/bin/zsh
# ERKE AI 配置工具 (macOS)
# 用法（在使用教程页复制这条命令，粘贴到「终端」里回车）：
#   zsh <(curl -fsSL https://tokenhub.erke.com/api/usage/config_tool_mac)
# 流程与 Windows 配置工具一致：输入 6 位配置码 → 选择客户端 → 自动写入
# ~/.workbuddy/models.json 或 ~/.codebuddy/models.json

SERVER="https://tokenhub.erke.com"

echo "================================"
echo "  ERKE AI 配置工具 (macOS)"
echo "================================"

# 1. 配置码
printf "配置码（在教程页点「生成配置码」，6 位，5 分钟内有效）: "
read CODE
CODE=$(printf '%s' "$CODE" | tr '[:lower:]' '[:upper:]' | tr -d '[:space:]')
if [ ${#CODE} -ne 6 ]; then
  echo "[错误] 配置码应为 6 位（当前 ${#CODE} 位）"
  exit 1
fi

# 2. 客户端选择
printf "配置到：[1] WorkBuddy   [2] CodeBuddy （回车默认 1）: "
read PROD
case "$PROD" in
  2) DIR="$HOME/.codebuddy"; NAME="CodeBuddy";  PRODUCT="codebuddy" ;;
  *) DIR="$HOME/.workbuddy"; NAME="WorkBuddy";  PRODUCT="workbuddy" ;;
esac

mkdir -p "$DIR"

# 3. 拉取配置（服务端 raw=1 直接返回 models.json 文件体，免去本地 JSON 解析）
TMP=$(mktemp) || exit 1
URL="$SERVER/api/usage/guide_redeem?code=$CODE&product=$PRODUCT&raw=1"
if ! curl -fsSL "$URL" -o "$TMP"; then
  echo "[错误] 配置码无效或已过期（配置码为一次性、5 分钟内有效），请回教程页重新生成后再试"
  rm -f "$TMP"
  exit 1
fi

# 4. 基本校验后落盘
if ! head -c 1 "$TMP" | grep -q '{' || ! grep -q '"models"' "$TMP"; then
  echo "[错误] 服务端返回内容异常："
  head -c 300 "$TMP"; echo
  rm -f "$TMP"
  exit 1
fi
mv "$TMP" "$DIR/models.json"

N=$(grep -o '"id"' "$DIR/models.json" | wc -l | tr -d ' ')
echo "--------------------------------"
echo "[成功] 已写入 $DIR/models.json（$N 个模型）"
echo "        服务器：$SERVER"
echo "        重启 $NAME 后生效"
