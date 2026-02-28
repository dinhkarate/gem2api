# gem2api

Gemini Web → OpenAI API proxy. Uses browser cookies to access gemini.google.com and exposes an OpenAI-compatible API.

## Features

- **OpenAI-compatible API** — `/v1/chat/completions` and `/v1/models`
- **Streaming + non-streaming** — Server-Sent Events (SSE) support
- **Cookie management** — Chrome Extension auto-syncs cookies, admin panel for manual management
- **Multi-account pool** — Load balance across multiple Google accounts
- **Health tracking** — Auto-disable accounts on errors, auto-unban after cooldown
- **Backward compatible** — Works with env var cookies (no DB needed)

## Quick Start

### Option 1: Env vars (simple, single account)

```bash
export SECURE_1PSID="your__Secure-1PSID_cookie_value"
export SECURE_1PSIDTS="your__Secure-1PSIDTS_cookie_value"

make build
./gem2api
```

### Option 2: Admin panel (recommended, multi-account)

```bash
make build
./gem2api
# Open http://localhost:8080/manage
# Login with admin/admin (default)
# Add cookies via the web interface
```

### Option 3: Chrome Extension (automated)

1. Set `CONNECTION_TOKEN=your-secret` on the server
2. Load `extension/` as unpacked extension in Chrome
3. Click extension icon → enter server URL and connection token
4. Extension auto-syncs cookies every 30 minutes

## Getting Cookies

1. Open https://gemini.google.com in your browser
2. Open DevTools → Application → Cookies
3. Copy `__Secure-1PSID` and `__Secure-1PSIDTS` values

## Cookie Management

### Chrome Extension

The included Chrome Extension (Manifest V3) automatically extracts cookies from gemini.google.com and sends them to your gem2api server.

**Setup:**

1. Go to `chrome://extensions` → Enable Developer mode
2. Click "Load unpacked" → Select the `extension/` folder
3. Click the gem2api extension icon
4. Enter your server URL (e.g., `http://localhost:8080`)
5. Enter your connection token (must match `CONNECTION_TOKEN` env var)
6. Click "Save" — extension will sync cookies every 30 minutes

**Features:**

- Alarm-based auto-sync (configurable, default 30 min)
- Manual sync button
- Status display (last sync time, success/error)

### Admin Panel

Access at `http://localhost:8080/manage`:

- **Dashboard**: Active/total accounts, pool stats
- **Account list**: Nickname, status (active/banned/disabled), last used, error count
- **Add account**: Paste cookies manually
- **Per-account actions**: Enable, disable, delete

Default credentials: `admin` / `admin` (change via `ADMIN_USERNAME` / `ADMIN_PASSWORD`)

### Plugin API

For programmatic cookie ingestion (used by Chrome Extension):

```bash
curl -X POST http://localhost:8080/api/cookies/update \
  -H "Authorization: Bearer your-connection-token" \
  -H "Content-Type: application/json" \
  -d '{"secure_1psid": "...", "secure_1psidts": "..."}'
```

### Multi-Account Pool

When multiple accounts are added, gem2api:

- Randomly selects an active account per request
- Retries with a different account on failure
- Auto-disables accounts after consecutive errors (default: 3)
- Auto-unbans accounts after cooldown (default: 1 hour)
- Falls back to env var cookies if no DB accounts available

## Environment Variables

| Variable           | Required | Default           | Description                             |
| ------------------ | -------- | ----------------- | --------------------------------------- |
| `SECURE_1PSID`     | No\*     | —                 | `__Secure-1PSID` cookie (fallback mode) |
| `SECURE_1PSIDTS`   | No       | —                 | `__Secure-1PSIDTS` cookie               |
| `API_KEY`          | No       | —                 | Protect proxy API with Bearer auth      |
| `PORT`             | No       | `8080`            | Server port                             |
| `PROXY_URL`        | No       | —                 | HTTP/SOCKS5 proxy URL                   |
| `DB_PATH`          | No       | `data/gem2api.db` | SQLite database path                    |
| `ADMIN_USERNAME`   | No       | `admin`           | Admin panel username                    |
| `ADMIN_PASSWORD`   | No       | `admin`           | Admin panel password                    |
| `CONNECTION_TOKEN` | No       | —                 | Chrome Extension auth token             |
| `SESSION_TTL`      | No       | `24h`             | Admin session duration                  |
| `ERROR_THRESHOLD`  | No       | `3`               | Consecutive errors before auto-ban      |
| `AUTO_UNBAN_AFTER` | No       | `1h`              | Auto-unban cooldown duration            |

\*Either `SECURE_1PSID` env var or DB accounts (via admin panel/extension) required.

## API Usage

### Chat Completions

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-2.5-flash",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

### Streaming

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-2.5-flash",
    "messages": [{"role": "user", "content": "Tell me a story"}],
    "stream": true
  }'
```

### List Models

```bash
curl http://localhost:8080/v1/models
```

## Available Models

| Model                              | Description                        | Gemini Advanced |
| ---------------------------------- | ---------------------------------- | --------------- |
| `gemini-2.0-flash`                 | Gemini 2.0 Flash                   | No              |
| `gemini-2.5-flash`                 | Gemini 2.5 Flash                   | No              |
| `gemini-2.5-pro`                   | Gemini 2.5 Pro                     | No              |
| `gemini-3-flash`                   | Gemini 3 Flash (free tier)         | No              |
| `gemini-3-flash-thinking`          | Gemini 3 Flash Thinking (free)     | No              |
| `gemini-3-pro`                     | Gemini 3 Pro (free tier)           | No              |
| `gemini-3-flash-advanced`          | Gemini 3 Flash (Advanced tier)     | Yes             |
| `gemini-3-flash-thinking-advanced` | Gemini 3 Flash Thinking (Advanced) | Yes             |
| `gemini-3.1-pro`                   | Gemini 3.1 Pro                     | Yes             |

> **Note**: Model hex IDs change with Google server updates. If a model stops working, check [HanaokaYuzu/Gemini-API](https://github.com/HanaokaYuzu/Gemini-API) for updated hex values and update `internal/gemini/types.go`.

## Docker

```bash
# Using docker-compose
docker compose up -d

# Or directly
docker build -t gem2api .
docker run -p 8080:8080 \
  -v gem2api-data:/app/data \
  -e ADMIN_PASSWORD="your-password" \
  -e CONNECTION_TOKEN="your-token" \
  gem2api
```

The SQLite database is stored in a Docker volume (`gem2api-data`) for persistence across container restarts.

## How It Works

1. **Bootstrap**: Fetches gemini.google.com/app to extract session tokens (CSRF, build label, session ID)
2. **Translate**: Converts OpenAI chat format → Gemini web API format (form-encoded, double-JSON-encoded)
3. **Parse**: Reads Gemini's proprietary length-prefixed response frames (UTF-16 code unit lengths)
4. **Stream**: Converts snapshot-streaming (cumulative text) to SSE delta streaming
5. **Pool**: Selects from active accounts, retries on failure, tracks health
6. **Rotate**: Background cookie rotation every ~9 minutes per account

## Architecture

```
[Chrome Extension] ──→ POST /api/cookies/update ──→ [SQLite DB]
                                                         │
[Admin Panel /manage] ──→ CRUD /api/admin/cookies ──→────┘
                                                         │
[Client] ──→ POST /v1/chat/completions                   │
                    │                                     │
                    ▼                                     ▼
             [Cookie Pool] ← random select ← [Active Accounts]
                    │
                    ▼
             [Gemini Client] ──→ gemini.google.com
                    │
                    ▼
             [Response Parser] ──→ OpenAI SSE format
```

## Known Limitations & Risks

| Risk                       | Impact                                  | Mitigation                               |
| -------------------------- | --------------------------------------- | ---------------------------------------- |
| **TLS fingerprinting**     | Go `net/http` detectable as non-browser | Use `PROXY_URL` with residential proxy   |
| **GOOGLE_ABUSE_EXEMPTION** | Triggered by datacenter IPs             | Use residential IP                       |
| **Account-level flagging** | Heavy use → forced sign-out             | Multi-account pool distributes load      |
| **Cookie rotation 429**    | Too-frequent rotation → rate limited    | Built-in 9-min interval                  |
| **Model hex ID drift**     | IDs change without notice               | Check HanaokaYuzu/Gemini-API for updates |

## TODO

- [ ] **TLS fingerprint impersonation** — Use [utls](https://github.com/refraction-networking/utls) to mimic Chrome TLS handshake
- [ ] **Image/file upload support** — Convert base64 image content parts to Gemini's file format
- [ ] **Conversation continuity** — Use cid/rid/rcid from responses to maintain multi-turn context
- [ ] **Thinking model output** — Extract model thoughts from `content[4][i][37][0][0]`
- [ ] **Dynamic model discovery** — Scrape available models from web UI
- [ ] **Cookie validation on add** — Test bootstrap before saving account to DB
- [ ] **Per-account concurrency limits** — Prevent overloading individual accounts

## Build

```bash
make build          # Build for current platform
make build-linux    # Cross-compile for Linux amd64
make build-darwin   # Cross-compile for macOS arm64
make build-all      # Build all platforms
make test           # Run tests
```

## Credits

- [HanaokaYuzu/Gemini-API](https://github.com/HanaokaYuzu/Gemini-API) — Python reference implementation for Gemini web API
- [CLIProxyAPI](https://github.com/patchescamerababy/CLIProxyAPI) — Go proxy architecture reference
- [flow2api](https://github.com/TheSmallHanCat/flow2api) — Cookie management and token pool architecture reference
