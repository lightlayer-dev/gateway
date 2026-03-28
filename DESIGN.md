# LightLayer Gateway — Design Document

## Vision

A standalone reverse proxy with a web dashboard that makes any API agent-ready. Zero code changes for the API owner. Configure via a Cloudflare-style web UI or YAML, point agent traffic through us, and we handle the four pillars of the agent lifecycle automatically:

**Find the API -> Register -> Pay -> See what's happening**

1. **Discovery** — agents find and understand your API via standard machine-readable endpoints
2. **Onboarding** — agents self-register and get credentials programmatically
3. **Payments** — x402 micropayments with a billing bridge to the origin's own system
4. **Analytics** — agent traffic telemetry, logging, and dashboard visualizations

Think Cloudflare, but specifically for AI agent traffic.

## Business Model

- **Now:** Fully self-hosted, open source (BSL 1.1 license — free to use, can't resell as a hosted service)
- **Future:** Hosted service (we run it for you, pay per usage)
- **License:** Business Source License 1.1 — use, modify, self-host freely. Cannot offer as a commercial managed service. Each version converts to Apache 2.0 after 4 years.

## Prior Art — What We Already Built

The gateway is the next evolution of work we already shipped in **agent-layer-ts** and **agent-layer-python** — middleware libraries that add agent-friendliness to existing web frameworks. Everything below was built, tested, and battle-tested. The gateway takes these learnings and moves them from "add to your code" to "put in front of your code."

### Features proven in agent-layer (ported to the gateway):

1. **Structured Error Envelopes** — consistent JSON error format for agents: `{type, code, message, status, is_retriable, retry_after, param, docs_url}`. Agents need machine-readable errors, not HTML 500 pages.

2. **Agent Detection** — User-Agent pattern matching for 18+ known AI agents: ChatGPT, GPTBot, ClaudeBot, Anthropic, PerplexityBot, Cohere, Bytespider, Amazonbot, Applebot, Meta-ExternalAgent, etc. This is the foundation — the gateway needs to know it's talking to an agent.

3. **Unified Discovery** — single config generates ALL discovery formats simultaneously:
   - `/.well-known/agent.json` (Google A2A Agent Card — v1.0 spec)
   - `/agents.txt` (served as a static file for AI agents — NOT enforced as access control)
   - `/llms.txt` + `/llms-full.txt` (LLM-oriented documentation)
   This is the killer feature. One YAML config, four machine-readable discovery endpoints.

   **NOTE:** We do NOT serve `/.well-known/ai` — that was our own format nobody uses. Removed.

4. **x402 Payments + Billing Bridge** — HTTP-native micropayments per the x402.org spec, with a billing webhook bridge to the origin's billing system:
   - Server declares pricing via PaymentRequirements
   - 402 response with PAYMENT-REQUIRED header
   - Client pays and retries with PAYMENT-SIGNATURE
   - Facilitator verification + settlement
   - Per-route pricing config
   - **Billing webhook bridge:** on successful payment, gateway calls origin's billing endpoint with `{ agent_id, amount, currency, tx_hash, network, timestamp }` so the origin can update the agent's quota/tier in their own system (Stripe, DB, whatever)
   - **429->402 interception:** when origin returns 429 (quota exceeded), gateway converts to 402 with x402 payment info
   - The API owner never touches crypto. The agent never touches Stripe. Gateway is the adapter.
   - **Future:** fiat x402 where agent owners pre-fund via credit card and x402 deducts from a balance instead of on-chain payment

5. **Analytics** — agent traffic telemetry with batch flushing:
    - Per-request: agent name, method, path, status, duration, content type, response size
    - Batch flush to endpoint or local callback
    - Agent detection integrated

6. **Agent Onboarding** — agent self-registration via webhook-based credential provisioning:
    - `POST /agent/register` endpoint for programmatic agent sign-up
    - Webhook to API owner's provisioning system (HMAC-SHA256 signed)
    - Standardized credential format: api_key, oauth2_client_credentials, bearer
    - 401 response for unauthenticated agents with registration info
    - Per-IP rate limiting on registration
    - Optional identity token verification
    - Provider allow-listing
    - Gateway is stateless — never stores credentials

7. **Content Negotiation** — smart error responses based on client type:
    - Detect if client prefers JSON (agents, curl, bots) vs HTML (browsers)
    - Agents get structured JSON error envelopes
    - Browsers get rendered HTML error pages
    - Based on Accept header + User-Agent pattern matching

8. **x402 Client Helpers** — client-side payment handling (for agents consuming paid APIs through the gateway):
    - Detect 402 responses, extract PaymentRequired from header
    - Auto-retry with payment via WalletSigner interface
    - Wrap fetch to transparently handle paid APIs

### Key Architecture Decisions from agent-layer:
- **Plugin ordering matters** — discovery -> onboarding -> payments -> analytics -> proxy (proven in both TS and Python)
- **Agent detection is foundational** — every other plugin depends on knowing if it's an agent and which one
- **Unified discovery config is essential** — maintaining separate discovery configs is a nightmare; one source of truth
- **x402 alone was insufficient** — the raw x402 protocol handles crypto payments but doesn't bridge to the origin's billing system. The billing webhook bridge solves this: the gateway calls the origin's billing endpoint with payment details so the origin can update quotas/tiers in their own system (Stripe, DB, etc.). Without this bridge, the API owner would need to handle crypto directly.
- **x402 is route-scoped** — different prices for different endpoints
- **agents.txt is served, not enforced** — agents.txt is a discovery file that tells agents about your API's preferences, not an access control mechanism
- **Content negotiation is critical** — agents need JSON, humans need HTML; the gateway must detect and adapt

## Why Go

- **Purpose-built for proxies** — Caddy, Traefik, Kong are all Go. net/http is best-in-class.
- **Single binary** — compile once, distribute everywhere. No runtime dependencies.
- **Performance** — low latency, low memory, excellent concurrency via goroutines.
- **Tiny Docker images** — ~10MB with scratch/distroless base.
- **Industry standard** — this is what infrastructure software is written in.

## Architecture

```
┌─────────────┐     ┌──────────────────────────┐     ┌──────────────┐
│   AI Agent   │────>│   LightLayer Gateway     │────>│  Origin API  │
│  (Claude,    │     │                          │     │  (any lang,  │
│   GPT, etc.) │<────│  ┌─────────┐ ┌────────┐ │<────│   any stack) │
└─────────────┘     │  │Discovery│ │Onboard │ │     └──────────────┘
                    │  │ Serving │ │  +Auth │ │
                    │  └─────────┘ └────────┘ │
                    │  ┌─────────┐ ┌────────┐ │
                    │  │Payment │ │Analytics│ │
                    │  │ Bridge │ │Logging │ │
                    │  └─────────┘ └────────┘ │
                    │                          │
                    │  Dashboard UI (port 9090) │
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
3. **Discovery** — edit API name/description/capabilities, preview generated endpoints (/llms.txt, /llms-full.txt, /.well-known/agent.json, /agents.txt)
4. **Onboarding** — configure agent self-registration, manage providers, view registration activity
5. **Payments** — configure paid routes, prices, see payment history
6. **Plugins** — toggle plugins on/off, configure each one (discovery, onboarding, payments, analytics)
7. **Settings** — origin URL, listen port, TLS, admin settings, export/import YAML config
8. **Logs** — real-time request log viewer with filtering (by agent, path, status, etc.)

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
    # Serves: /.well-known/agent.json, /agents.txt, /llms.txt, /llms-full.txt

  agent_onboarding:
    enabled: false
    # provisioning_webhook: https://api.example.com/internal/provision-agent
    # webhook_secret: ${WEBHOOK_SECRET}
    # webhook_timeout: 10s
    # require_identity: false
    # allowed_providers: []
    # rate_limit:
    #   max_registrations: 10
    #   window: 1h

  payments:
    enabled: false
    # facilitator: https://x402.org/facilitator
    # pay_to: "0xYourWalletAddress"
    # billing_webhook: https://api.example.com/api/agent-payment
    # billing_webhook_secret: ${BILLING_WEBHOOK_SECRET}
    # routes:
    #   - path: /api/premium/*
    #     price: "0.01"
    #     currency: USDC

  analytics:
    enabled: true
    log_file: ./agent-traffic.log
    # db_path: ./analytics.db
    # endpoint: https://dashboard.lightlayer.dev/api/events
    # api_key: your-key

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

# Score an API's agent-readiness (Lighthouse-style)
lightlayer-gateway score https://api.example.com

# Score with verbose output
lightlayer-gateway score https://api.example.com --verbose
```

### Startup Output

```
 LightLayer Gateway v0.1.0

  Listening:  http://localhost:8080
  Origin:     https://api.example.com
  Admin:      http://localhost:9090

  Plugins:
    discovery        serving /.well-known/agent.json, /agents.txt, /llms.txt, /llms-full.txt
    agent_onboarding agent self-registration via webhook
    payments         x402 payment bridge (2 paid routes)
    analytics        logging to ./agent-traffic.log

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
handler := discovery.Middleware()(
    onboarding.Middleware()(
        payments.Middleware()(
            analytics.Middleware()(
                reverseProxy,
            ),
        ),
    ),
)
```

### Plugin Execution Order

1. **Discovery** — intercept /.well-known/agent.json, /llms.txt, /llms-full.txt, /agents.txt
2. **Agent Onboarding** — handle POST /agent/register, return 401 with registration info for unauthenticated requests
3. **Payments** — x402 payment negotiation, 429->402 interception, billing webhook bridge
4. **Analytics** — log request (non-blocking, async flush)
5. **-> Reverse Proxy -> Origin** (with structured error wrapping + content negotiation on failures)

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
│   │   │   └── discovery.go     # Discovery endpoint serving (agent.json, llms.txt, agents.txt)
│   │   ├── onboarding/
│   │   │   ├── onboarding.go    # Agent self-registration + 401 handler
│   │   │   ├── webhook.go       # Webhook HTTP client, HMAC signing
│   │   │   ├── types.go         # Request/response types
│   │   │   └── onboarding_test.go
│   │   ├── payments/
│   │   │   └── payments.go      # x402 payment handling + billing bridge
│   │   └── analytics/
│   │       └── analytics.go     # Traffic analytics (JSONL + SQLite)
│   ├── detection/
│   │   └── agent.go             # Agent User-Agent detection
│   ├── admin/
│   │   ├── admin.go             # Admin/Dashboard API server
│   │   ├── routes.go            # REST API routes for dashboard
│   │   └── websocket.go         # WebSocket for live logs/metrics
│   ├── store/
│   │   ├── store.go             # Storage interface
│   │   └── sqlite.go            # SQLite implementation (config, analytics, sessions)
│   └── score/
│       ├── scanner.go           # Agent-readiness scanner (port from @agent-layer/score)
│       ├── checks.go            # Individual check implementations
│       └── reporter.go          # Score output formatting
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
│   │   │   ├── Discovery.tsx    # Edit API description, preview endpoints
│   │   │   ├── Onboarding.tsx   # Agent self-registration config
│   │   │   ├── Payments.tsx     # Paid routes, payment history
│   │   │   ├── Plugins.tsx      # Toggle/configure plugins
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
├── Dockerfile                   # Multi-stage: build Go + build UI -> single image
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

### Phase 2: Discovery & Onboarding Plugins (Cycles 6-10)
- Plugin interface + pipeline builder
- Discovery plugin (unified: /.well-known/agent.json, /llms.txt, /llms-full.txt, /agents.txt — from agent-layer unified-discovery)
- Agent detection (18+ known agents — port from agent-layer analytics.ts patterns)
- Agent onboarding plugin (self-registration via webhook, credential provisioning)
- Structured error envelopes on all gateway errors (from agent-layer errors.ts)

### Phase 3: Payments & Analytics (Cycles 11-15)
- x402 payment plugin (route-scoped pricing, facilitator verify/settle — from agent-layer x402.ts)
- Analytics plugin (JSONL logging, async flush, SQLite storage — from agent-layer analytics.ts)
- Content negotiation (JSON for agents, HTML for browsers — from agent-layer error-handler.ts)
- Admin REST API (health, metrics, agents, config CRUD)
- Hot reload (SIGHUP + file watcher)
- SQLite store for analytics data and config persistence

### Phase 4: Dashboard UI (Cycles 16-18)
- React + Vite + Tailwind + shadcn/ui scaffolding
- Dashboard pages: Overview, Analytics, Discovery, Onboarding, Payments, Plugins, Settings, Logs
- Admin API integration, WebSocket for live logs
- Embed built UI in Go binary via `embed`

### Phase 5: Polish & Distribution (Cycles 19-20)
- Docker + docker-compose, integration tests
- Agent-readiness score CLI (`lightlayer-gateway score <url>`)
- README, examples, BSL 1.1 license, final audit

## Success Metrics

- `docker compose up` -> working gateway + dashboard in < 30 seconds
- `lightlayer-gateway init && lightlayer-gateway start` -> working gateway in < 5 seconds
- < 2ms latency overhead per request
- Single binary (with embedded UI) under 25MB
- Docker image under 30MB
- Dashboard loads in < 1 second
- Zero external dependencies for self-hosted (no Redis, no Postgres — just SQLite)
- Zero-config discovery from YAML description

---

## Implementation Notes (Cycle 20 — Final Audit)

### What Was Built

All four pillar plugins are implemented and tested:

1. **Discovery** — `internal/plugins/discovery/` (/.well-known/agent.json, /llms.txt, /llms-full.txt, /agents.txt)
2. **Agent Onboarding** — `internal/plugins/onboarding/` (POST /agent/register, webhook provisioning, 401 handler)
3. **Payments** — `internal/plugins/payments/` (x402 route-scoped pricing, facilitator verify, billing webhook bridge)
4. **Analytics** — `internal/plugins/analytics/` (JSONL logging, async flush, SQLite storage)

Supporting infrastructure:

- **Structured error envelopes** — `internal/plugins/errors.go` (AgentErrorEnvelope)
- **Agent detection** — `internal/detection/agent.go` (18+ known agents)
- **Content negotiation** — `internal/plugins/contentneg.go` (JSON for agents, HTML for browsers)
- **Agent-readiness scoring** — `internal/score/` (scanner, checks, reporter) + CLI command
- **Dashboard UI** — `ui/` (React + TypeScript + Vite + Tailwind + shadcn/ui, embedded in binary)

### Architecture Decisions That Changed from Plan

- **Plugin failure is non-fatal:** Plugins that fail Init() are logged and skipped. The proxy always stays up.
- **Panic recovery per-plugin:** `wrapWithRecovery()` in pipeline.go catches panics from individual plugins without crashing the gateway.
- **SQLite graceful degradation:** If the analytics SQLite store fails to open, the gateway logs a warning and runs without storage. Proxy functionality is never blocked by storage errors.
- **Config env var overrides kept minimal:** Only 5 bootstrap-level env vars (port, host, origin URL, config path, admin port). Everything else goes in gateway.yaml. This keeps the mental model simple.
- **Single Go binary with embedded UI:** `ui/dist` is embedded via Go's `embed` package. No separate UI server needed.
- **Pure-Go SQLite (modernc.org/sqlite):** No CGO dependency. Simplifies cross-compilation and Docker images.
- **Four focused plugins instead of ten:** The product was refocused from a broad feature set to four tightly scoped pillars. Discovery serves files (including agents.txt and A2A Agent Card) but does not enforce access control. agents.txt is a discovery file, not a middleware. This reduces surface area and keeps the codebase maintainable.

### Performance Results (Benchmark — Cycle 20)

| Benchmark | Result | Allocs |
|-----------|--------|--------|
| Bare proxy (no plugins) | ~120 us/op | 105 allocs/op |
| All plugins enabled | ~19 us/op | 40 allocs/op |
| 1000 concurrent requests | ~25 us/req | 55 allocs/req |
| Proxy latency overhead | ~22 us (0.022 ms) | 51 allocs/op |

**Latency overhead is 0.022ms — well under the 2ms target.**

Note: "bare proxy" benchmark includes full HTTP round trip to httptest.Server. "All plugins" benchmark measures the complete middleware chain.

### Binary Size

- Single binary with embedded UI: **18 MB** (under 25 MB target)

### Race Detection

- `go test -race ./...` — **zero data races** (fixed a race in TestStartBootsServer where a shared bytes.Buffer was accessed concurrently; replaced with a synchronized buffer wrapper)

### Test Coverage

- 25 packages with test files
- All tests pass
- Integration tests in `internal/integration/`
- Benchmark tests in `internal/proxy/bench_test.go`
