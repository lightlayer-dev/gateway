# Agent Onboarding Plugin — Architecture

## Problem

AI agents can't sign up for API access programmatically. Today, a human must:
1. Visit a website, create an account
2. Navigate to a settings page
3. Generate an API key or OAuth client
4. Copy credentials into the agent's config

This breaks the promise of agent-native APIs. If an agent discovers an API through our gateway (via /.well-known/agent.json, /llms.txt, etc.), it should be able to **register and get credentials without human intervention**.

## Solution

The gateway exposes a single standardized registration endpoint (`POST /agent/register`) that all agents learn once. The API owner implements one webhook that provisions credentials using whatever auth system they already have (Auth0, Supabase, Firebase, homegrown — doesn't matter).

The gateway never stores credentials. It's a facilitator.

## Flow

```
Agent                          Gateway                         API Owner's Webhook
  │                               │                                    │
  │  1. POST /agent/register      │                                    │
  │  { identity_token: "..." }    │                                    │
  │──────────────────────────────▶│                                    │
  │                               │  2. Verify identity token          │
  │                               │     (JWT signature check)          │
  │                               │                                    │
  │                               │  3. POST /internal/provision-agent │
  │                               │  { agent_id, name, provider,      │
  │                               │    verified: true }                │
  │                               │───────────────────────────────────▶│
  │                               │                                    │
  │                               │                                    │  4. Create account/key
  │                               │                                    │     in their auth system
  │                               │                                    │
  │                               │  5. { credentials: { ... } }       │
  │                               │◀───────────────────────────────────│
  │                               │                                    │
  │  6. { credentials: { ... } }  │                                    │
  │◀──────────────────────────────│                                    │
  │                               │                                    │
  │  7. Future requests use       │                                    │
  │     credentials directly      │                                    │
  │──────────────────────────────▶│───────────────────────────────────▶│
```

## Agent-Facing Interface

### Registration

```
POST /agent/register
Content-Type: application/json

{
  "identity_token": "eyJ...",      // Optional: signed JWT proving agent identity
  "agent_id": "claude-bot-xyz",     // Agent's self-identified ID
  "agent_name": "ClaudeBot",        // Human-readable name
  "agent_provider": "Anthropic",    // Who operates this agent
  "metadata": {}                    // Optional: any extra info
}
```

### Registration Response

The gateway returns credentials in a standardized format. Three types:

**API Key:**
```json
{
  "status": "provisioned",
  "credentials": {
    "type": "api_key",
    "token": "sk_live_abc123",
    "header": "X-API-Key",
    "expires_at": null
  }
}
```

**OAuth2 Client Credentials:**
```json
{
  "status": "provisioned",
  "credentials": {
    "type": "oauth2_client_credentials",
    "client_id": "agent_xyz",
    "client_secret": "secret_abc",
    "token_endpoint": "https://api.example.com/oauth/token",
    "scopes": ["read", "write"]
  }
}
```

**Bearer Token with Refresh:**
```json
{
  "status": "provisioned",
  "credentials": {
    "type": "bearer",
    "access_token": "eyJ...",
    "refresh_token": "rt_abc123",
    "token_endpoint": "https://api.example.com/oauth/token",
    "expires_in": 3600
  }
}
```

### Auth Required Response

When an unauthenticated agent hits the API:

```
HTTP 401
Content-Type: application/json

{
  "error": "auth_required",
  "message": "This API requires authentication. Register to get credentials.",
  "register_url": "/agent/register",
  "auth_docs": "https://docs.example.com/auth",
  "supported_credential_types": ["api_key", "oauth2_client_credentials", "bearer"]
}
```

This tells the agent exactly what to do — no guessing, no reading docs, no human help.

## API Owner Configuration

### Minimal (webhook only):
```yaml
plugins:
  agent_onboarding:
    enabled: true
    provisioning_webhook: https://api.example.com/internal/provision-agent
```

### Full:
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

### Webhook Request (Gateway → API Owner):

```
POST /internal/provision-agent
Content-Type: application/json
X-Webhook-Signature: sha256=abc123...   # HMAC-SHA256 of body using webhook_secret

{
  "agent_id": "claude-bot-xyz",
  "agent_name": "ClaudeBot",
  "agent_provider": "Anthropic",
  "identity_verified": true,
  "request_ip": "1.2.3.4",
  "timestamp": "2026-03-28T18:00:00Z"
}
```

### Webhook Response (API Owner → Gateway):

```json
{
  "status": "provisioned",
  "credentials": {
    "type": "api_key",
    "token": "sk_live_abc123",
    "header": "X-API-Key",
    "expires_at": "2027-03-28T00:00:00Z"
  }
}
```

Or rejection:
```json
{
  "status": "rejected",
  "reason": "Agent provider not allowed"
}
```

## Payments Bridge — x402 to Origin Billing

The gateway bridges x402 payments with the origin's own billing system. The API owner never touches crypto. The agent never touches Stripe. The gateway is the adapter.

### Flow

```
Agent                          Gateway                         Origin API
  │                               │                                │
  │  1. GET /api/data             │                                │
  │  (free tier credentials)      │                                │
  │──────────────────────────────▶│──────────────────────────────▶ │
  │                               │                                │
  │                               │  2. 429 Too Many Requests      │
  │                               │     (quota exceeded)           │
  │                               │◀──────────────────────────────│
  │                               │                                │
  │  3. 402 Payment Required      │                                │
  │  (x402 payment info)          │                                │
  │◀──────────────────────────────│                                │
  │                               │                                │
  │  4. Pay via x402 (crypto)     │                                │
  │  Payment-Signature header     │                                │
  │──────────────────────────────▶│                                │
  │                               │  5. Verify payment with        │
  │                               │     x402 facilitator           │
  │                               │                                │
  │                               │  6. POST billing_webhook       │
  │                               │  { agent_id, amount, currency, │
  │                               │    tx_hash, timestamp }        │
  │                               │──────────────────────────────▶ │
  │                               │                                │
  │                               │                                │  7. Update agent quota/tier
  │                               │                                │     (Stripe, DB, whatever)
  │                               │                                │
  │                               │  8. Retry original request     │
  │                               │──────────────────────────────▶ │
  │                               │                                │
  │  9. 200 OK                    │  ◀──────────────────────────── │
  │◀──────────────────────────────│                                │
```

### Configuration

```yaml
plugins:
  payments:
    enabled: true
    facilitator: https://x402.org/facilitator
    pay_to: "0xYourWalletAddress"
    billing_webhook: https://example.com/api/agent-payment
    billing_webhook_secret: ${BILLING_WEBHOOK_SECRET}
    billing_webhook_timeout: 10s
    routes:
      - path: /api/premium/*
        price: "0.01"
        currency: USDC
        description: "Premium API access"
```

### Billing Webhook Request (Gateway → Origin)

```
POST /api/agent-payment
Content-Type: application/json
X-Webhook-Signature: sha256=abc123...

{
  "agent_id": "claude-bot-xyz",
  "amount": "0.01",
  "currency": "USDC",
  "tx_hash": "0xTX123...",
  "network": "eip155:8453",
  "timestamp": "2026-03-28T18:00:00Z"
}
```

The origin updates the agent's quota/tier in their own system and returns 200 OK. The gateway then retries the original request.

### Why This Matters

- **API owner never touches crypto** — they receive a webhook with payment details and update their billing system
- **Agent never touches Stripe** — they pay with x402 (crypto) and the gateway handles the conversion
- **Gateway is the adapter** — bridges two incompatible payment systems

### Future: Fiat x402

Agent wallets are emerging (Coinbase AgentKit, Crossmint) but still early. Future possibility: fiat x402 where agent owners pre-fund a balance via credit card, and x402 deducts from that balance instead of on-chain payment. This would remove the crypto requirement entirely while keeping the same protocol.

## What Stays

- **Discovery** — how agents find the API. Now also advertises the registration endpoint.
- **Payments (x402)** — orthogonal to auth. Bridges crypto payments to origin billing.
- **Analytics** — tracks registrations as events.

## Discovery Integration

The agent onboarding info should be included in discovery endpoints so agents know registration is available:

**/.well-known/agent.json (A2A Agent Card):**
```json
{
  "authentication": {
    "type": "agent_onboarding",
    "registration_url": "/agent/register",
    "supported_credential_types": ["api_key", "oauth2_client_credentials", "bearer"]
  }
}
```

**/llms.txt:**
```
## Authentication
This API supports agent self-registration.
Register at: /agent/register
```

## Implementation

### Files:
```
internal/plugins/onboarding/
├── ARCHITECTURE.md     # This file
├── onboarding.go       # Plugin main: registration endpoint, webhook caller
├── webhook.go          # Webhook HTTP client, HMAC signing, timeout
├── types.go            # Request/response types, credential types
└── onboarding_test.go  # Tests
```

### Key Decisions:
- **No credential storage** — gateway is stateless for credentials
- **Webhook-based** — API owner has full control, we don't couple to any auth provider
- **Standardized credential format** — three types cover 99% of auth patterns
- **HMAC webhook signing** — API owner can verify the request came from their gateway
- **Rate limiting on registration** — prevent abuse
- **Identity verification is optional** — because most agent providers don't issue identity tokens yet. When they do, API owners can require it.
