#!/usr/bin/env bash
# apphub-release.sh — 把当前 git 提交打包上传到 AppHub 保存为新版本
# 用法: bash scripts/apphub-release.sh ["版本说明"]
# 依赖: 环境变量 APPHUB_MCP_KEY（密钥），git 仓库当前 HEAD
set -euo pipefail

KEY="${APPHUB_MCP_KEY:?请先 export APPHUB_MCP_KEY=apphub_mcp_xxx}"
URL="https://apphub.erke.com/api/mcp"
PID="04a13723-1fdf-4856-a84a-c2ddbc2eaa9e"   # new-api 项目
EXCLUDES=(
  'config-tool/erke-config-tool.exe'   # 9.8MB 二进制，超出上传限制
  'web/default'                        # 生产不用的主题，压缩文件数到限制内
)
DESC="${1:-$(git log -1 --pretty=format:'%h %s')}"

mcp() { curl -s --max-time 600 -X POST "$URL" \
  -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" -d "$1"; }

extract() { python3 -c "
import sys, json
raw = sys.stdin.read()
for line in raw.splitlines():
    line = line.strip()
    if line.startswith('data:') or (line.startswith('{') and 'result' in line):
        d = json.loads(line[5:] if line.startswith('data:') else line)
        r = d.get('result', d)
        if 'content' in r: print(r['content'][0]['text'])
        else: print(json.dumps(r, ensure_ascii=False)[:400])
        break
"; }

# 1) 打包当前 HEAD（排除大文件）
ARCHIVE_ARGS=()
for e in "${EXCLUDES[@]}"; do ARCHIVE_ARGS+=(":!$e"); done
TMP=$(mktemp -u).zip
git archive HEAD --format=zip -o "$TMP" -- . "${ARCHIVE_ARGS[@]}"

# 2) 上传预览
PAYLOAD=$(python3 - "$TMP" "$PID" << 'PYEOF'
import base64, json, sys
b64 = base64.b64encode(open(sys.argv[1], 'rb').read()).decode()
print(json.dumps({"jsonrpc":"2.0","id":1,"method":"tools/call","params":{
  "name":"preview_code_upload",
  "arguments":{"projectId":sys.argv[2],"zipBase64":b64,"stripRoot":False}}}))
PYEOF
)
PREVIEW=$(mcp "$PAYLOAD" | extract | python3 -c "import sys,json;print(json.load(sys.stdin)['id'])")
echo "预览上传完成: $PREVIEW"

# 3) 计算下一个版本号（当天递增）
LAST=$(mcp "{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"list_versions\",\"arguments\":{\"projectId\":\"$PID\"}}}" | extract | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
    vs = [v['name'] for v in d.get('versions', d if isinstance(d, list) else [])]
    print(vs[0] if vs else '')
except Exception: print('')" 2>/dev/null || true)
TODAY=$(date +%Y%m%d)
N=1
if [ -n "$LAST" ] && echo "$LAST" | grep -q "$TODAY"; then
  N=$(( $(echo "$LAST" | sed "s/.*\.$TODAY\.//" | grep -o '[0-9]*$' || echo 0) + 1 ))
fi
VER="v$(git describe --tags --always 2>/dev/null || git rev-parse --short HEAD).$TODAY.$N"

# 4) 保存版本
mcp "{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"tools/call\",\"params\":{\"name\":\"save_code_version\",\"arguments\":{\"projectId\":\"$PID\",\"previewId\":\"$PREVIEW\",\"name\":\"$VER\",\"description\":\"$DESC\"}}}" | extract
echo "版本: $VER"
rm -f "$TMP"
