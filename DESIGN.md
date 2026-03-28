# LightLayer Gateway — Design Document

## Vision

A standalone reverse proxy with a web dashboard that sits between AI agents and APIs. Zero code changes for the API owner. Configure via a Cloudflare-style web UI or YAML, point agent traffic through us, and we handle identity verification, payment negotiation, discovery serving, rate limiting, and analytics — automatically.

Think Cloudflare, but specifically for AI agent traffic.

## Business Model

- **Now:** Fully self-hosted, open source (BSL 1.1 license — free to use, can't resell as a hosted service)
- **Future:** Hosted service (we run it for you, pay per usage)
- **License:** Business Source License 1.1 — use, modify, self-host freely. Cannot offer as a commercial managed service. Each version converts to Apache 2.0 after 4 years.

## Prior Art — What We Already Built

The gateway is the next evolution of work we already shipped in **agent-layer-ts** and **agent-layer-python** — middleware libraries that add agent-friendliness to existing web frameworks. Everything below was built, tested, and battle-tested. The gateway takes these learnings and moves them from "add to your code" to "put in front of your code."

### Features proven in agent-layer (port ALL of these to the gateway):

1. **Structured Error Envelopes** — consistent JSON error format for agents: `{type, code, message, status, is_retriable, retry_after, param, docs_url}`. Agents need machine-readable errors, not HTML 500 pages.

2. **Agent Detection** — User-Agent pattern matching for 18+ known AI agents: ChatGPT, GPTBot, ClaudeBot, Anthropic, PerplexityBot, Cohere, Bytespider, Amazonbot, Applebot, Meta-ExternalAgent, etc. This is the foundation — the gateway needs to know it's talking to an agent.

3. **Unified Discovery** — single config generates ALL discovery formats simultaneously:
   - `/.well-known/ai` (AI manifest)
   - `/.well-known/agent.json` (Google A2A Agent Card)
   - `/agents.txt` (robots.txt-style permissions for AI agents — per-agent rules, rate limits, preferred interface, auth requirements)
   - `/llms.txt` + `/llms-full.txt` (LLM-oriented documentation)
   This is the killer feature. One YAML config → five machine-readable discovery endpoints.

4. **Agent Identity (IETF draft-klrc-aiagent-auth-00)** — not just JWT verification, but full SPIFFE/WIMSE workload identity:
   - SPIFFE ID parsing (`spiffe://trust-domain/path`)
   - Scoped authorization policies (agent patterns, trust domains, required scopes, path/method matching)
   - Delegated access detection (agent acting on behalf of a user)
   - Audit event generation
   - Three modes: log (observe), warn (log + header), enforce (reject unverified)

5. **x402 Payments** — HTTP-native micropayments per the x402.org spec:
   - Server declares pricing via PaymentRequirements
   - 402 response with PAYMENT-REQUIRED header
   - Client pays and retries with PAYMENT-SIGNATURE
   - Facilitator verification + settlement
   - Per-route pricing config

6. **OAuth2 with PKCE** — full authorization code flow:
   - PKCE code verifier/challenge generation
   - `/.well-known/oauth-authorization-server` discovery endpoint
   - Token exchange, refresh, and validation with scope checking
   - Zero external dependencies (Web Crypto API)

7. **MCP Server** — auto-generate Model Context Protocol tool definitions from API routes:
   - Route metadata → MCP tool definitions (name, description, JSON Schema input)
   - JSON-RPC 2.0 server handling initialize, tools/list, tools/call
   - Enables AI agents to discover and call API endpoints via MCP

8. **AG-UI Protocol** — Server-Sent Events streaming for agent UIs (CopilotKit, Google ADK):
   - Lifecycle events (RUN_STARTED, RUN_FINISHED, RUN_ERROR)
   - Text streaming (TEXT_MESSAGE_START/CONTENT/END)
   - Tool call streaming (TOOL_CALL_START/ARGS/END/RESULT)
   - State management (STATE_SNAPSHOT, STATE_DELTA)

9. **API Key Auth** — simple key-based authentication as an alternative to OAuth2/JWT

10. **Analytics** — agent traffic telemetry with batch flushing:
    - Per-request: agent name, method, path, status, duration, content type, response size
    - Batch flush to endpoint or local callback
    - Agent detection integrated

11. **Security Headers** — HSTS, X-Content-Type-Options, X-Frame-Options, Referrer-Policy, CSP, Permissions-Policy

12. **robots.txt** — AI-agent-aware robots.txt generation with explicit rules for known AI agents

13. **agents.txt** — per-agent access control with rate limits, preferred interface (REST/MCP/GraphQL/A2A), and auth requirements

14. **API Key Auth** — scoped API keys as a simpler alternative to OAuth2/JWT:
    - ScopedApiKey: keyId, companyId, userId, scopes, expiresAt, metadata
    - Pluggable store interface (in-memory for dev, SQLite for production)
    - Key generation, validation, scope checking, expiration

15. **x402 Client Helpers** — client-side payment handling (for agents consuming paid APIs through the gateway):
    - Detect 402 responses, extract PaymentRequired from header
    - Auto-retry with payment via WalletSigner interface
    - Wrap fetch to transparently handle paid APIs

16. **Content Negotiation** — smart error responses based on client type:
    - Detect if client prefers JSON (agents, curl, bots) vs HTML (browsers)
    - Agents get structured JSON error envelopes
    - Browsers get rendered HTML error pages
    - Based on Accept header + User-Agent pattern matching

17. **Agent-Readiness Scoring** (from @agent-layer/score) — Lighthouse-style CLI scanner:
    - Score any API on agent-friendliness (0-100)
    - Checks: discovery endpoints, error format, rate limit headers, auth docs, cost transparency, structured responses
    - Built into gateway CLI: `lightlayer-gateway score https://api.example.com`
    - Shows what the gateway adds to the score

### Key Architecture Decisions from agent-layer:
- **Plugin ordering matters** — security → discovery → identity → rate limits → payments → analytics → proxy (proven in both TS and Python)
- **Agent detection is foundational** — every other plugin depends on knowing if it's an agent and which one
- **Unified discovery config is essential** — maintaining 5 separate discovery configs is a nightmare; one source of truth
- **Three identity modes (log/warn/enforce)** let users adopt gradually
- **x402 is route-scoped** — different prices for different endpoints
- **agents.txt > robots.txt for agents** — robots.txt is for crawlers, agents.txt is for agents (different rules, different semantics)
- **Content negotiation is critical** — agents need JSON, humans need HTML; the gateway must detect and adapt
- **MCP auto-generation from discovery config** — define capabilities once, get MCP tools for free
- **API keys as gateway-level auth** — simpler than OAuth2 for most use cases, gateway manages keys centrally

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
    # hsts_max_age: 31536000
    # frame_options: DENY
    # content_type_options: nosniff
    # referrer_policy: strict-origin-when-cross-origin

  oauth2:
    enabled: false
    # client_id: your-client-id
    # authorization_endpoint: https://auth.example.com/authorize
    # token_endpoint: https://auth.example.com/token
    # scopes:
    #   read: "Read access"
    #   write: "Write access"

  mcp:
    enabled: false
    # name: "My API"
    # version: "1.0.0"
    # instructions: "REST API for widgets"
    # Auto-generates MCP tools from discovery capabilities

  api_keys:
    enabled: false
    # store: sqlite  # sqlite (persistent) or memory (dev only)
    # keys:
    #   - id: key_prod_abc123
    #     scopes: [read, write]
    #     expires_at: 2027-01-01T00:00:00Z
    #     metadata: { company: "Acme Corp" }

  agents_txt:
    enabled: true
    # rules:
    #   - agent: "*"
    #     allow: ["/api/*"]
    #     deny: ["/internal/*"]
    #     rate_limit: { max: 100, window_seconds: 60 }
    #     preferred_interface: rest  # rest | mcp | graphql | a2a
    #   - agent: "ClaudeBot"
    #     allow: ["/api/*", "/docs/*"]
    #     rate_limit: { max: 500, window_seconds: 60 }

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
 ⚡ LightLayer Gateway v0.1.0

  Listening:  http://localhost:8080
  Origin:     https://api.example.com
  Admin:      http://localhost:9090

  Plugins:
    ✓ discovery   serving /.well-known/ai, /agents.txt, /llms.txt
    ✓ identity    enforcing agent verification
    ✓ rate_limits 100 req/min default
    ✓ analytics   logging to ./agent-traffic.log
    ✓ security    CORS + security headers + robots.txt
    ✓ oauth2      PKCE flow + discovery endpoint
    ✓ mcp         MCP tool server (auto-generated from routes)
    ✓ agents_txt  per-agent access control

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

1. **Security** — CORS, security headers, HSTS, CSP
2. **Discovery** — intercept /.well-known/ai, /.well-known/agent.json, /llms.txt, /agents.txt
3. **OAuth2** — intercept /.well-known/oauth-authorization-server, /authorize, /token
4. **MCP** — intercept /mcp endpoint (JSON-RPC 2.0)
5. **Agents.txt** — enforce per-agent path access rules
6. **API Keys** — validate scoped API keys (simpler alternative to JWT)
7. **Identity** — verify agent credentials (JWT/SPIFFE/WIMSE)
8. **Rate Limits** — per-agent rate limiting (sliding window)
9. **Payments** — x402 payment negotiation
10. **Analytics** — log request (non-blocking, async flush)
11. **→ Reverse Proxy → Origin** (with structured error wrapping + content negotiation on failures)

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
│   │   ├── security/
│   │   │   └── security.go      # CORS, security headers, HSTS, CSP
│   │   ├── oauth2/
│   │   │   └── oauth2.go        # OAuth2 PKCE flow + discovery
│   │   ├── mcp/
│   │   │   └── mcp.go           # MCP JSON-RPC server (auto-generated tools)
│   │   ├── agentstxt/
│   │   │   └── agentstxt.go     # agents.txt generation + enforcement
│   │   └── apikeys/
│   │       └── apikeys.go       # Scoped API key auth + management
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
- Discovery plugin (unified: /.well-known/ai, /.well-known/agent.json, /llms.txt — from agent-layer unified-discovery)
- Agent detection (18+ known agents — port from agent-layer analytics.ts patterns)
- Identity plugin (JWT/SPIFFE/WIMSE verification, 3 modes, authz policies — from agent-layer agent-identity.ts)
- Rate limiting plugin (sliding window, per-agent — from agent-layer rate-limit.ts)
- Security plugin (CORS, HSTS, CSP, all security headers — from agent-layer security-headers.ts)
- Structured error envelopes on all gateway errors (from agent-layer errors.ts)

### Phase 3: Payments, Auth, MCP & Admin API (Cycles 11-15)
- x402 payment plugin (route-scoped pricing, facilitator verify/settle — from agent-layer x402.ts)
- agents.txt plugin (per-agent access rules, rate limits, preferred interface — from agent-layer agents-txt.ts)
- OAuth2 plugin (PKCE flow, discovery endpoint — from agent-layer oauth2.ts)
- MCP plugin (auto-generate tools from discovery config, JSON-RPC server — from agent-layer mcp.ts)
- Analytics plugin (JSONL logging, async flush, SQLite storage — from agent-layer analytics.ts)
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
