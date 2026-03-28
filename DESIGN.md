# LightLayer Gateway — Design Document

## Vision

A standalone reverse proxy with a web dashboard that sits between AI agents and APIs. Zero code changes for the API owner. Configure via a Cloudflare-style web UI or YAML, point agent traffic through us, and we handle identity verification, payment negotiation, discovery serving, rate limiting, and analytics — automatically.

Think Cloudflare, but specifically for AI agent traffic.

## Business Model

- **Now:** Fully self-hosted, open source (BSL 1.1 license — free to use, can't resell as a hosted service)
- **Future:** Hosted service (we run it for you, pay per usage)
- **License:** Business Source License 1.1 — use, modify, self-host freely. Cannot offer as a commercial managed service. Each version converts to Apache 2.0 after 4 years.

## Why Go

- **Purpose-built for proxies** — Caddy, Traefik, Kong are all Go. net/http is best-in-class.
- **Single binary** — compile once, distribute everywhere. No runtime dependencies.
- **Performance** — low latency, low memory, excellent concurrency via goroutines.
- **Tiny Docker images** — ~10MB with scratch/distroless base.
- **Industry standard** — this is what infrastructure software is written in.

## Architecture

```
┌─────────────┐     ┌──────────────────────────┐     ┌──────────────┐
│   AI Agent   │────▶│   LightLayer Gateway     │────▶│  Origin API  │
│  (Claude,    │     │                          │     │  (any lang,  │
│   GPT, etc.) │◀────│  ┌─────────┐ ┌────────┐ │◀────│   any stack) │
└─────────────┘     │  │Identity │ │Payment │ │     └──────────────┘
                    │  │  Check  │ │ x402   │ │
                    │  └─────────┘ └────────┘ │
                    │  ┌─────────┐ ┌────────┐ │
                    │  │Discovery│ │Analytics│ │
                    │  │ Serving │ │Logging │ │
                    │  └─────────┘ └────────┘ │
                    │  ┌─────────┐ ┌────────┐ │
                    │  │  Rate   │ │Security│ │
                    │  │ Limits  │ │Headers │ │
                    │  └─────────┘ └────────┘ │
                    └──────────────────────────┘
```

### Core Components

1. **Proxy Engine** — net/http reverse proxy using httputil.ReverseProxy
2. **Plugin Pipeline** — ordered middleware chain (Go http.Handler pattern)
3. **Config Loader** — YAML config with validation, env var overrides, hot reload
4. **Admin API** — separate HTTP server for health, metrics, runtime config
5. **CLI** — `lightlayer-gateway` binary with init/start/validate/dev subcommands

### Technology Stack

**Backend (Go):**
- **Proxy:** net/http + httputil.ReverseProxy
- **Config:** gopkg.in/yaml.v3
- **CLI:** cobra
- **JWT:** golang-jwt/jwt/v5
- **Database:** SQLite (embedded, zero-config) for config/analytics storage — no external DB required for self-hosted
- **Logging:** slog (stdlib, structured)
- **Testing:** stdlib testing + testify

**Frontend (Dashboard UI):**
- **Framework:** React + TypeScript (Vite)
- **UI:** Tailwind CSS + shadcn/ui (clean, modern, Cloudflare-esque)
- **State:** TanStack Query (server state), Zustand (client state)
- **Charts:** Recharts (analytics visualizations)
- **Embedded:** Built frontend is embedded in the Go binary via `embed` — single binary serves both proxy + UI
- **API:** Go backend serves a REST API that the dashboard consumes

### Dashboard UI Design

The dashboard is the primary way most users interact with the gateway. Inspired by Cloudflare's dashboard:

**Pages:**
1. **Overview** — proxy status, uptime, request count, latency, origin health (like Cloudflare home)
2. **Analytics** — agent traffic charts: requests over time, top agents, top paths, error rates, response times
3. **Plugins** — toggle plugins on/off, configure each one (discovery, identity, rate limits, payments, security)
4. **Discovery** — edit API name/description/capabilities, preview generated endpoints
5. **Rate Limits** — visual rule builder: default limits, per-agent overrides, see current usage
6. **Identity** — configure verification mode, manage trusted issuers, see agent activity
7. **Payments** — configure paid routes, prices, see payment history
8. **Settings** — origin URL, listen port, TLS, admin settings, export/import YAML config
9. **Logs** — real-time request log viewer with filtering (by agent, path, status, etc.)

**UI Principles:**
- Clean, professional, minimal — no clutter (Cloudflare-inspired)
- Every setting changeable from UI writes back to config (YAML or DB)
- Real-time updates where possible (WebSocket for live logs/metrics)
- Mobile-responsive
- Dark mode support

## Configuration Design

Inspired by: Caddy (Caddyfile), Traefik (traefik.yml), Cloudflare Workers (wrangler.toml).

### `gateway.yaml` — Primary Config File

```yaml
# LightLayer Gateway Configuration
gateway:
  listen:
    port: 8080
    host: 0.0.0.0
    # tls:
    #   cert: /path/to/cert.pem
    #   key: /path/to/key.pem

  origin:
    url: https://api.example.com
    timeout: 30s
    # retries: 2

plugins:
  discovery:
    enabled: true
    name: "Example API"
    description: "A REST API for managing widgets"
    version: "1.0.0"
    capabilities:
      - name: "widgets"
        description: "CRUD operations for widgets"
        methods: ["GET", "POST", "PUT", "DELETE"]
        paths: ["/api/widgets", "/api/widgets/*"]
    # Serves: /.well-known/ai, /.well-known/agent.json, /agents.txt, /llms.txt

  identity:
    enabled: true
    mode: enforce  # log | warn | enforce
    # trusted_issuers:
    #   - https://auth.anthropic.com

  payments:
    enabled: false
    # facilitator: https://x402.org/facilitator
    # routes:
    #   - path: /api/premium/*
    #     price: "0.01"
    #     currency: USDC

  rate_limits:
    enabled: true
    default:
      requests: 100
      window: 1m
    # per_agent:
    #   claude: { requests: 500, window: 1m }

  analytics:
    enabled: true
    log_file: ./agent-traffic.log
    # endpoint: https://dashboard.lightlayer.dev/api/events
    # api_key: your-key

  security:
    enabled: true
    # cors_origins: ["*"]

admin:
  enabled: true
  port: 9090
  # auth_token: your-secret-token
```

### Config Principles

1. **Sensible defaults** — minimal config to start, add plugins as needed
2. **Progressive disclosure** — 5 lines for a bare proxy, full config for production
3. **Self-documenting** — generated config file has comments explaining every option
4. **Env var overrides** — `LIGHTLAYER_PORT`, `LIGHTLAYER_ORIGIN_URL`, etc.
5. **Hot reload** — SIGHUP or file watch triggers config reload without restart

## CLI Design

```bash
# Initialize config
lightlayer-gateway init

# Validate config
lightlayer-gateway validate

# Start the gateway
lightlayer-gateway start

# Start with specific config
lightlayer-gateway start --config ./production.yaml

# Dev mode (verbose, auto-reload)
lightlayer-gateway dev

# Check status (queries admin API)
lightlayer-gateway status
```

### Startup Output

```
 ⚡ LightLayer Gateway v0.1.0

  Listening:  http://localhost:8080
  Origin:     https://api.example.com
  Admin:      http://localhost:9090

  Plugins:
    ✓ discovery   serving /.well-known/ai, /agents.txt, /llms.txt
    ✓ identity    enforcing agent verification
    ✓ rate_limits 100 req/min default
    ✓ analytics   logging to ./agent-traffic.log
    ✓ security    CORS + robots.txt

  Ready to proxy agent traffic.
```

## Plugin Architecture

Go-native middleware pattern using http.Handler:

```go
// Plugin is the interface all gateway plugins implement.
type Plugin interface {
    Name() string
    Init(cfg map[string]interface{}) error
    Middleware() func(http.Handler) http.Handler
    Close() error
}

// RequestContext carries per-request metadata through the pipeline.
type RequestContext struct {
    RequestID  string
    StartTime  time.Time
    AgentInfo  *AgentInfo
    Metadata   map[string]interface{}
}

// AgentInfo describes a detected AI agent.
type AgentInfo struct {
    Detected bool
    Name     string
    Provider string
    Version  string
    Verified bool
}
```

Plugins wrap as standard Go middleware, composable via `alice` or manual chaining:

```go
handler := security.Middleware()(
    discovery.Middleware()(
        identity.Middleware()(
            rateLimit.Middleware()(
                payments.Middleware()(
                    analytics.Middleware()(
                        reverseProxy,
                    ),
                ),
            ),
        ),
    ),
)
```

### Plugin Execution Order

1. **Security** — CORS, security headers
2. **Discovery** — intercept well-known paths
3. **Identity** — verify agent credentials
4. **Rate Limits** — per-agent rate limiting
5. **Payments** — x402 payment negotiation
6. **Analytics** — log request (non-blocking)
7. **→ Reverse Proxy → Origin**

## File Structure

```
gateway/
├── cmd/
│   └── gateway/
│       └── main.go              # Entry point
├── internal/
│   ├── cli/
│   │   ├── root.go              # Cobra root command
│   │   ├── init.go              # init subcommand
│   │   ├── start.go             # start subcommand
│   │   ├── validate.go          # validate subcommand
│   │   ├── dev.go               # dev subcommand
│   │   └── status.go            # status subcommand
│   ├── config/
│   │   ├── config.go            # Config structs + loader
│   │   ├── defaults.go          # Default values
│   │   ├── env.go               # Env var overrides
│   │   ├── validate.go          # Config validation
│   │   └── watcher.go           # File watch + hot reload
│   ├── proxy/
│   │   ├── proxy.go             # Reverse proxy engine
│   │   ├── headers.go           # Header manipulation
│   │   └── transport.go         # Custom transport (timeouts, retries)
│   ├── plugins/
│   │   ├── plugin.go            # Plugin interface
│   │   ├── pipeline.go          # Plugin pipeline builder
│   │   ├── discovery/
│   │   │   └── discovery.go     # Discovery endpoint serving
│   │   ├── identity/
│   │   │   └── identity.go      # Agent identity verification
│   │   ├── ratelimit/
│   │   │   └── ratelimit.go     # Per-agent rate limiting
│   │   ├── payments/
│   │   │   └── payments.go      # x402 payment handling
│   │   ├── analytics/
│   │   │   └── analytics.go     # Traffic analytics
│   │   └── security/
│   │       └── security.go      # CORS, security headers
│   ├── detection/
│   │   └── agent.go             # Agent User-Agent detection
│   ├── admin/
│   │   ├── admin.go             # Admin/Dashboard API server
│   │   ├── routes.go            # REST API routes for dashboard
│   │   └── websocket.go         # WebSocket for live logs/metrics
│   └── store/
│       ├── store.go             # Storage interface
│       └── sqlite.go            # SQLite implementation (config, analytics, sessions)
├── ui/                          # Frontend dashboard (React + TypeScript)
│   ├── package.json
│   ├── vite.config.ts
│   ├── tsconfig.json
│   ├── index.html
│   ├── src/
│   │   ├── main.tsx
│   │   ├── App.tsx
│   │   ├── pages/
│   │   │   ├── Overview.tsx     # Status, uptime, request count
│   │   │   ├── Analytics.tsx    # Traffic charts, top agents
│   │   │   ├── Plugins.tsx      # Toggle/configure plugins
│   │   │   ├── Discovery.tsx    # Edit API description, preview endpoints
│   │   │   ├── RateLimits.tsx   # Rule builder, usage display
│   │   │   ├── Identity.tsx     # Verification config, agent activity
│   │   │   ├── Payments.tsx     # Paid routes, payment history
│   │   │   ├── Settings.tsx     # Origin, port, TLS, YAML export/import
│   │   │   └── Logs.tsx         # Real-time request log viewer
│   │   ├── components/
│   │   │   ├── Layout.tsx       # Sidebar + header shell
│   │   │   ├── Sidebar.tsx      # Navigation sidebar
│   │   │   ├── MetricCard.tsx   # Stat card component
│   │   │   └── Chart.tsx        # Reusable chart wrapper
│   │   ├── lib/
│   │   │   ├── api.ts           # API client for gateway admin endpoints
│   │   │   └── ws.ts            # WebSocket client for live data
│   │   └── styles/
│   │       └── globals.css      # Tailwind imports
│   └── public/
├── configs/
│   └── gateway.yaml             # Default config template
├── docker-compose.yml           # Full self-hosted setup
├── go.mod
├── go.sum
├── Dockerfile                   # Multi-stage: build Go + build UI → single image
├── Makefile
├── .github/
│   └── workflows/
│       └── ci.yml
├── DESIGN.md
├── README.md
└── LICENSE                      # BSL 1.1
```

## Distribution

1. **Docker Compose** (primary) — `docker compose up` spins up gateway + UI, everything included
2. **Single binary** — Go binary with embedded UI assets, GitHub Releases (linux/darwin, amd64/arm64)
3. **Docker image** — `docker run ghcr.io/lightlayer-dev/gateway`
4. **Homebrew** — `brew install lightlayer/tap/gateway`

### Self-Hosted Setup (docker-compose.yml)

```yaml
services:
  gateway:
    image: ghcr.io/lightlayer-dev/gateway:latest
    ports:
      - "8080:8080"   # Proxy
      - "9090:9090"   # Dashboard UI + Admin API
    volumes:
      - ./gateway.yaml:/etc/lightlayer/gateway.yaml
      - gateway-data:/var/lib/lightlayer  # SQLite DB for analytics/config
    environment:
      - LIGHTLAYER_CONFIG=/etc/lightlayer/gateway.yaml

volumes:
  gateway-data:
```

No external databases, no Redis, no message queues. One container, one volume. SQLite handles storage.

## Implementation Phases

### Phase 1: Core Proxy (Cycles 1-5)
- Go module init, project scaffolding, CI
- Config structs + YAML loader + validation
- Reverse proxy engine (httputil.ReverseProxy + custom transport)
- CLI commands (cobra): init, start, validate
- Proxy edge cases: error handling, timeouts, streaming, graceful shutdown

### Phase 2: Discovery & Identity Plugins (Cycles 6-10)
- Plugin interface + pipeline builder
- Discovery plugin (well-known endpoints)
- Agent detection + Identity plugin (JWT verification, modes)
- Rate limiting plugin (sliding window, per-agent)
- Security plugin (CORS, headers, robots.txt)

### Phase 3: Payments, Analytics & Admin API (Cycles 11-15)
- x402 payment plugin
- Analytics plugin (JSONL logging, async, SQLite storage)
- Admin REST API (health, metrics, agents, config CRUD)
- Hot reload (SIGHUP + file watcher)
- SQLite store for analytics data and config persistence

### Phase 4: Dashboard UI (Cycles 16-18)
- React + Vite + Tailwind + shadcn/ui scaffolding
- Dashboard pages: Overview, Analytics, Plugins, Settings, Logs
- Admin API integration, WebSocket for live logs
- Embed built UI in Go binary via `embed`

### Phase 5: Polish & Distribution (Cycles 19-20)
- Docker + docker-compose, integration tests
- README, examples, BSL 1.1 license, final audit

## Success Metrics

- `docker compose up` → working gateway + dashboard in < 30 seconds
- `lightlayer-gateway init && lightlayer-gateway start` → working gateway in < 5 seconds
- < 2ms latency overhead per request
- Single binary (with embedded UI) under 25MB
- Docker image under 30MB
- Dashboard loads in < 1 second
- Zero external dependencies for self-hosted (no Redis, no Postgres — just SQLite)
- Zero-config discovery from YAML description
