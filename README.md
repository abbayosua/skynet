# Skynet

Teman coding baru kamu, kini tersedia di terminal favoritmu.
Tools, kode, dan workflow kamu, terhubung dengan LLM pilihanmu.

## Fitur

- **Multi-Model:** pilih dari berbagai LLM atau tambahkan sendiri via API yang kompatibel dengan OpenAI atau Anthropic
- **Fleksibel:** ganti LLM di tengah sesi tanpa kehilangan konteks
- **Berbasis Sesi:** kelola banyak sesi kerja dan konteks per proyek
- **LSP-Enhanced:** Skynet menggunakan LSP untuk konteks tambahan, sama seperti kamu
- **Ekstensibel:** tambah kemampuan via MCP (`http`, `stdio`, dan `sse`)
- **Berfungsi di Mana Saja:** dukung penuh di setiap terminal di macOS, Linux, Windows (PowerShell dan WSL), Android, FreeBSD, OpenBSD, dan NetBSD

## Instalasi

Gunakan package manager:

```bash
# Homebrew
brew install abbayosua/tap/skynet

# NPM
npm install -g @abbayosua/skynet

# Arch Linux (btw)
yay -S skynet-bin

# Nix
nix run github:abbayosua/skynet#skynet

# FreeBSD
pkg install skynet
```

Pengguna Windows:

```bash
# Winget
winget install abbayosua.skynet

# Scoop
scoop bucket add abbayosua https://github.com/abbayosua/scoop-bucket.git
scoop install skynet
```

<details>
<summary><strong>Nix (NUR)</strong></summary>

Skynet tersedia via NUR di `nur.repos.abbayosua.skynet`. Cara paling up-to-date untuk mendapatkan Skynet di Nix.

Kamu juga bisa coba Skynet via NUR dengan `nix-shell`:

```bash
# Add the NUR channel.
nix-channel --add https://github.com/nix-community/NUR/archive/main.tar.gz nur
nix-channel --update

# Get Skynet in a Nix shell.
nix-shell -p '(import <nur> { pkgs = import <nixpkgs> {}; }).repos.abbayosua.skynet'
```

</details>

<details>
<summary><strong>Debian/Ubuntu</strong></summary>

```bash
sudo mkdir -p /etc/apt/keyrings
curl -fsSL https://repo.skynet.sh/apt/gpg.key | sudo gpg --dearmor -o /etc/apt/keyrings/skynet.gpg
echo "deb [signed-by=/etc/apt/keyrings/skynet.gpg] https://repo.skynet.sh/apt/ * *" | sudo tee /etc/apt/sources.list.d/skynet.list
sudo apt update && sudo apt install skynet
```

</details>

<details>
<summary><strong>Fedora/RHEL</strong></summary>

```bash
echo '[skynet]
name=Skynet
baseurl=https://repo.skynet.sh/yum/
enabled=1
gpgcheck=1
gpgkey=https://repo.skynet.sh/yum/gpg.key' | sudo tee /etc/yum.repos.d/skynet.repo
sudo yum install skynet
```

</details>

Atau, unduh langsung:

- [Paket][releases] tersedia dalam format Debian dan RPM
- [Binary][releases] tersedia untuk Linux, macOS, Windows, FreeBSD, OpenBSD, dan NetBSD

[releases]: https://github.com/abbayosua/skynet/releases

Atau instal dengan Go:

```
go install github.com/abbayosua/skynet@latest
```

> [!WARNING]
> Produktivitas bisa meningkat saat menggunakan Skynet dan kamu mungkin akan
> teralihkan saat pertama kali menggunakan aplikasi ini. Jika gejala menetap,
> bergabunglah ke [Slack][slack] atau [Discord][discord] dan alihkan perhatian
> kami yang lain.

## Memulai

Cara tercepat untuk memulai adalah dengan mengambil API key untuk provider
pilihanmu seperti Anthropic, OpenAI, Groq, OpenRouter, atau Vercel AI Gateway
dan jalankan Skynet. Kamu akan diminta memasukkan API key.

Kamu juga bisa mengatur environment variable untuk provider yang didukung.

| Environment Variable        | Provider                                           |
| --------------------------- | -------------------------------------------------- |
| `HYPER_API_KEY`             | Hyper                                              |
| `ANTHROPIC_API_KEY`         | Anthropic                                          |
| `OPENAI_API_KEY`            | OpenAI                                             |
| `VERCEL_API_KEY`            | Vercel AI Gateway                                  |
| `GEMINI_API_KEY`            | Google Gemini                                      |
| `SYNTHETIC_API_KEY`         | Synthetic                                          |
| `ZAI_API_KEY`               | Z.ai                                               |
| `MINIMAX_API_KEY`           | MiniMax                                            |
| `HF_TOKEN`                  | Hugging Face Inference                             |
| `CEREBRAS_API_KEY`          | Cerebras                                           |
| `OPENROUTER_API_KEY`        | OpenRouter                                         |
| `IONET_API_KEY`             | io.net                                             |
| `GROQ_API_KEY`              | Groq                                               |
| `AVIAN_API_KEY`             | Avian                                              |
| `OPENCODE_API_KEY`          | OpenCode Zen & Go                                  |
| `VERTEXAI_PROJECT`          | Google Cloud VertexAI (Gemini)                     |
| `VERTEXAI_LOCATION`         | Google Cloud VertexAI (Gemini)                     |
| `AWS_ACCESS_KEY_ID`         | Amazon Bedrock (Claude)                            |
| `AWS_SECRET_ACCESS_KEY`     | Amazon Bedrock (Claude)                            |
| `AWS_REGION`                | Amazon Bedrock (Claude)                            |
| `AWS_PROFILE`               | Amazon Bedrock (Custom Profile)                    |
| `AWS_BEARER_TOKEN_BEDROCK`  | Amazon Bedrock                                     |
| `AZURE_OPENAI_API_ENDPOINT` | Azure OpenAI models                                |
| `AZURE_OPENAI_API_KEY`      | Azure OpenAI models (optional when using Entra ID) |
| `AZURE_OPENAI_API_VERSION`  | Azure OpenAI models                                |

### Langganan

Jika kamu lebih suka penggunaan berbasis langganan, berikut beberapa paket yang kompatibel dengan Skynet:

- Synthetic
- GLM Coding Plan
- Kimi Code
- MiniMax Coding Plan

## Konfigurasi

> [!TIP]
> Skynet hadir dengan skill bawaan `skynet-config` untuk mengkonfigurasi dirinya sendiri.
> Dalam banyak kasus, kamu cukup meminta Skynet untuk mengkonfigurasi dirinya.

Skynet berjalan dengan baik tanpa konfigurasi. Namun, jika kamu perlu atau ingin
menyesuaikan Skynet, konfigurasi dapat ditambahkan baik lokal di proyek itu sendiri,
atau global, dengan prioritas berikut:

1. `.skynet.json`
2. `skynet.json`
3. `$HOME/.config/skynet/skynet.json`

Konfigurasi disimpan sebagai objek JSON:

```json
{
  "this-setting": { "this": "that" },
  "that-setting": ["ceci", "cela"]
}
```

Sebagai catatan tambahan, Skynet juga menyimpan data sementara, seperti status
aplikasi, di satu lokasi tambahan:

```bash
# Unix
$HOME/.local/share/skynet/skynet.json

# Windows
%LOCALAPPDATA%\skynet\skynet.json
```

> [!TIP]
> Kamu bisa mengganti lokasi config user dan data dengan mengatur:
>
> - `SKYNET_GLOBAL_CONFIG`
> - `SKYNET_GLOBAL_DATA`

### LSP

Skynet dapat menggunakan LSP untuk konteks tambahan guna membantu pengambilan keputusan,
sama seperti yang kamu lakukan. LSP dapat ditambahkan secara manual seperti ini:

```json
{
  "$schema": "https://skynet.land/skynet.json",
  "lsp": {
    "go": {
      "command": "gopls",
      "env": {
        "GOTOOLCHAIN": "go1.24.5"
      }
    },
    "typescript": {
      "command": "typescript-language-server",
      "args": ["--stdio"]
    },
    "nix": {
      "command": "nil"
    }
  }
}
```

### MCP

Skynet juga mendukung server Model Context Protocol (MCP) melalui tiga tipe
transport: `stdio` untuk server command-line, `http` untuk endpoint HTTP, dan `sse`
untuk Server-Sent Events.

Ekspansi nilai gaya shell (`$VAR`, `${VAR:-default}`, `$(command)`, quoting,
nesting) berfungsi di `command`, `args`, `env`, `headers`, dan `url`, jadi
secret berbasis file langsung berfungsi. Kamu bisa menggunakan nilai seperti `"$TOKEN"`
atau `"$(cat /path/to/secret/token)"`. Ekspansi berjalan melalui shell bawaan Skynet,
sehingga sintaks yang sama berfungsi di semua sistem yang didukung, termasuk Windows.

Variabel yang tidak disetel akan diperluas menjadi string kosong secara default, sesuai bash.
Untuk kredensial wajib, gunakan `${VAR:?message}` agar variabel yang tidak disetel gagal
dengan `message` saat dimuat, bukan diam-diam menjadi string kosong:

```json
{ "api_key": "${CODEBERG_TOKEN:?set CODEBERG_TOKEN}" }
```

Header (baik MCP `headers` maupun provider `extra_headers`) yang nilainya
menjadi string kosong akan dihapus dari permintaan keluar, bukan dikirim
sebagai `Header:`. Ini menjaga header opsional yang bergantung pada env seperti
`"OpenAI-Organization": "$OPENAI_ORG_ID"` tetap bersih saat variabel tidak disetel.

Provider `extra_body` adalah JSON passthrough yang tidak diekspansi; taruh nilai
yang digerakkan env di `extra_headers` atau `api_key` / `base_url` provider,
semuanya mendukung ekspansi.

> **Catatan keamanan:** `skynet.json` adalah kode tepercaya. `$(...)` di dalamnya
> berjalan saat dimuat dengan hak istimewa shell kamu, sebelum UI muncul.
> Jangan jalankan Skynet di direktori yang `skynet.json`-nya belum kamu periksa.

```json
{
  "$schema": "https://skynet.land/skynet.json",
  "mcp": {
    "filesystem": {
      "type": "stdio",
      "command": "node",
      "args": ["/path/to/mcp-server.js"],
      "timeout": 120,
      "disabled": false,
      "disabled_tools": ["some-tool-name"],
      "env": {
        "NODE_ENV": "production"
      }
    },
    "github": {
      "type": "http",
      "url": "https://api.githubcopilot.com/mcp/",
      "timeout": 120,
      "disabled": false,
      "disabled_tools": ["create_issue", "create_pull_request"],
      "headers": {
        "Authorization": "Bearer $GH_PAT"
      }
    },
    "streaming-service": {
      "type": "sse",
      "url": "https://example.com/mcp/sse",
      "timeout": 120,
      "disabled": false,
      "headers": {
        "API-Key": "$(echo $API_KEY)"
      }
    }
  }
}
```

### Hooks

Skynet memiliki dukungan awal untuk hooks. Untuk detailnya, lihat
[panduan hooks](./docs/hooks/).

### Mengabaikan File

Skynet menghormati file `.gitignore` secara default, tapi kamu juga bisa membuat
file `.skynetignore` untuk menentukan file dan direktori tambahan yang harus
diabaikan Skynet. Ini berguna untuk mengecualikan file yang ingin ada di version
control tapi tidak ingin dipertimbangkan Skynet saat memberikan konteks.

File `.skynetignore` menggunakan sintaks yang sama dengan `.gitignore` dan bisa
ditempatkan di root proyek atau di subdirektori.

### Mengizinkan Tools

Secara default, Skynet akan meminta izin sebelum menjalankan tool calls. Jika
kamu mau, kamu bisa mengizinkan tool untuk dijalankan tanpa dimintai izin.
Gunakan dengan hati-hati.

```json
{
  "$schema": "https://skynet.land/skynet.json",
  "permissions": {
    "allowed_tools": [
      "view",
      "ls",
      "grep",
      "edit",
      "mcp_context7_get-library-doc"
    ]
  }
}
```

Kamu juga bisa melewati semua prompt izin dengan menjalankan Skynet menggunakan
flag `--yolo`. Berhati-hatilah dengan fitur ini.

### Menonaktifkan Tool Bawaan

Jika kamu ingin mencegah Skynet menggunakan tool bawaan tertentu, kamu bisa
menonaktifkannya melalui daftar `options.disabled_tools`. Tool yang dinonaktifkan
sepenuhnya disembunyikan dari agent.

```json
{
  "$schema": "https://skynet.land/skynet.json",
  "options": {
    "disabled_tools": ["bash", "sourcegraph"]
  }
}
```

Untuk menonaktifkan tool dari server MCP, lihat [bagian konfigurasi MCP](#mcp).

### Menonaktifkan Skills

Jika kamu ingin mencegah Skynet menggunakan skill tertentu, kamu bisa
menonaktifkannya melalui daftar `options.disabled_skills`. Skill yang dinonaktifkan
tersembunyi dari agent, termasuk skill bawaan dan skill yang ditemukan dari disk.

```json
{
  "$schema": "https://skynet.land/skynet.json",
  "options": {
    "disabled_skills": ["skynet-config"]
  }
}
```

### Agent Skills

Skynet mendukung standar terbuka Agent Skills untuk
memperluas kemampuan agent dengan paket skill yang dapat digunakan kembali. Skill
adalah folder yang berisi file `SKILL.md` dengan instruksi yang dapat ditemukan
dan diaktifkan Skynet sesuai kebutuhan.

Lokasi global yang kami cari untuk skill:

* `$SKYNET_SKILLS_DIR`
* `$XDG_CONFIG_HOME/agents/skills` atau `~/.config/agents/skills/`
* `$XDG_CONFIG_HOME/skynet/skills` atau `~/.config/skynet/skills/`
* `~/.agents/skills/`
* `~/.claude/skills/`
* Di Windows, kami _juga_ mencari di
  * `%LOCALAPPDATA%\agents\skills\` atau `%USERPROFILE%\AppData\Local\agents\skills\`
  * `%LOCALAPPDATA%\skynet\skills\` atau `%USERPROFILE%\AppData\Local\skynet\skills\`
* Path tambahan yang dikonfigurasi via `options.skills_paths`

Selain itu, kami _juga_ memuat skill di proyek kamu dari path relatif berikut:

* `.agents/skills`
* `.skynet/skills`
* `.claude/skills`
* `.cursor/skills`

```jsonc
{
  "$schema": "https://skynet.land/skynet.json",
  "options": {
    "skills_paths": [
      "~/.config/skynet/skills", // Windows: "%LOCALAPPDATA%\\skynet\\skills",
      "./project-skills",
    ],
  },
}
```

Kamu bisa memulai dengan contoh skill dari [anthropics/skills](https://github.com/anthropics/skills):

```bash
# Unix
mkdir -p ~/.config/skynet/skills
cd ~/.config/skynet/skills
git clone https://github.com/anthropics/skills.git _temp
mv _temp/skills/* . && rm -rf _temp
```

```powershell
# Windows (PowerShell)
mkdir -Force "$env:LOCALAPPDATA\skynet\skills"
cd "$env:LOCALAPPDATA\skynet\skills"
git clone https://github.com/anthropics/skills.git _temp
mv _temp/skills/* . ; rm -r -force _temp
```

### Notifikasi Desktop

Skynet mengirim notifikasi desktop saat tool call memerlukan izin dan saat
agent menyelesaikan gilirannya. Notifikasi hanya dikirim saat jendela terminal
tidak dalam fokus _dan_ terminal mendukung pelaporan status fokus.

```jsonc
{
  "$schema": "https://skynet.land/skynet.json",
  "options": {
    "disable_notifications": false, // default
  },
}
```

Untuk menonaktifkan notifikasi desktop, setel `disable_notifications` ke `true`
dalam konfigurasi. Di macOS, notifikasi saat ini tidak memiliki ikon karena
keterbatasan platform.

### Inisialisasi

Saat kamu menginisialisasi proyek, Skynet menganalisis codebase kamu dan membuat
file konteks yang membantu bekerja lebih efektif di sesi mendatang.
Secara default, file ini bernama `AGENTS.md`, tapi kamu bisa menyesuaikan
nama dan lokasinya dengan opsi `initialize_as`:

```json
{
  "$schema": "https://skynet.land/skynet.json",
  "options": {
    "initialize_as": "AGENTS.md"
  }
}
```

Ini berguna jika kamu lebih suka konvensi penamaan berbeda atau ingin
menempatkan file di direktori tertentu (misalnya, `SKYNET.md` atau
`docs/LLMs.md`). Skynet akan mengisi file dengan konteks spesifik proyek
seperti perintah build, pola kode, dan konvensi yang ditemukan selama
inisialisasi.

### Pengaturan Atribusi

Secara default, Skynet menambahkan informasi atribusi ke commit Git dan pull request
yang dibuatnya. Kamu bisa menyesuaikan perilaku ini dengan opsi `attribution`:

```json
{
  "$schema": "https://skynet.land/skynet.json",
  "options": {
    "attribution": {
      "trailer_style": "co-authored-by",
      "generated_with": true
    }
  }
}
```

- `trailer_style`: Mengontrol trailer atribusi yang ditambahkan ke pesan commit
  (default: `assisted-by`)
  - `assisted-by`: Menambahkan `Assisted-by: Skynet:[ModelID]`
  - `co-authored-by`: Menambahkan `Co-Authored-By: Skynet <skynet@skynet.land>`
  - `none`: Tanpa trailer atribusi
- `generated_with`: Saat true (default), menambahkan baris `💘 Generated with Skynet` ke
  pesan commit dan deskripsi PR

### Provider Kustom

Skynet mendukung konfigurasi provider kustom untuk API yang kompatibel dengan OpenAI
dan Anthropic.

> [!NOTE]
> Perhatikan bahwa kami mendukung dua "tipe" untuk OpenAI. Pastikan memilih yang tepat
> untuk pengalaman terbaik!
>
> - `openai` harus digunakan saat memproksi atau merutekan permintaan melalui OpenAI.
> - `openai-compat` harus digunakan saat menggunakan provider non-OpenAI yang memiliki API kompatibel dengan OpenAI.

#### API Kompatibel OpenAI

Berikut contoh konfigurasi untuk Deepseek, yang menggunakan API kompatibel
OpenAI. Jangan lupa setel `DEEPSEEK_API_KEY` di environment kamu.

```json
{
  "$schema": "https://skynet.land/skynet.json",
  "providers": {
    "deepseek": {
      "type": "openai-compat",
      "base_url": "https://api.deepseek.com/v1",
      "api_key": "$DEEPSEEK_API_KEY",
      "models": [
        {
          "id": "deepseek-chat",
          "name": "Deepseek V3",
          "cost_per_1m_in": 0.27,
          "cost_per_1m_out": 1.1,
          "cost_per_1m_in_cached": 0.07,
          "cost_per_1m_out_cached": 1.1,
          "context_window": 64000,
          "default_max_tokens": 5000
        }
      ]
    }
  }
}
```

#### API Kompatibel Anthropic

Provider kustom kompatibel Anthropic mengikuti format ini:

```json
{
  "$schema": "https://skynet.land/skynet.json",
  "providers": {
    "custom-anthropic": {
      "type": "anthropic",
      "base_url": "https://api.anthropic.com/v1",
      "api_key": "$ANTHROPIC_API_KEY",
      "extra_headers": {
        "anthropic-version": "2023-06-01"
      },
      "models": [
        {
          "id": "claude-sonnet-4-20250514",
          "name": "Claude Sonnet 4",
          "cost_per_1m_in": 3,
          "cost_per_1m_out": 15,
          "cost_per_1m_in_cached": 3.75,
          "cost_per_1m_out_cached": 0.3,
          "context_window": 200000,
          "default_max_tokens": 50000,
          "can_reason": true,
          "supports_attachments": true
        }
      ]
    }
  }
}
```

### Amazon Bedrock

Skynet saat ini mendukung menjalankan model Anthropic melalui Bedrock, dengan caching dinonaktifkan.

- Provider Bedrock akan muncul setelah kamu memiliki konfigurasi AWS, mis. `aws configure`
- Skynet juga mengharapkan `AWS_REGION` atau `AWS_DEFAULT_REGION` disetel
- Untuk menggunakan profile AWS tertentu, setel `AWS_PROFILE` di environment, mis. `AWS_PROFILE=myprofile skynet`
- Alternatif untuk `aws configure`, kamu juga bisa setel `AWS_BEARER_TOKEN_BEDROCK`

### Vertex AI Platform

Vertex AI akan muncul dalam daftar provider yang tersedia saat `VERTEXAI_PROJECT` dan `VERTEXAI_LOCATION` disetel. Kamu juga perlu diautentikasi:

```bash
gcloud auth application-default login
```

Untuk menambahkan model spesifik ke konfigurasi, atur seperti berikut:

```json
{
  "$schema": "https://skynet.land/skynet.json",
  "providers": {
    "vertexai": {
      "models": [
        {
          "id": "claude-sonnet-4@20250514",
          "name": "VertexAI Sonnet 4",
          "cost_per_1m_in": 3,
          "cost_per_1m_out": 15,
          "cost_per_1m_in_cached": 3.75,
          "cost_per_1m_out_cached": 0.3,
          "context_window": 200000,
          "default_max_tokens": 50000,
          "can_reason": true,
          "supports_attachments": true
        }
      ]
    }
  }
}
```

### Model Lokal

Model lokal juga dapat dikonfigurasi via API kompatibel OpenAI. Berikut dua contoh umum:

#### Ollama

```json
{
  "providers": {
    "ollama": {
      "name": "Ollama",
      "base_url": "http://localhost:11434/v1/",
      "type": "openai-compat",
      "models": [
        {
          "name": "Qwen 3 30B",
          "id": "qwen3:30b",
          "context_window": 256000,
          "default_max_tokens": 20000
        }
      ]
    }
  }
}
```

#### LM Studio

```json
{
  "providers": {
    "lmstudio": {
      "name": "LM Studio",
      "base_url": "http://localhost:1234/v1/",
      "type": "openai-compat",
      "models": [
        {
          "name": "Qwen 3 30B",
          "id": "qwen/qwen3-30b-a3b-2507",
          "context_window": 256000,
          "default_max_tokens": 20000
        }
      ]
    }
  }
}
```

## Logging

Terkadang kamu perlu melihat log. Untungnya, Skynet mencatat berbagai
hal. Log disimpan di `./.skynet/logs/skynet.log` relatif terhadap proyek.

CLI juga memiliki beberapa perintah bantuan untuk memudahkan penelusuran log terbaru:

```bash
# Cetak 1000 baris terakhir
skynet logs

# Cetak 500 baris terakhir
skynet logs --tail 500

# Ikuti log secara real-time
skynet logs --follow
```

Ingin lebih banyak log? Jalankan `skynet` dengan flag `--debug`, atau aktifkan di
konfigurasi:

```json
{
  "$schema": "https://skynet.land/skynet.json",
  "options": {
    "debug": true,
    "debug_lsp": true
  }
}
```

## Pembaruan Provider Otomatis

Secara default, Skynet secara otomatis memeriksa daftar terbaru
provider dan model dari Catwalk, database provider Skynet open source. Ini berarti saat provider dan model baru
tersedia, atau saat metadata model berubah, Skynet secara otomatis
memperbarui konfigurasi lokal kamu.

### Menonaktifkan pembaruan provider otomatis

Bagi mereka dengan akses internet terbatas, atau yang lebih suka bekerja di
lingkungan air-gapped, fitur ini mungkin tidak diinginkan, dan dapat
dinonaktifkan.

Untuk menonaktifkan pembaruan provider otomatis, setel `disable_provider_auto_update` ke
dalam konfigurasi `skynet.json`:

```json
{
  "$schema": "https://skynet.land/skynet.json",
  "options": {
    "disable_provider_auto_update": true
  }
}
```

Atau setel environment variable `SKYNET_DISABLE_PROVIDER_AUTO_UPDATE`:

```bash
export SKYNET_DISABLE_PROVIDER_AUTO_UPDATE=1
```

### Memperbarui provider secara manual

Memperbarui provider secara manual dimungkinkan dengan perintah `skynet update-providers`:

```bash
# Perbarui provider dari Catwalk.
skynet update-providers

# Perbarui provider dari URL Catwalk kustom.
skynet update-providers https://example.com/

# Perbarui provider dari file lokal.
skynet update-providers /path/to/local-providers.json

# Reset provider ke versi bawaan, yang disematkan saat build.
skynet update-providers embedded

# Untuk info lebih lanjut:
skynet update-providers --help
```

## Metrik

Skynet mencatat metrik penggunaan pseudonim (terkait dengan hash perangkat),
yang diandalkan maintainer untuk menginformasikan pengembangan dan prioritas
dukungan. Metrik hanya mencakup metadata penggunaan; prompt dan respons TIDAK
PERNAH dikumpulkan.

Detail tentang apa yang dikumpulkan ada di kode sumber.

Kamu dapat memilih keluar dari pengumpulan metrik kapan saja dengan mengatur
environment variable berikut:

```bash
export SKYNET_DISABLE_METRICS=1
```

Atau dengan mengatur berikut dalam konfigurasi:

```json
{
  "options": {
    "disable_metrics": true
  }
}
```

Skynet juga menghormati konvensi `DO_NOT_TRACK` yang dapat diaktifkan via `export DO_NOT_TRACK=1`.

## Tanya Jawab

### Mengapa salin tempel clipboard tidak berfungsi?

Menginstal tool tambahan mungkin diperlukan di lingkungan mirip Unix.

| Environment         | Tool                     |
| ------------------- | ------------------------ |
| Windows             | Dukungan bawaan          |
| macOS               | Dukungan bawaan          |
| Linux/BSD + Wayland | `wl-copy` dan `wl-paste` |
| Linux/BSD + X11     | `xclip` atau `xsel`      |

## Berkontribusi

Lihat [panduan kontribusi](https://github.com/abbayosua/skynet?tab=contributing-ov-file#contributing).

## Ada masukan?

Kami ingin mendengar pendapatmu tentang proyek ini.

[slack]: https://skynet.land/slack
[discord]: https://skynet.land/discord

## Lisensi

[FSL-1.1-MIT](https://github.com/abbayosua/skynet/raw/main/LICENSE.md)

---

<a href="https://github.com/abbayosua/skynet">Skynet</a>
