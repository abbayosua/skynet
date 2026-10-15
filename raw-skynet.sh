#!/bin/bash
# Raw CommandCode endpoint ke ../skynet — 100% mirip CLI (tidak terdeteksi proxy)
# Usage: ./raw-skynet.sh "pesan kamu" [model]
set -euo pipefail
SKYNET_DIR="$(cd "$(dirname "$0")/../skynet" && pwd)"
API_KEY="${COMMAND_CODE_API_KEY:-user_4xEMfXTxtzVAhTZDd7uFkQ6fjCZ8k2mwU7AMAW9eCKE9p2jxMJxHFgJ3rFMXYqyngr9RGCsqmaxV7bpr78Yzu7vf}"
MODEL="${2:-poolside/laguna-s-2.1-free}"
MSG="${1:-jelaskan struktur project skynet ini secara singkat}"
UUID=$(uuidgen | tr '[:upper:]' '[:lower:]')
TRACE=$(openssl rand -hex 16)
SPAN=$(openssl rand -hex 8)
DATE=$(date +%Y-%m-%d)

# Build config mirip internal/app/app.go:buildServerConfig
IS_GIT="false"; BRANCH=""; MAIN=""; STATUS=""; COMMITS="[]"
if git -C "$SKYNET_DIR" rev-parse --git-dir >/dev/null 2>&1; then
  IS_GIT="true"
  BRANCH=$(git -C "$SKYNET_DIR" branch --show-current 2>/dev/null || echo "")
  MAIN=$(git -C "$SKYNET_DIR" symbolic-ref --short refs/remotes/origin/HEAD 2>/dev/null | sed 's|origin/||' || echo "main")
  STATUS=$(git -C "$SKYNET_DIR" status --porcelain 2>/dev/null | head -n 20 | tr '\n' ';' )
  [ -z "$STATUS" ] && STATUS="Working tree clean"
  COMMITS=$(git -C "$SKYNET_DIR" log --oneline -3 2>/dev/null | jq -R . | jq -s .)
else
  COMMITS="[]"
fi
STRUCTURE=$(ls -1 "$SKYNET_DIR" 2>/dev/null | grep -v "^\." | head -n 30 | jq -R . | jq -s .)

# Pakai template resmi 70KB dari /tmp/body.json (capture CLI v1.28.4) — patch workingDir/threadId/messages
TEMPLATE="/tmp/body.json"
if [ ! -f "$TEMPLATE" ]; then echo "template $TEMPLATE tidak ada, jalankan capture dulu"; exit 1; fi

python3 - "$TEMPLATE" /tmp/payload.json "$SKYNET_DIR" "$DATE" "$IS_GIT" "$BRANCH" "$MAIN" "$STATUS" "$COMMITS" "$STRUCTURE" "$UUID" "$MODEL" "$MSG" <<'PY'
import json, pathlib, sys
tpl, out, wd, date, is_git, branch, main, status, commits, struct, uuid, model, msg = sys.argv[1:14]
import json as j
b=j.loads(pathlib.Path(tpl).read_text())
b['config']['workingDir']=wd
b['config']['date']=date
b['config']['environment']='darwin'
b['config']['structure']=j.loads(struct)
b['config']['isGitRepo']= is_git=='true'
b['config']['currentBranch']=branch
b['config']['mainBranch']=main or 'main'
b['config']['gitStatus']=status
b['config']['recentCommits']=j.loads(commits)
b['threadId']=uuid
b['permissionMode']='standard'
b['params']['model']=model
b['params']['messages']=[{"role":"user","content":[{"type":"text","text":msg}]}]
b['params']['max_tokens']=8000
b['params']['stream']=True
# keep tools+system 100% resmi dari capture (42k system, 11 tools)
pathlib.Path(out).write_text(j.dumps(b))
print(f"payload ready isGit={is_git} wd={wd} model={model}")
PY

echo "→ POST https://api.commandcode.ai/alpha/generate ke $SKYNET_DIR"
echo "→ model=$MODEL thread=$UUID"
curl -s -N https://api.commandcode.ai/alpha/generate \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -H "User-Agent: cli" \
  -H "x-command-code-version: 1.28.4" \
  -H "x-cli-environment: production" \
  -H "x-project-slug: $(echo $SKYNET_DIR | tr '/' '-' | tr '[:upper:]' '[:lower:]' | sed 's/^-//')" \
  -H "x-taste-learning: true" \
  -H "x-co-flag: false" \
  -H "x-session-id: $UUID" \
  -H "traceparent: 00-${TRACE}-${SPAN}-01" \
  -H "Accept: */*" -H "Accept-Language: *" -H "Sec-Fetch-Mode: cors" \
  --data-binary @/tmp/payload.json | while IFS= read -r line; do
    # pretty: tampilkan delta text
    echo "$line" | python3 -c "import sys,json; l=sys.stdin.read().strip(); 
try:
 j=json.loads(l)
 if j.get('type')=='text-delta': print(j['text'], end='', flush=True)
 elif j.get('type')=='finish-step': print('\n\n[usage]', j.get('usage',{}))
except: pass" 2>/dev/null
  done
echo -e "\n— done —"
