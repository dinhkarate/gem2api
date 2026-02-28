# gem2api

Gemini Web → OpenAI API proxy. Uses browser cookies to access gemini.google.com and exposes an OpenAI-compatible API.

## Quick Start

```bash
# Set your cookies from gemini.google.com
export SECURE_1PSID="your__Secure-1PSID_cookie_value"
export SECURE_1PSIDTS="your__Secure-1PSIDTS_cookie_value"

# Build and run
make build
./gem2api
```

## Getting Cookies

1. Open https://gemini.google.com in your browser
2. Open DevTools → Application → Cookies
3. Copy the values of `__Secure-1PSID` and `__Secure-1PSIDTS`

## Environment Variables

| Variable         | Required | Description                                           |
| ---------------- | -------- | ----------------------------------------------------- |
| `SECURE_1PSID`   | Yes      | `__Secure-1PSID` cookie from gemini.google.com        |
| `SECURE_1PSIDTS` | No       | `__Secure-1PSIDTS` cookie (recommended, auto-rotated) |
| `API_KEY`        | No       | Protect proxy access with an API key                  |
| `PORT`           | No       | Server port (default: 8080)                           |
| `PROXY_URL`      | No       | HTTP/SOCKS5 proxy URL                                 |

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

> **Note**: Google splits Gemini 3 models into free (`ai-free`) and Advanced (`ai-pro`) tiers with different hex IDs. The "3.1" label only applies to Pro. Advanced models require a paid Gemini Advanced subscription.
>
> Model hex IDs change with Google server updates. If a model stops working, check [HanaokaYuzu/Gemini-API](https://github.com/HanaokaYuzu/Gemini-API) for updated hex values and update `internal/gemini/types.go`.

## Docker

```bash
# Using docker-compose
cp .env.example .env  # Edit with your cookies
docker compose up -d

# Or directly
docker build -t gem2api .
docker run -p 8080:8080 \
  -e SECURE_1PSID="..." \
  -e SECURE_1PSIDTS="..." \
  gem2api
```

## How It Works

1. **Bootstrap**: Fetches gemini.google.com/app to extract session tokens (CSRF, build label, session ID)
2. **Translate**: Converts OpenAI chat format → Gemini web API format (form-encoded, double-JSON-encoded)
3. **Parse**: Reads Gemini's proprietary length-prefixed response frames (UTF-16 code unit lengths)
4. **Stream**: Converts snapshot-streaming (cumulative text) to SSE delta streaming
5. **Rotate**: Background cookie rotation every ~9 minutes

## Known Limitations & Risks

### No reCAPTCHA (unlike flow2api)

The gemini.google.com web API does **not** require reCAPTCHA tokens per request (unlike flow2api which targets Google Flow/VideoFX). Authentication is purely cookie-based.

### Bot Detection

Google uses multiple layers of detection:

| Detection Method           | Impact                                                     | Mitigation                                     |
| -------------------------- | ---------------------------------------------------------- | ---------------------------------------------- |
| **TLS fingerprinting**     | Go's `net/http` TLS signature is detectable as non-browser | Use `PROXY_URL` with residential proxy         |
| **GOOGLE_ABUSE_EXEMPTION** | Triggered by datacenter IPs, VPNs, geo-restricted regions  | Use residential IP, avoid datacenter/VPS       |
| **Account-level flagging** | Heavy automated use → forced sign-out, CAPTCHA on re-login | Moderate request frequency, use fresh accounts |
| **Cookie rotation 429**    | Too-frequent `/RotateCookies` calls → rate limited         | Built-in 9-min interval (already handled)      |

### Rate Limits

- No hard per-query rate limit documented for StreamGenerate
- Pro models (`gemini-3.x-pro`) may stall/queue during peak hours
- Cookie rotation endpoint can return 429 under heavy use

### Model Hex ID Drift

Model hex IDs (`x-goog-ext-525001261-jspb` header values) change without notice when Google updates servers. If a model returns errors, the hex ID likely needs updating.

## TODO

- [ ] **TLS fingerprint impersonation** — Use [utls](https://github.com/refraction-networking/utls) to mimic Chrome TLS handshake (similar to curl-cffi in Python)
- [ ] **Multi-account cookie pool** — Round-robin across multiple `__Secure-1PSID` cookies for higher throughput
- [ ] **Image/file upload support** — Convert base64 image content parts to Gemini's file format
- [ ] **Conversation continuity** — Use cid/rid/rcid from responses to maintain multi-turn context on Gemini side
- [ ] **Thinking model output** — Extract and expose model thoughts from `content[4][i][37][0][0]` for thinking models
- [ ] **Dynamic model discovery** — Scrape available models from web UI instead of hardcoding hex IDs
- [ ] **Graceful session refresh** — Re-bootstrap session tokens when CSRF token expires (currently requires restart)
- [ ] **Additional `x-goog-ext-*` headers** — Newer Gemini web client sends extra headers (`x-goog-ext-525005358-jspb` with UUID, etc.) that may become required

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
