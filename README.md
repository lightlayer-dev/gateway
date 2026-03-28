# LightLayer Gateway

[![CI](https://github.com/lightlayer-dev/gateway/actions/workflows/ci.yml/badge.svg)](https://github.com/lightlayer-dev/gateway/actions/workflows/ci.yml)
![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-BSL_1.1-blue)

**A reverse proxy that makes any API agent-ready.** Zero code changes — put it in front of your API and agents can discover it, onboard themselves, pay for access, and see what's happening.

**Find the API. Register. Pay. See what's happening.** — the full agent lifecycle, handled by the gateway.

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
| **Discovery** | Serves `/llms.txt`, `/llms-full.txt`, `/.well-known/agent.json` (A2A Agent Card), `/agents.txt` — so agents can find and understand your API |
| **Onboarding** | Agents self-register via `POST /agent/register` and get credentials back — no human intervention |
| **Payments** | x402 micropayments with billing webhook bridge — agents pay crypto, your origin gets a billing event. **The bridge between crypto rails and your existing billing system.** |
| **Analytics** | Agent traffic logging with SQLite storage and dashboard charts |

<!-- Screenshot placeholder: ![Dashboard](docs/dashboard.png) -->

---

## Architecture

```
┌─────────────┐     ┌──────────────────────────┐     ┌──────────────┐
│   AI Agent   │────▶│   LightLayer Gateway     │────▶│  Origin API  │
│  (Claude,    │     │                          │     │  (any lang,  │
│   GPT, etc.) │◀────│  Discovery → Onboarding  │◀────│   any stack) │
└─────────────┘     │  → Payments → Analytics   │     └──────────────┘
                    │  → Reverse Proxy          │
                    │                          │
                    │  Dashboard UI (port 9090) │
                    └──────────────────────────┘
```

The plugin pipeline executes in order: **Discovery → Agent Onboarding → Payments → Analytics → Proxy**.

Single binary. Single container. No external databases — SQLite handles everything.

---

## Agent Payments (x402)

The payments plugin is the core of the gateway's monetization story. It bridges x402 crypto micropayments with the origin's own billing system. **The API owner never touches crypto. The agent never touches Stripe. The gateway is the adapter.**

### Full Agent Lifecycle

```
Discovery → Onboarding → Free Tier → Payment → Paid Tier
    │            │            │           │          │
    ▼            ▼            ▼           ▼          ▼
 Agent finds  Agent gets  Agent uses  Agent pays  Origin updates
 the API via  credentials  free quota  via x402    agent's tier
 /llms.txt    via POST    until 429   (crypto)   via billing
 /agent.json  /agent/      from                    webhook
              register     origin
```

### How the Billing Bridge Works

1. Agent uses free tier credentials, origin returns **429** (quota exceeded)
2. Gateway intercepts the 429, returns **402** with x402 payment info
3. Agent pays via x402 (crypto)
4. Gateway verifies payment with x402 facilitator
5. Gateway calls the origin's **billing webhook** with payment details
6. Origin updates the agent's quota/tier in their own system (Stripe, DB, whatever)
7. Gateway retries the original request

This means any API with an existing billing system can accept agent payments without adopting crypto infrastructure. The gateway handles the translation.

### Payments Configuration

```yaml
plugins:
  payments:
    enabled: true
    facilitator: https://x402.org/facilitator
    pay_to: "0xYourWalletAddress"
    billing_webhook: https://api.example.com/api/agent-payment
    billing_webhook_secret: ${BILLING_WEBHOOK_SECRET}
    billing_webhook_timeout: 10s
    routes:
      - path: /api/premium/*
        price: "0.01"
        currency: USDC
        description: "Premium API access"
```

The billing webhook receives a POST with `{ agent_id, amount, currency, tx_hash, network, timestamp }` signed with HMAC-SHA256. The origin processes the payment in their own billing system and returns 200 OK.

---

## Agent Onboarding

Agent onboarding lets AI agents register for API credentials programmatically — no human intervention needed. When an agent discovers your API via `/.well-known/agent.json` or `/llms.txt`, it can `POST /agent/register` to get credentials back instantly.

### How It Works

1. Agent sends `POST /agent/register` with its identity
2. Gateway forwards the request to your **provisioning webhook**
3. Your webhook creates credentials in your auth system (Auth0, Supabase, Firebase, etc.)
4. Gateway returns the credentials to the agent
5. Agent uses credentials for all subsequent requests

The gateway never stores credentials — it's a stateless facilitator.

### Onboarding Configuration

```yaml
plugins:
  agent_onboarding:
    enabled: true
    provisioning_webhook: https://api.example.com/internal/provision-agent
    webhook_secret: ${WEBHOOK_SECRET}    # HMAC-SHA256 signature for webhook calls
    webhook_timeout: 10s
    require_identity: false               # If true, agent must present a signed JWT
    rate_limit:
      max_registrations: 10              # Per IP per hour
      window: 1h
    allowed_providers: []                # Empty = allow all. ["Anthropic", "OpenAI"] to restrict
```

### Example Webhook Implementation

Your webhook receives a POST from the gateway and returns credentials:

```python
# FastAPI example
@app.post("/internal/provision-agent")
async def provision_agent(request: Request):
    body = await request.json()

    # Verify HMAC signature (optional)
    signature = request.headers.get("X-Webhook-Signature")
    # ... verify with your webhook_secret

    # Create credentials in your auth system
    api_key = create_api_key(
        agent_id=body["agent_id"],
        agent_name=body["agent_name"],
        provider=body["agent_provider"],
    )

    return {
        "status": "provisioned",
        "credentials": {
            "type": "api_key",
            "token": api_key,
            "header": "X-API-Key",
            "expires_at": "2027-01-01T00:00:00Z"
        }
    }
```

### Unauthenticated Request Handling

When an agent hits the API without credentials, the gateway returns a helpful 401:

```json
{
  "error": "auth_required",
  "message": "This API requires authentication. Register to get credentials.",
  "register_url": "/agent/register",
  "supported_credential_types": ["api_key", "oauth2_client_credentials", "bearer"]
}
```

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

  # Agent onboarding — self-registration via webhook
  agent_onboarding:
    enabled: false
    # provisioning_webhook: https://api.example.com/internal/provision-agent
    # webhook_secret: ${WEBHOOK_SECRET}
    # webhook_timeout: 10s
    # require_identity: false
    # allowed_providers: []       # Empty = allow all
    # rate_limit:
    #   max_registrations: 10     # Per IP per hour
    #   window: 1h

  # x402 micropayments — the bridge between crypto and your billing system
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
    #     description: "Premium API access"

  # Traffic analytics
  analytics:
    enabled: true
    log_file: ./agent-traffic.log
    # db_path: ./analytics.db    # SQLite for dashboard charts
    # endpoint: https://your-endpoint.com/events

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

Everything else (plugins, TLS, payments, etc.) goes in `gateway.yaml`. For Docker, mount the config file.

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

1. **Discovery** — Intercepts `/llms.txt`, `/llms-full.txt`, `/.well-known/agent.json`, `/agents.txt`
2. **Agent Onboarding** — Handles `POST /agent/register`, returns 401 with registration info for unauthenticated requests
3. **Payments** — x402 payment negotiation and billing webhook bridge
4. **Analytics** — Logs request (async, non-blocking)
5. **→ Reverse Proxy → Origin**

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
Agent-Readiness Score: 34/100 (D)
   https://api.example.com — 1250ms

  [FAIL] Agent Discovery Endpoints (0/10)
     No agent discovery endpoints found

  [FAIL] llms.txt (0/10)
     No /llms.txt found

  [PASS] Content-Type Headers (10/10)
     Content-Type header present with charset

  [WARN] Rate Limit Headers (4/10)
     Some rate limit headers present

Quick wins to improve your score:
   - Serve /.well-known/agent.json and /agents.txt
   - Add /llms.txt with structured markdown
   - Include X-RateLimit-Remaining and Reset headers

With LightLayer Gateway, your score would be: 89/100 (B) (+55)
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
- [ ] Configure `analytics.db_path` for persistent analytics
- [ ] Set up log rotation for `analytics.log_file`
- [ ] Mount a persistent volume for SQLite data

---

## Performance

Benchmarks run on a single-core VM (DO-Premium-Intel). Proxy latency target: <2ms.

| Benchmark | Latency | Allocs/op |
|-----------|---------|-----------|
| Bare proxy (no plugins) | ~120 us | 105 |
| All plugins enabled | ~19 us | 40 |
| 1000 concurrent requests | ~25 us/req | 55 |
| **Proxy latency overhead** | **~22 us (0.022 ms)** | 51 |

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
