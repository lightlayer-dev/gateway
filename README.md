# LightLayer Gateway

[![CI](https://github.com/lightlayer-dev/gateway/actions/workflows/ci.yml/badge.svg)](https://github.com/lightlayer-dev/gateway/actions/workflows/ci.yml)
![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-BSL_1.1-blue)

**A reverse proxy that makes any API agent-ready.** Zero code changes — put it in front of your API and AI agents can discover, authenticate, pay for, and interact with your service automatically.

Think **Cloudflare, but for AI agent traffic.**

---

## Install

### Download Binary (recommended)

Download the latest release for your platform from [GitHub Releases](https://github.com/lightlayer-dev/gateway/releases/latest):

```bash
# Linux (amd64)
curl -fsSL https://github.com/lightlayer-dev/gateway/releases/latest/download/lightlayer-gateway-linux-amd64.tar.gz | tar xz

# Linux (arm64)
curl -fsSL https://github.com/lightlayer-dev/gateway/releases/latest/download/lightlayer-gateway-linux-arm64.tar.gz | tar xz

# macOS (Apple Silicon)
curl -fsSL https://github.com/lightlayer-dev/gateway/releases/latest/download/lightlayer-gateway-darwin-arm64.tar.gz | tar xz

# macOS (Intel)
curl -fsSL https://github.com/lightlayer-dev/gateway/releases/latest/download/lightlayer-gateway-darwin-amd64.tar.gz | tar xz
```

### Quick Start

```bash
lightlayer-gateway init      # Generate gateway.yaml
# Edit gateway.yaml — set your origin URL
lightlayer-gateway start     # Start the proxy
```

The gateway is now running:
- **Proxy** → http://localhost:8080 (point agent traffic here)
- **Dashboard** → http://localhost:9090 (web UI for configuration)

### Docker

```bash
docker run -d \
  -p 8080:8080 -p 9090:9090 \
  -v $(pwd)/gateway.yaml:/etc/lightlayer/gateway.yaml \
  -v gateway-data:/var/lib/lightlayer \
  ghcr.io/lightlayer-dev/gateway:latest
```

Or with Docker Compose:

```yaml
# docker-compose.yml
services:
  gateway:
    image: ghcr.io/lightlayer-dev/gateway:latest
    ports:
      - "8080:8080"
      - "9090:9090"
    volumes:
      - ./gateway.yaml:/etc/lightlayer/gateway.yaml
      - gateway-data:/var/lib/lightlayer
    environment:
      - LIGHTLAYER_CONFIG=/etc/lightlayer/gateway.yaml
    restart: unless-stopped

volumes:
  gateway-data:
```

### Build from Source

```bash
make build    # Builds UI + Go binary
make test     # Run tests
make run      # Build and start
```

---

## What It Does

LightLayer Gateway sits between AI agents and your API. It automatically handles:

| Feature | Description |
|---------|-------------|
| **Discovery** | Serves `/.well-known/ai`, `/.well-known/agent.json`, `/llms.txt`, `/agents.txt` — so agents can find and understand your API |
| **Identity** | Verifies agent credentials (JWT/SPIFFE/WIMSE) per the IETF draft-klrc-aiagent-auth spec |
| **Rate Limiting** | Per-agent sliding window rate limits with configurable overrides |
| **Payments** | x402 micropayment negotiation — agents pay per request |
| **Analytics** | Agent traffic logging with SQLite storage and dashboard charts |
| **Security** | CORS, HSTS, CSP, X-Content-Type-Options, and more |
| **OAuth2** | Full PKCE authorization flow with discovery endpoint |
| **MCP** | Auto-generates Model Context Protocol tools from your API |
| **A2A** | Full Google A2A protocol server — your REST API becomes A2A-compatible |
| **AG-UI** | SSE streaming for CopilotKit, Google ADK, and other agent UIs |
| **API Keys** | Scoped API key auth as a simpler alternative to OAuth2 |
| **agents.txt** | Per-agent access control with rate limits and preferred interface |

<!-- Screenshot placeholder: ![Dashboard](docs/dashboard.png) -->

---

## Architecture

```
┌─────────────┐     ┌──────────────────────────┐     ┌──────────────┐
│   AI Agent   │────▶│   LightLayer Gateway     │────▶│  Origin API  │
│  (Claude,    │     │                          │     │  (any lang,  │
│   GPT, etc.) │◀────│  Security → Discovery →  │◀────│   any stack) │
└─────────────┘     │  Identity → Rate Limits → │     └──────────────┘
                    │  Payments → Analytics →   │
                    │  → Reverse Proxy          │
                    │                          │
                    │  Dashboard UI (port 9090) │
                    └──────────────────────────┘
```

The plugin pipeline executes in order: Security → Discovery → OAuth2 → MCP → A2A → AG-UI → agents.txt → API Keys → Identity → Rate Limits → Payments → Analytics → Proxy.

Single binary. Single container. No external databases — SQLite handles everything.

---

## Configuration

The gateway is configured via `gateway.yaml`. Generate a starter config with:

```bash
lightlayer-gateway init
```

### Configuration Reference

```yaml
gateway:
  listen:
    port: 8080              # Proxy listen port
    host: 0.0.0.0           # Bind address
    # tls:
    #   cert: /path/to/cert.pem
    #   key: /path/to/key.pem

  origin:
    url: https://api.example.com  # Your API's URL
    timeout: 30s                  # Request timeout
    # retries: 2                  # Retry failed requests

plugins:
  # Agent discovery — serves machine-readable API metadata
  discovery:
    enabled: true
    name: "My API"
    description: "Description of your API"
    version: "1.0.0"
    capabilities:
      - name: "widgets"
        description: "CRUD operations for widgets"
        methods: ["GET", "POST", "PUT", "DELETE"]
        paths: ["/api/widgets", "/api/widgets/*"]

  # Agent identity verification
  identity:
    enabled: true
    mode: log              # log | warn | enforce
    # trusted_issuers:
    #   - https://auth.anthropic.com

  # x402 micropayments
  payments:
    enabled: false
    # facilitator: https://x402.org/facilitator
    # routes:
    #   - path: /api/premium/*
    #     price: "0.01"
    #     currency: USDC

  # Per-agent rate limiting
  rate_limits:
    enabled: true
    default:
      requests: 100
      window: 1m
    # per_agent:
    #   claude: { requests: 500, window: 1m }

  # Traffic analytics
  analytics:
    enabled: true
    log_file: ./agent-traffic.log
    # db_path: ./analytics.db    # SQLite for dashboard charts
    # endpoint: https://your-endpoint.com/events

  # Security headers + CORS
  security:
    enabled: true
    # cors_origins: ["*"]

  # OAuth2 with PKCE
  oauth2:
    enabled: false
    # client_id: your-client-id

  # MCP tool server (auto-generated from discovery)
  mcp:
    enabled: false
    # endpoint: /mcp

  # Google A2A protocol server
  a2a:
    enabled: false
    # endpoint: /a2a
    # streaming: true

  # AG-UI SSE streaming
  ag_ui:
    enabled: false
    # endpoint: /ag-ui

  # Scoped API keys
  api_keys:
    enabled: false
    # keys:
    #   - id: key_prod_abc123
    #     scopes: [read, write]

  # agents.txt access control
  agents_txt:
    enabled: true

admin:
  enabled: true
  port: 9090               # Dashboard + Admin API port
  # auth_token: your-secret
```

### Environment Variable Overrides

The config file is the source of truth. Env vars are only for bootstrap-level overrides — the things you need before or instead of a config file:

| Variable | Config Path |
|----------|-------------|
| `LIGHTLAYER_CONFIG` | Config file path |
| `LIGHTLAYER_PORT` | `gateway.listen.port` |
| `LIGHTLAYER_HOST` | `gateway.listen.host` |
| `LIGHTLAYER_ORIGIN_URL` | `gateway.origin.url` |
| `LIGHTLAYER_ADMIN_PORT` | `admin.port` |

Everything else (plugins, TLS, rate limits, payments, etc.) goes in `gateway.yaml`. For Docker, mount the config file.

---

## Plugin Guide

### Enabling/Disabling Plugins

Every plugin can be toggled via the config file or the dashboard UI:

```yaml
plugins:
  discovery:
    enabled: true    # Toggle on/off
```

### Plugin Execution Order

Plugins execute as middleware in a fixed order optimized for correctness:

1. **Security** — CORS + security headers (runs first to protect all responses)
2. **Discovery** — Intercepts `/.well-known/ai`, `/agents.txt`, `/llms.txt`
3. **OAuth2** — Intercepts auth endpoints
4. **MCP** — Intercepts `/mcp` (JSON-RPC 2.0)
5. **A2A** — Intercepts `/a2a` (JSON-RPC 2.0 task lifecycle)
6. **AG-UI** — Intercepts `/ag-ui` (SSE streaming)
7. **agents.txt** — Enforces per-agent path access rules
8. **API Keys** — Validates scoped API keys
9. **Identity** — Verifies agent JWT/SPIFFE credentials
10. **Rate Limits** — Enforces per-agent rate limits
11. **Payments** — x402 payment negotiation
12. **Analytics** — Logs request (async, non-blocking)
13. **→ Reverse Proxy → Origin**

### Writing Custom Plugins

Plugins implement the `Plugin` interface:

```go
type Plugin interface {
    Name() string
    Init(cfg map[string]interface{}) error
    Middleware() func(http.Handler) http.Handler
    Close() error
}
```

Register your plugin in an `init()` function:

```go
func init() {
    plugins.Register("my_plugin", func() plugins.Plugin {
        return &MyPlugin{}
    })
}
```

---

## CLI Reference

```
lightlayer-gateway [command]

Commands:
  init        Generate a starter gateway.yaml config file
  start       Start the gateway proxy server
    --config, -c    Path to config file (default: gateway.yaml)
  dev         Start in development mode (verbose logging, auto-reload)
    --config, -c    Path to config file (default: gateway.yaml)
  validate    Validate a config file
    --config, -c    Path to config file (default: gateway.yaml)
  status      Check running gateway status via admin API
    --config, -c    Path to config file (default: gateway.yaml)
  score       Score an API's agent-readiness (0-100)
    --verbose, -v   Show detailed suggestions per check
    --json          Output machine-readable JSON
    --timeout       Per-request timeout (default: 10s)
  version     Print version
  help        Help about any command
```

### Agent-Readiness Score

Scan any API to see how agent-friendly it is:

```bash
lightlayer-gateway score https://api.example.com
```

```
🤖 Agent-Readiness Score: 34/100 (D)
   https://api.example.com — 1250ms

  ❌ Agent Discovery Endpoints (0/10)
     No agent discovery endpoints found

  ❌ llms.txt (0/10)
     No /llms.txt found

  ✅ Content-Type Headers (10/10)
     Content-Type header present with charset

  ⚠️ Rate Limit Headers (4/10)
     Some rate limit headers present

🔧 Quick wins to improve your score:
   • Serve /.well-known/ai and /.well-known/agent.json
   • Add /llms.txt with structured markdown
   • Include X-RateLimit-Remaining and Reset headers

💡 With LightLayer Gateway, your score would be: 89/100 (B) (+55)
```

Use `--json` for CI integration or `--verbose` for detailed suggestions.

---

## Deployment

### Systemd

```ini
[Unit]
Description=LightLayer Gateway
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/lightlayer-gateway start --config /etc/lightlayer/gateway.yaml
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

### Behind a Reverse Proxy (nginx)

```nginx
server {
    listen 443 ssl;
    server_name api.example.com;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # SSE streaming support
        proxy_buffering off;
        proxy_cache off;
    }
}
```

### Production Checklist

- [ ] Set `admin.auth_token` to protect the dashboard
- [ ] Configure TLS (or terminate at a load balancer)
- [ ] Set `identity.mode: enforce` for production
- [ ] Configure `analytics.db_path` for persistent analytics
- [ ] Set up log rotation for `analytics.log_file`
- [ ] Mount a persistent volume for SQLite data

---

## Performance

Benchmarks run on a single-core VM (DO-Premium-Intel). Proxy latency target: <2ms.

| Benchmark | Latency | Allocs/op |
|-----------|---------|-----------|
| Bare proxy (no plugins) | ~120 µs | 105 |
| All plugins enabled | ~19 µs | 40 |
| 1000 concurrent requests | ~25 µs/req | 55 |
| **Proxy latency overhead** | **~22 µs (0.022 ms)** | 51 |

Binary size: **18 MB** (with embedded dashboard UI).

Run benchmarks yourself:

```bash
go test -bench=. -benchmem ./internal/proxy/
```

---

## Development

```bash
# Prerequisites
go 1.22+
node 20+

# Build everything
make build

# Run tests
make test

# Run with race detector
go test -race ./...

# Dev mode (auto-reload on config changes)
lightlayer-gateway dev --config gateway.yaml
```

---

## See Also

- **[agent-layer-ts](https://github.com/lightlayer-dev/agent-layer-ts)** — Code-level middleware for Express, Koa, Hono, and Fastify (MIT)
- **[agent-layer-python](https://github.com/lightlayer-dev/agent-layer-python)** — Code-level middleware for FastAPI, Flask, and Django (MIT)
- **[agent-bench](https://github.com/lightlayer-dev/agent-bench)** — Benchmark CLI for agent-readiness scoring
- **[company.lightlayer.dev](https://company.lightlayer.dev)** — Blog and research on the agent-native web

## License

LightLayer Gateway is licensed under the [Business Source License 1.1](LICENSE) (BSL 1.1).

- **Use, modify, and self-host freely** — no restrictions for your own use, internal use, or consulting
- **Cannot be offered as a commercial hosted service** — you can't resell it as a managed gateway service
- **Converts to Apache 2.0** on March 28, 2030 (4 years from release)

This means: use it however you want for your own APIs. The only restriction is you can't take this code and sell "gateway-as-a-service" to others.

For commercial licensing inquiries, contact isaac@lightlayer.dev.

© 2026 Flockly, Inc.
