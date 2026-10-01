#!/usr/bin/env bash
# apphub-release.sh — 把当前 git 提交打包上传到 AppHub 保存为新版本
# 用法: bash scripts/apphub-release.sh ["版本说明"]
# 依赖: export APPHUB_MCP_KEY=apphub_mcp_xxx
#
# ⚠️ 编码铁律：所有含中文的 JSON 一律由内嵌 python 生成（json.dumps 默认
# ensure_ascii=True → 纯 ASCII，\uXXXX 转义）写进临时文件，再 curl
# --data-binary @file 发送。绝不直接 curl -d "中文"——Windows Git Bash 会把
# 命令行中文按 GBK 编码发出，服务端按 UTF-8 解就是乱码（2026-10-01 踩过：
# 版本说明乱码，而同批 ZIP 代码完好，因为 ZIP payload 走的就是 ASCII 通道）。
set -euo pipefail

KEY="${APPHUB_MCP_KEY:?请先 export APPHUB_MCP_KEY=apphub_mcp_xxx}"
URL="https://apphub.erke.com/api/mcp"
PID="04a13723-1fdf-4856-a84a-c2ddbc2eaa9e"   # AppHub 上的 new-api 项目
EXCLUDES=(
  'config-tool/erke-config-tool.exe'   # 9.8MB 二进制，超出上传限制
  'web/default'                        # 生产不用的主题，压缩文件数到限制内
)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

# send <请求文件>：调 MCP，打印工具返回的 JSON 文本（单行）
send() {
  curl -s --max-time 600 -X POST "$URL" \
    -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
    -H "Accept: application/json, text/event-stream" --data-binary @"$1" | python3 -c "
import sys, json
raw = sys.stdin.read()
for line in raw.splitlines():
    line = line.strip()
    if line.startswith('data:') or (line.startswith('{') and 'result' in line):
        d = json.loads(line[5:] if line.startswith('data:') else line)
        r = d.get('result', d)
        print(r['content'][0]['text'] if isinstance(r, dict) and 'content' in r
              else json.dumps(r, ensure_ascii=False)[:400])
        break
"
}

# 1) 打包当前 HEAD（排除大文件/多余主题）
ARCHIVE_ARGS=()
for e in "${EXCLUDES[@]}"; do ARCHIVE_ARGS+=(":!$e"); done
git archive HEAD --format=zip -o "$WORK/src.zip" -- . "${ARCHIVE_ARGS[@]}"

# 2) 生成请求与说明（全部经 python，ASCII 安全；说明取参数，缺省取 HEAD 提交标题）
AH_PID="$PID" AH_ZIP="$WORK/src.zip" AH_DESC="${1:-}" \
AH_META="$WORK/meta.json" AH_REQ="$WORK/req.json" python3 << 'PYEOF'
import base64, json, os, subprocess
pid = os.environ['AH_PID']
desc = os.environ.get('AH_DESC', '').strip()
if not desc:
    desc = subprocess.run(['git', 'log', '-1', '--pretty=format:%h %s'],
                          capture_output=True).stdout.decode('utf-8', 'replace').strip()
b64 = base64.b64encode(open(os.environ['AH_ZIP'], 'rb').read()).decode()
json.dump({'desc': desc}, open(os.environ['AH_META'], 'w', encoding='ascii'))
json.dump({'jsonrpc': '2.0', 'id': 1, 'method': 'tools/call', 'params': {
    'name': 'preview_code_upload',
    'arguments': {'projectId': pid, 'zipBase64': b64, 'stripRoot': False}}},
    open(os.environ['AH_REQ'], 'w', encoding='ascii'))
PYEOF
PREVIEW=$(send "$WORK/req.json" | python3 -c "import sys,json;print(json.load(sys.stdin)['id'])")
echo "预览上传完成: $PREVIEW"

# 3) 版本号推算：git描述.日期.当日序号（从平台现有版本递增）
AH_PID="$PID" python3 > "$WORK/list.json" << 'PYEOF'
import json, os
print(json.dumps({'jsonrpc': '2.0', 'id': 2, 'method': 'tools/call', 'params': {
    'name': 'list_versions', 'arguments': {'projectId': os.environ['AH_PID']}}}))
PYEOF
VER=$(send "$WORK/list.json" | python3 -c "
import sys, json, subprocess, datetime
names = [v['name'] for v in json.load(sys.stdin).get('tags', [])]
today = datetime.date.today().strftime('%Y%m%d')
n = 1
for nm in names:
    if today in nm:
        try: n = max(n, int(nm.rsplit('.', 1)[1]) + 1)
        except Exception: pass
g = subprocess.run(['git','describe','--tags','--always'], capture_output=True).stdout.decode().strip() \
    or subprocess.run(['git','rev-parse','--short','HEAD'], capture_output=True).stdout.decode().strip()
print(f'v{g}.{today}.{n}')")

# 4) 保存版本（说明来自 meta.json，本就是 ASCII 安全内容）
AH_PID="$PID" AH_PREVIEW="$PREVIEW" AH_VER="$VER" \
AH_META="$WORK/meta.json" AH_OUT="$WORK/save.json" python3 << 'PYEOF'
import json, os
desc = json.load(open(os.environ['AH_META'], encoding='ascii'))['desc']
req = {'jsonrpc': '2.0', 'id': 3, 'method': 'tools/call', 'params': {
    'name': 'save_code_version', 'arguments': {
        'projectId': os.environ['AH_PID'], 'previewId': os.environ['AH_PREVIEW'],
        'name': os.environ['AH_VER'], 'description': desc}}}
open(os.environ['AH_OUT'], 'w', encoding='ascii').write(json.dumps(req))
PYEOF
send "$WORK/save.json"
echo "版本已保存: $VER"
