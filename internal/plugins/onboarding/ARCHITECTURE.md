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

## What Gets Deprecated

The following plugins are deprecated in favor of agent_onboarding:

- **identity** — was verifying agent JWTs at the gateway level. Now agent identity verification is part of onboarding (optional `require_identity` flag). Gateway-level identity gating had no real use case.
- **api_keys** — was managing gateway-issued API keys. Agents should get credentials from the origin, not the gateway. The gateway is a proxy, not an auth provider.
- **oauth2** — was running an OAuth2 server on the gateway. Same problem — the gateway shouldn't be issuing tokens.

These plugins will be marked deprecated with log warnings and removed in v0.3.

## What Stays

- **Discovery** — how agents find the API. Now also advertises the registration endpoint.
- **Rate limiting** — still needed per-agent after they're authenticated.
- **Payments (x402)** — orthogonal to auth.
- **Analytics** — tracks registrations as events.
- **Security** — CORS, headers, robots.txt still needed.
- **A2A** — task protocol, unrelated to auth.
- **MCP** — tool protocol, unrelated to auth.
- **AG-UI** — streaming protocol, unrelated to auth.
- **agents.txt** — access control by agent name, still useful.

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
