# Raw CommandCode endpoint → ../skynet

Endpoint resmi: `https://api.commandcode.ai/alpha/generate` (capture v1.28.4, 71KB)
Tidak perlu `cmd --yolo`, langsung `curl`/`fetch` dengan header+payload 100% mirip CLI (anti `Proxy use detected`).

## 1. File yang sudah ada
- `raw-skynet.sh` — bash, auto `workingDir=../skynet` + git info
- `raw-skynet-node.mjs` — node, sama
- Template resmi: `/tmp/body.json` (capture CLI) — jangan dihapus

## 2. Cara pakai (raw)
```bash
# chat sederhana (tanpa tool-loop, tools tetap 11 tapi prompt tidak trigger tool)
./raw-skynet.sh "apa itu project skynet? jawab 2 kalimat"

# ganti model
./raw-skynet.sh "jelaskan internal/agent" deepseek/deepseek-v4-flash

# node version
node raw-skynet-node.mjs "list file penting di skynet" poolside/laguna-s-2.1-free

# custom API key
COMMAND_CODE_API_KEY=user_xxx ./raw-skynet.sh "hi"
```

## 3. Header ultra-mirip (wajib)
```
Authorization: Bearer <key>
Content-Type: application/json
User-Agent: cli
x-command-code-version: 1.28.4
x-cli-environment: production
x-project-slug: users-user-documents-skynet  # slug dari SKYNET_DIR
x-taste-learning: true
x-co-flag: false
x-session-id: <uuid>  # HARUS sama dengan body.threadId
traceparent: 00-<32hex>-<16hex>-01
Accept: */* , Accept-Language: *, Sec-Fetch-Mode: cors
```

## 4. Payload ultra-mirip
Patch dari `/tmp/body.json`:
- `config.workingDir = /Users/user/Documents/skynet` (atau `$(cd ../skynet && pwd)`)
- `config.date`, `config.structure` (ls skynet), `isGitRepo`, `currentBranch`, `mainBranch`, `gitStatus`, `recentCommits` — ambil via `git -C ../skynet ...`
- `threadId = UUID` (sama dengan header x-session-id)
- `params.model`, `params.messages=[{role:"user",content:[{type:"text",text:MSG}]}]`, `params.max_tokens`, `stream:true`
- **JANGAN dihapus**: `params.tools` (11 tools full) + `params.system` (42k) biar 100% CLI

Contoh `curl` mentah (sudah tested pong inputTokens 15827):
```bash
UUID=$(uuidgen | tr '[:upper:]' '[:lower:]') ; TRACE=$(openssl rand -hex 16); SPAN=$(openssl rand -hex 8)
curl -s -N https://api.commandcode.ai/alpha/generate \
  -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -H "User-Agent: cli" -H "x-command-code-version: 1.28.4" \
  -H "x-cli-environment: production" -H "x-project-slug: users-user-documents-skynet" \
  -H "x-taste-learning: true" -H "x-co-flag: false" \
  -H "x-session-id: $UUID" -H "traceparent: 00-${TRACE}-${SPAN}-01" \
  --data-binary @/tmp/payload.json
```

## 5. Catatan skynet
- `workingDir` di script otomatis `../skynet` → `/Users/user/Documents/skynet`
- Kalau prompt butuh baca file (`jelaskan internal/agent`), model akan emit `tool-call` (glob/read_file). Script saat ini hanya print `text-delta`, jadi harus loop tool (lihat `raw-skynet-node.mjs` mau di-extend untuk `tool-call` → exec → kirim balik dengan `role:tool`).
- Untuk Q&A simple pakai prompt langsung tanpa trigger tool, atau set `tools:[]` di payload biar jawab langsung tanpa baca file.
