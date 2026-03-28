# LightLayer Gateway — Design Document

## Vision

A standalone reverse proxy that sits between AI agents and APIs. Zero code changes for the API owner. Configure via YAML, point agent traffic through us, and we handle identity verification, payment negotiation, discovery serving, rate limiting, and analytics — automatically.

Think Cloudflare, but specifically for AI agent traffic.

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

- **Language:** Go 1.22+
- **Proxy:** net/http + httputil.ReverseProxy
- **Config:** gopkg.in/yaml.v3
- **CLI:** cobra (standard Go CLI framework)
- **Validation:** go-playground/validator or custom
- **JWT:** golang-jwt/jwt/v5
- **Logging:** slog (stdlib, structured)
- **Testing:** stdlib testing + testify

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
│   └── admin/
│       └── admin.go             # Admin API server
├── configs/
│   └── gateway.yaml             # Default config template
├── go.mod
├── go.sum
├── Dockerfile
├── Makefile
├── .github/
│   └── workflows/
│       └── ci.yml
├── DESIGN.md
├── README.md
└── LICENSE
```

## Distribution

1. **Binary releases** — GitHub Releases with binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64
2. **Docker** — `docker run ghcr.io/lightlayer-dev/gateway`
3. **Homebrew** — `brew install lightlayer/tap/gateway`
4. **Go install** — `go install github.com/lightlayer-dev/gateway/cmd/gateway@latest`

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

### Phase 3: Payments & Analytics (Cycles 11-15)
- x402 payment plugin
- Analytics plugin (JSONL logging, async, remote endpoint batching)
- Admin API (health, metrics, agents, config)
- Hot reload (SIGHUP + file watcher)
- Dashboard integration

### Phase 4: Polish & Distribution (Cycles 16-20)
- CLI polish (interactive init, test command, status)
- Dockerfile + multi-arch builds
- Integration tests (end-to-end)
- README + examples + quickstart
- Performance benchmarks + final audit

## Success Metrics

- `lightlayer-gateway init && lightlayer-gateway start` works in < 5 seconds
- < 2ms latency overhead per request
- Single binary under 15MB
- Docker image under 15MB (distroless)
- Zero-config discovery from YAML description
