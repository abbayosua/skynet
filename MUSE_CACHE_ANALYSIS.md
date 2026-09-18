# Analisis Cache & Issue: `opencode-go/muse-spark-1.3-contributor`

Status: **SELESAI — akar masalah terkonfirmasi (13 langkah investigasi, bukti empiris dari wire).**
Model target: `muse-spark-1.3-contributor` @ provider `opencode-go` ("muse 1.3 contributor").

**Update: kedua masalah sudah diperbaiki (v0.1.33).**

| Masalah | Perbaikan | Verifikasi |
|---|---|---|
| P1 `prompt_cache_key` hilang di `/responses` | Transport `opencodeCacheTransport` (`internal/agent/coordinator.go`) menyuntik field itu ke body `/responses` opencode, memakai `stableOpenCodeSessionID(dataDir)` — sama seperti CLI resmi. | Capture server lokal: `pck ses_tARxlfiYbloH11fGx8S1GBtoJV` ada di **semua** request `/responses`. 5 unit test baru lulus. |
| P2 5xx pada model small | `minimax-m2.7` **permanen 500 dari upstream** (dikonfirmasi via curl, dengan/tanpa header). Model lain (`minimax-m3`, `deepseek-v4-flash`, `glm-5.2`, `longcat-2.0`) semuanya 200. `models.small` diganti ke `minimax-m3`. | curl langsung ke gateway. |
| **P3 hit-rate pasti >95%** | **Akar utama kehilangan cache selama sesi coding.** `isDeepSeekCachePrompt` menerima nama provider *fantasy* (`"openai-compat"`), bukan ID provider (`"opencode-go"`), sehingga cabang "stable prefix" **tidak pernah aktif** — system prompt memuat `git status --short` + 3 commit terakhir + tanggal asli, yang berubah setiap kali file disentuh. Divergensi terukur di byte 18 141 dari 125 042 (14,5%). Sekarang `prompt.Build` dipanggil dengan `ModelCfg.Provider`, jadi cabang itu aktif: tanggal dipin `1/1/2006`, `GitStatus` hanya branch. | 4 turn berturut-turut dengan file diubah di antara setiap turn: **99,1–99,8%** hit, semua 200. Sebelum fix: 1 kali edit → drop ke **86,8%**. |

---

## 9. Ringkasan lever cache (terukur)

| Lever | Efek | Status |
|---|---|---|
| `x-opencode-session` stabil | **menentukan** worker + namespace cache. Uji: id sama → `cached=2417/2431` (99,4%); id acak → `cached=0`. | sudah persist di `<dataDir>/opencode_session_id` |
| Snapshot git/tanggal dibekukan saat provider cache | 1 edit file = invalidasi prefix. Sisipan 1 file tak-tertrack menggeser daftar terurut → divergence di byte 18 141. | **diperbaiki** |
| `prompt_cache_key` di `/responses` | parity dengan CLI resmi; di gateway ini cache ternyata di-key oleh konten prefix, jadi bukan penggerak utama hit. | diperbaiki |
| Routing muse ke `/responses` | `/chat/completions` untuk muse = 84× HTTP 500. | sudah dipatok di `opencodeNeedsResponsesAPI` |
| Model small sehat | `minimax-m2.7` 500 permanen → `shouldAutoRetry` mengulang 30×. | config diganti |

**Yang masih bisa menurunkan hit-rate (di luar kendali):** panggilan pertama di worker dingin, cache upstream kedaluwarsa setelah idle lama, serta perubahan `AGENTS.md`/skills/daftar tool/compaction yang memang mengubah prefix.

---

## 1. Ringkasan (TL;DR)

| Pertanyaan | Jawaban |
|---|---|
| Musenya lewat endpoint apa? | **`POST https://opencode.ai/zen/go/v1/responses`** (Responses API), bukan chat-completions. |
| Apakah penyebabnya "tidak ada header `x-opencode-go`"? | **BUKAN.** Header itu tidak pernah ada. Yang ada `x-opencode-session`, dan itu **sudah terkirim** & stabil. |
| Akar masalah cache miss? | **`prompt_cache_key` HILANG di body `/responses`.** Field ini di-set oleh Skynet lewat `extra_body`, tetapi fantasy hanya menerapkannya di jalur **chat-completions**; jalur **Responses API menjatuhkannya**. |
| Apakah cache 0%? | Tidak. Prefix cache implicit tetap jalan (terukur **`cached_tokens = 24 945`** pada turn ke-2) — tetapi tanpa `prompt_cache_key` hit-rate tidak optimal/tidak deterministik (CLI resmi mengirimnya). |
| "Sering ada issue"-nya apa? | **HTTP 500 `Internal server error` dari gateway opencode.ai untuk model small `minimax-m2.7` via `/chat/completions`**, berulang tiap turn. Bukan tool-call malformed, bukan cache miss. |

Ada **dua** masalah berbeda, keduanya sudah dipisahkan dengan bukti:

- **P1 (cache):** `prompt_cache_key` tidak sampai ke `/responses`.
- **P2 (issue):** gateway mengembalikan 5xx pada jalur chat-completions untuk model small.

---

## 2. Target & konfigurasi efektif

| Field | Nilai | Sumber |
|---|---|---|
| Provider ID | `opencode-go` | `~/.local/share/skynet/providers.json` |
| Type | `openai-compat` | definisi builtin (`catwalk` `configs/opencode-go.json`); override user tidak set `type` → `internal/config/load.go:253` memakai `p.Type` |
| BaseURL | `https://opencode.ai/zen/go/v1` | `api_endpoint` builtin |
| API key | `<redacted>` (override user, data-dir) | `~/.local/share/skynet/skynet.json` |
| Model | `muse-spark-1.3-contributor` (`can_reason: true`, ctx `1 048 576`) | providers.json / `skynet models` |
| `x-opencode-session` | `ses_<26 base62>` (stabil, disimpan di `<dataDir>/opencode_session_id`) | `internal/agent/coordinator.go:1294-1343` |

Bukti `x-opencode-session` persist: `<cwd>/.skynet/opencode_session_id` = `ses_tARxlfiYbloH11fGx8S1GBtoJV` (30 byte).

---

## 3. Metode investigasi

Semua bukti diambil dari **request outbound nyata**, bukan tebakan:

1. **Instrumentasi sementara** di `internal/agent/coordinator.go` (cabang `strings.HasPrefix(providerID, "opencode")`) berupa `http.RoundTripper` yang mencatat method, path, header (`Authorization` di-redact), dan probe body (`prompt_cache_key`, `store`, `previous_response_id`, top-level keys, roles, hash system prompt & tools). Instrumentasi **selalu direvert** setelah selesai.
2. **Capture CLI resmi**: `opencode` v1.18.29 diarahkan ke server capture lokal via `OPENCODE_CONFIG_CONTENT` (`npm: @ai-sdk/openai`, `baseURL` → `127.0.0.1`). Ini faithful karena catalog opencode memakai override per-model `"provider": {"npm": "@ai-sdk/openai"}` untuk muse (terbukti di `~/.cache/opencode/models.json`).
3. Perintah uji: `skynet run -m opencode-go/muse-spark-1.3-contributor "…" -d` (dan `--continue` untuk turn ke-2).

---

## 4. Bukti

### 4.1 Jalur routing terkonfirmasi (`/responses`)

`internal/agent/coordinator.go:1130-1137` → `openaicompat.WithUseResponsesAPI()` +
`WithResponsesAPIFunc(opencodeNeedsResponsesAPI)`; `:1160-1169` mengembalikan `true` untuk model yang mengandung `muse`.
Fantasy: `openai.go:191` → `isResponsesModel` (`:214-219`, memakai func custom) → `newResponsesLanguageModel(...)` → paket `openai-go/responses` → path `/responses`.

Log outbound nyata:

```
POST /zen/go/v1/responses
  X-Opencode-Session: ses_tARxlfiYbloH11fGx8S1GBtoJV
  Authorization: <redacted>
  User-Agent: SkyNet/v0.1.32
  X-Stainless-*: (dari openai-go SDK)
```

### 4.2 `prompt_cache_key` HILANG di `/responses` (Skynet)

```
POST /zen/go/v1/responses   body_bytes=101829
  top_level_keys: [input max_output_tokens model store stream tool_choice tools]
  model: muse-spark-1.3-contributor
  prompt_cache_key: <nil>      <-- HILANG
  store: false
  previous_response_id: <nil>
  input_len: 3   roles: [system user user]
  tools_count: 74
```

### 4.3 CLI resmi MENGIRIM `prompt_cache_key` di endpoint & model yang sama

```
POST /v1/responses   body_bytes=89089
  headers: x-opencode-client: cli
           x-opencode-project: 4e57a1827d875ed52d9d89d8eac281af401a2bb3
           x-opencode-request: msg_0a5f8f128001uCqM73bOMrzuIs
           x-opencode-session: ses_f5a070efbffeZQVukVR6J8wDMG
  top_level_keys: [include input max_output_tokens model prompt_cache_key store stream tool_choice tools]
  prompt_cache_key: ses_f5a070efbffeZQVukVR6J8wDMG   <-- ADA, = nilai x-opencode-session
  store: false
```

### 4.4 Mekanisme: mengapa field itu jatuh

1. Skynet menyetel `prompt_cache_key` ke `mergedOptions["extra_body"]` → `openaicompat.ParseOptions` → `*openaicompat.ProviderOptions.ExtraBody` — **opsi per-call** (`coordinator.go:706-712`).
2. fantasy hanya menerapkan `ExtraBody` di `openaicompat.PrepareCallFunc`
   (`providers/openaicompat/language_model_hooks.go:50-51` → `params.SetExtraFields(...)`).
3. `PrepareCallFunc` didaftarkan sebagai **`openai.WithLanguageModelOptions(...)`** (`openaicompat.go:28-33`).
4. `openai.go:191-196`: bila `useResponsesAPI && isResponsesModel` → `newResponsesLanguageModel(modelID, name, client, objectMode)` dibangun **tanpa** `languageModelOptions` → `PrepareCallFunc` **tidak pernah dipanggil** → `ExtraBody` dibuang diam-diam.
5. Tambahan: model responses membaca opsi per-call sebagai `*openai.ResponsesProviderOptions` (`responses_language_model.go:154-156`), bukan `*openaicompat.ProviderOptions` → mismatch tipe.

**Konfirmasi silang (dua sisi independen):**

- Harness unit (mock server): memaksa `WithSDKOptions(WithJSONSet("prompt_cache_key", …))` level **client** → field **muncul** di body `/responses`:
  `{"store":false,"input":[…],"model":"muse-spark-1.3-contributor","prompt_cache_key":"sess-xyz"}`
- Jalur **chat-completions** (dipaksa) → `prompt_cache_key_present = True`; jalur **responses** → `False`. Jadi hanya jalur chat yang menerapkannya.

### 4.5 Bukan ketidakstabilan prefix

Dua turn (dua proses terpisah), body `/responses` ditumpuk & dibandingkan:

```
turn1 main       size=101850  in=3  sys_len=30890  sys_sha=bc4a455cdc69b47e  tools_sha=5166ece2c8aa692d
turn2 main       size=102742  in=8  sys_len=30890  sys_sha=bc4a455cdc69b47e  tools_sha=5166ece2c8aa692d
longest common prefix: 31712 byte (31.1%)
  ctx A: …"role":"user"}],"model":"muse-spark-1.3-contributor","tool_choice":"auto",…
  ctx B: …"role":"user"},{"content":"Ok","role":"assistant"},…
```

- System prompt & tools **byte-identik lintas turn dan lintas proses** (sha sama) → tidak ada timestamp/snapshot dinamis, urutan tool tidak berubah.
- Divergensi pertama **tepat di ujung `input` turn-1** = percakapan bertambah panjang (normal).
- Field non-input identik: `model`, `max_output_tokens=131072`, `store=false`, `stream=true`, `tool_choice=auto`.

### 4.6 Cache sebenarnya masih jalan (parsial)

```
/responses  status=200  pck=False  cached_tokens=113     (turn1)
/responses  status=200  pck=False  cached_tokens=24945   (turn2)
```

Implikasi: gateway melakukan prefix caching implicit; hilangnya `prompt_cache_key` menurunkan/menidak-stabilkan hit-rate, bukan mematikan cache.

### 4.7 Issue sebenarnya: 5xx upstream pada chat-completions

```
count=3  path=/zen/go/v1/chat/completions  model=minimax-m2.7             status=500  err="Internal server error"
count=3  path=/zen/go/v1/responses         model=muse-spark-1.3-contributor status=200  err=-
ZZ-REPAIR (tool-call repair) total = 0
```

Body error upstream (75 byte):

```json
{"type":"error","error":{"type":"error","message":"Internal server error"}}
```

- Diretry oleh `shouldAutoRetry` (`coordinator.go:337-360`, mencakup 5xx/429/408/409).
- Terjadi pada **setiap** turn, termasuk turn yang sukses → sumber utama keluhan "sering ada issue".
- **Tool-call malformed tidak ter-reproduksi** untuk muse 1.3: `patchToolSchemas` (`toolpatch.go:9-18`, hint muse aktif) & `repairOpencodeInput` **0 kali** terpicu; `read go.mod` berhasil.
- Catatan: memaksa muse ke chat-completions (`ZZ_FORCE_CHAT=1`) menghasilkan **84× HTTP 500** dan run macet di retry → **routing muse ke `/responses` sudah benar** dan tidak boleh diubah.

---

## 5. Yang BUKAN penyebab

| Kandidat | Status | Bukti |
|---|---|---|
| Header `x-opencode-go` tidak ada | ❌ tidak relevan | Header tersebut tidak pernah ada; yang dipakai `x-opencode-session` dan sudah terkirim. |
| `x-opencode-session` hilang/berubah-ubah | ❌ | Terkirim di semua request, format `^ses_[0-9A-Za-z]{26}$`, nilai identik lintas turn & lintas proses. |
| Prefix tidak stabil (timestamp/snapshot/urutan tool) | ❌ | sha system prompt & tools identik; divergence hanya saat percakapan bertambah. |
| Routing model salah | ❌ | Routing ke `/responses` benar; chat-completions justru 500. |
| Cache mati total | ❌ | `cached_tokens = 24 945` pada turn ke-2. |
| Tool-call malformed | ❌ (tidak ter-reproduksi pada muse 1.3) | 0 repair. |

---

## 6. Rekomendasi perbaikan

### P1 — bawa `prompt_cache_key` ke `/responses`

Opsi paling kecil risikonya (sudah terbukti jalan di harness): kirim lewat jalur **client-level**, bukan per-call.

- Di `internal/agent/coordinator.go`, saat membangun provider opencode (`buildOpenaiCompatProvider`, ~`:1130`), tambahkan untuk model responses:
  `openaicompat.WithSDKOptions(openaisdk.WithJSONSet("prompt_cache_key", <sesi>))`
  — pola ini sudah dipakai untuk `providerCfg.ExtraBody` (`:1155-1157`) dan **terbukti** muncul di body `/responses`.
- Kendala: provider dibangun sekali per provider, sedangkan `prompt_cache_key` adalah **per-sesi**. Pilihan:
  a. Naikkan dukungan di fantasy (ajukan patch upstream): terapkan `ExtraBody` juga di jalur Responses (`newResponsesLanguageModel` / `ResponsesProviderOptions`).
  b. Ubah tipe opsi per-call menjadi `*openai.ResponsesProviderOptions` untuk model responses (agar `responses_language_model.go:154-156` membacanya).
  c. Tambal di Skynet: bungkus request dengan `RoundTripper` yang menyuntik `prompt_cache_key` ke body JSON pada `/responses`.
- Nilai yang dipakai: samakan dengan konvensi CLI resmi → `prompt_cache_key == x-opencode-session` (yakni `stableOpenCodeSessionID(dataDir)`).

### P2 — 5xx `minimax-m2.7` pada chat-completions

- Klasifikasikan sebagai **upstream unavailable** (bukan bug Skynet), tetapi perbaiki UX/efisiensi:
  tambah backoff + batas retry eksplisit untuk 5xx pada model small agar tidak memicu retry storm.
- Pertimbangkan fallback: bila model small mengembalikan 5xx, lewati title/summarize alih-alih mengulang.
- Bila gateway juga 500 untuk model small lain, set `models.small` yang sehat di config provider.

### P3 — parity dengan CLI resmi (opsional)

Kirim `x-opencode-client`, `x-opencode-project`, `x-opencode-request` seperti CLI, untuk observability/routing yang sama. Juga pertimbangkan `include`/`reasoning` pada `/responses` (keduanya hilang oleh mekanisme P1 yang sama).

---

## 7. Cara reproduksi cepat

```bash
# 1) Turn muse normal (lihat header & body outbound)
skynet run -m opencode-go/muse-spark-1.3-contributor "reply pong" -d
grep "ZZ-PROBE\|ZZ-CACHE" .skynet/logs/skynet.log | tail

# 2) Turn kedua (verifikasi cached_tokens dan stabilitas prefix)
skynet run --continue -m opencode-go/muse-spark-1.3-contributor "lanjut" -d
grep "cached_tokens" .skynet/logs/skynet.log | tail

# 3) Bandingkan dengan CLI resmi (capture server lokal + OPENCODE_CONFIG_CONTENT)
#    lihat langkah 8 pada log investigasi
```

---

## 8. Catatan

- Semua instrumentasi bersifat **sementara** dan sudah **direvert**; tidak ada perubahan permanen akibat investigasi ini.
- Tidak ada kredensial yang ditulis ke dokumen ini (API key/`Authorization` selalu `<redacted>`).
