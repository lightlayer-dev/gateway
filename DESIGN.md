# LightLayer Gateway — Design Document

## Vision

A standalone reverse proxy that sits between AI agents and APIs. Zero code changes for the API owner. Configure via YAML or dashboard, point agent traffic through us, and we handle identity verification, payment negotiation, discovery serving, rate limiting, and analytics — automatically.

Think Cloudflare, but specifically for AI agent traffic.

## Why This Exists

Our middleware libraries (agent-layer-ts, agent-layer-python) require developers to integrate code into their apps. That works for some teams, but:

1. **Many API owners don't want to change code** — especially enterprise teams with frozen deploys
2. **Not everyone uses Node or Python** — Go, Java, Rust teams are locked out
3. **Middleware is hard to monetize** — open-source libraries don't generate revenue
4. **A proxy is a service** — clear value prop, clear pricing (per-request, per-agent, tiered)

The gateway uses our existing agent-layer-ts modules internally, so all the battle-tested logic (identity, payments, discovery, etc.) is reused.

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

1. **Proxy Engine** — HTTP reverse proxy (http-proxy or custom) that forwards requests to the origin
2. **Plugin Pipeline** — ordered middleware chain that processes requests/responses
3. **Config Loader** — reads YAML config, validates, hot-reloads on change
4. **Admin API** — REST API for runtime config changes, health checks, metrics
5. **CLI** — `lightlayer-gateway` command for init, start, validate, etc.

### Technology Stack

- **Runtime:** Node.js (Bun-compatible)
- **Language:** TypeScript
- **Proxy:** `http-proxy` or raw `http.createServer` + fetch forwarding
- **Config:** YAML (primary), JSON (supported), environment variables (overrides)
- **Packaging:** Single binary via `pkg` or Docker image, also `npx`-runnable

## Configuration Design

Inspired by: Cloudflare Workers (`wrangler.toml`), Caddy (`Caddyfile`), Traefik (`traefik.yml`), Kong (`kong.yml`).

The config should feel immediately familiar to anyone who's used a reverse proxy.

### `gateway.yaml` — The Primary Config File

```yaml
# LightLayer Gateway Configuration
gateway:
  # Where the gateway listens
  listen:
    port: 8080
    host: 0.0.0.0
    # tls:
    #   cert: /path/to/cert.pem
    #   key: /path/to/key.pem

  # Where to forward requests (your API)
  origin:
    url: https://api.example.com
    # timeout: 30s
    # retries: 2
    # headers:
    #   X-Forwarded-By: lightlayer-gateway

# What the gateway does to agent traffic
plugins:
  # Serve discovery endpoints automatically
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

  # Verify agent identity before forwarding
  identity:
    enabled: true
    # How strict — "log" (observe only), "warn" (header but forward), "enforce" (reject unverified)
    mode: enforce
    # trusted_issuers:
    #   - https://auth.anthropic.com
    #   - https://auth.openai.com

  # x402 payment handling
  payments:
    enabled: false
    # facilitator: https://x402.org/facilitator
    # routes:
    #   - path: /api/premium/*
    #     price: "$0.01"
    #     currency: USDC
    #     network: base

  # Per-agent rate limiting
  rate_limits:
    enabled: true
    default:
      requests: 100
      window: 1m
    # per_agent:
    #   claude: { requests: 500, window: 1m }
    #   gpt: { requests: 200, window: 1m }

  # Agent traffic analytics
  analytics:
    enabled: true
    # Where to send events
    # endpoint: https://dashboard.lightlayer.dev/api/events
    # api_key: your-key-here
    # Or log locally
    log_file: ./agent-traffic.log

  # Security headers for agent responses
  security:
    enabled: true
    # cors_origins: ["*"]
    # robots_txt: true (auto-serve robots.txt with agent permissions)

# Optional: Admin API for runtime changes
admin:
  enabled: true
  port: 9090
  # auth_token: your-secret-token
```

### Design Principles for Config

1. **Sensible defaults** — `lightlayer-gateway init` generates a working config with comments explaining every option
2. **Progressive disclosure** — start with 5 lines (listen + origin), add plugins as needed
3. **No required plugins** — bare proxy works out of the box, plugins are opt-in
4. **Comments as docs** — the config file itself teaches you how to use it
5. **Environment variable overrides** — `LIGHTLAYER_ORIGIN_URL=https://api.example.com` overrides `origin.url`
6. **Hot reload** — change the YAML, gateway picks it up without restart (SIGHUP or file watch)

## CLI Design

Inspired by: `wrangler` (Cloudflare), `caddy`, `docker`.

```bash
# Initialize a new gateway config
lightlayer-gateway init
# → Creates gateway.yaml with sensible defaults and helpful comments

# Validate config
lightlayer-gateway validate
# → Checks config syntax, warns about common mistakes

# Start the gateway
lightlayer-gateway start
# → Starts proxy, prints URL, shows active plugins

# Start with a specific config
lightlayer-gateway start --config ./production.yaml

# Start in dev mode (verbose logging, auto-reload)
lightlayer-gateway dev

# Check status
lightlayer-gateway status
# → Shows uptime, request count, active plugins, origin health

# Test a specific route
lightlayer-gateway test /api/widgets
# → Sends a test request through the gateway, shows what each plugin did
```

### CLI Output Design

Startup should look clean and informative:

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

Each plugin is a middleware function with a standard interface:

```typescript
interface GatewayPlugin {
  name: string;
  // Called once on startup
  init(config: PluginConfig): Promise<void>;
  // Called for every request (before forwarding to origin)
  onRequest?(ctx: RequestContext): Promise<RequestAction>;
  // Called for every response (before sending to agent)
  onResponse?(ctx: ResponseContext): Promise<void>;
  // Called on shutdown
  destroy?(): Promise<void>;
}

type RequestAction =
  | { type: 'forward' }                    // Continue to origin
  | { type: 'respond'; response: Response } // Short-circuit (e.g., serve discovery)
  | { type: 'reject'; status: number; body: string }; // Block request
```

### Plugin Execution Order

1. **Security** — CORS, security headers (always first)
2. **Discovery** — intercept /.well-known/*, /agents.txt, /llms.txt (short-circuit, never hits origin)
3. **Identity** — verify agent credentials
4. **Rate Limits** — check/enforce per-agent limits
5. **Payments** — handle x402 negotiation
6. **Analytics** — log the request (non-blocking, async)
7. **→ Forward to origin →**
8. **Analytics** — log the response (non-blocking, async)

## Key Design Decisions

### 1. Agent Detection

How does the gateway know a request is from an AI agent vs. a human browser?

- **User-Agent header** — most agents identify themselves (ClaudeBot, GPT-4, etc.)
- **Agent-Identity header** — IETF draft standard for agent auth
- **Known agent IP ranges** — optional, less reliable
- **Request patterns** — API-style requests without cookies/sessions
- **Default:** treat all traffic as potentially agent traffic, let plugins decide

### 2. Non-Agent Traffic

What happens to regular human traffic?

- **Pass-through** — forward to origin unchanged, no plugin processing
- **Configurable** — `agent_only: true` to only intercept detected agent requests
- **Analytics still works** — can log all traffic, tag agent vs. human

### 3. Docker-First Distribution

Primary distribution is Docker:

```bash
docker run -v ./gateway.yaml:/etc/lightlayer/gateway.yaml -p 8080:8080 lightlayer/gateway
```

Also: npm (`npx lightlayer-gateway`), standalone binary.

### 4. Dashboard Integration

The gateway can report to the LightLayer Dashboard:

```yaml
analytics:
  endpoint: https://dashboard.lightlayer.dev/api/events
  api_key: your-key
```

This is the monetization path — gateway is free/open-source, dashboard analytics is the paid tier.

## Implementation Phases

### Phase 1: Core Proxy (Cycles 1-5)
- Project scaffolding (TypeScript, ESLint, Vitest, CI)
- Config loader + YAML parser + validation
- HTTP reverse proxy engine
- CLI commands: init, start, validate
- Basic request/response logging
- Tests for proxy correctness (headers, status codes, streaming, WebSocket passthrough)

### Phase 2: Discovery & Identity Plugins (Cycles 6-10)
- Plugin architecture (interface, loading, ordering)
- Discovery plugin (serves /.well-known/ai, /agents.txt, /llms.txt, agent.json from config)
- Identity plugin (JWT verification, SPIFFE ID parsing, log/warn/enforce modes)
- Agent detection module (User-Agent parsing, header inspection)
- Rate limiting plugin (per-agent, sliding window, in-memory + Redis option)
- Security plugin (CORS, security headers, robots.txt)

### Phase 3: Payments & Analytics (Cycles 11-15)
- x402 payment plugin (402 responses, payment verification, facilitator integration)
- Analytics plugin (structured event logging, async batching)
- Dashboard integration (report events to remote endpoint)
- Admin API (health, metrics, runtime config)
- Hot config reload (file watch + SIGHUP)

### Phase 4: Polish & Distribution (Cycles 16-20)
- CLI polish (init with interactive prompts, test command, status)
- Docker image + Dockerfile
- Comprehensive README with quickstart guide
- npm package (`npx lightlayer-gateway`)
- Integration tests (end-to-end proxy tests with real HTTP servers)
- Performance benchmarks (latency overhead measurement)
- Dev mode (verbose logging, auto-reload, request inspector)
- Error handling (graceful degradation when origin is down, plugin failures don't crash proxy)

## File Structure

```
gateway/
├── src/
│   ├── index.ts              # Entry point
│   ├── cli/
│   │   ├── index.ts          # CLI command router
│   │   ├── init.ts           # lightlayer-gateway init
│   │   ├── start.ts          # lightlayer-gateway start
│   │   ├── validate.ts       # lightlayer-gateway validate
│   │   ├── status.ts         # lightlayer-gateway status
│   │   └── test.ts           # lightlayer-gateway test
│   ├── config/
│   │   ├── loader.ts         # YAML/JSON config loader
│   │   ├── schema.ts         # Config validation (Zod)
│   │   ├── defaults.ts       # Default config values
│   │   └── env.ts            # Environment variable overrides
│   ├── proxy/
│   │   ├── engine.ts         # Core reverse proxy
│   │   ├── headers.ts        # Header manipulation
│   │   └── streaming.ts      # Streaming/chunked response support
│   ├── plugins/
│   │   ├── interface.ts      # Plugin interface definition
│   │   ├── pipeline.ts       # Plugin execution pipeline
│   │   ├── discovery.ts      # Discovery endpoint serving
│   │   ├── identity.ts       # Agent identity verification
│   │   ├── rate-limits.ts    # Per-agent rate limiting
│   │   ├── payments.ts       # x402 payment handling
│   │   ├── analytics.ts      # Traffic analytics/logging
│   │   └── security.ts       # CORS, security headers, robots.txt
│   ├── admin/
│   │   ├── server.ts         # Admin API server
│   │   └── routes.ts         # Admin API routes
│   ├── detection/
│   │   └── agent.ts          # Agent detection (User-Agent, headers, patterns)
│   └── utils/
│       ├── logger.ts         # Structured logging
│       └── time.ts           # Duration parsing ("1m", "30s")
├── tests/
│   ├── proxy/
│   ├── plugins/
│   ├── config/
│   ├── cli/
│   └── integration/
├── templates/
│   └── gateway.yaml          # Default config template (generated by `init`)
├── package.json
├── tsconfig.json
├── vitest.config.ts
├── Dockerfile
├── .github/
│   └── workflows/
│       └── ci.yml
└── README.md
```

## Competitive Landscape

| Product | What It Does | Our Edge |
|---------|-------------|----------|
| Kong | General API gateway | Not agent-aware; no identity/payments/discovery |
| Cloudflare | CDN + security | Network-level, not application-level agent intelligence |
| Apigee | API management | Enterprise complexity, no agent-specific features |
| Traefik | Cloud-native proxy | Routing only, no agent middleware |

**Our positioning:** The first gateway purpose-built for AI agent traffic. Not a general API gateway with agent features bolted on — agent-first from day one.

## Success Metrics

- `lightlayer-gateway init && lightlayer-gateway start` works in < 30 seconds
- < 5ms latency overhead per request
- Zero-config discovery serving (generates endpoints from config description)
- One-file config that's self-documenting
- Docker image under 50MB
