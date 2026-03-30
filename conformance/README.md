# Agent-Layer Conformance Test Suite

Language-agnostic HTTP conformance tests for any server implementing the LightLayer agent-layer spec. Works with agent-layer-ts, agent-layer-python, agent-layer-go, or the LightLayer Gateway — anything that serves the agent-layer endpoints.

## Quick Start

```bash
cd conformance
npm install

# Run against a local server
BASE_URL=http://localhost:8080 npm test

# Run against any server
BASE_URL=https://api.example.com npm test
```

## What It Tests

| Test File | Endpoints | Assertions |
|-----------|-----------|------------|
| `discovery.test.ts` | `GET /agents.txt`, `/llms.txt`, `/llms-full.txt` | Format, content-type `text/plain`, required directives |
| `well-known.test.ts` | `GET /.well-known/ai`, `/ai/json-ld`, `/agent.json` | JSON structure, A2A Agent Card schema, JSON-LD `@context` |
| `openapi.test.ts` | `GET /openapi.json` | OpenAPI structure, version field, info, paths |
| `mcp.test.ts` | `POST /.well-known/mcp` | JSON-RPC initialize, tools/list, tools/call, error handling |
| `rate-limit.test.ts` | Burst requests | `X-RateLimit-*` headers, 429 responses, Retry-After |
| `errors.test.ts` | `GET /nonexistent` | Structured error envelope, content negotiation (JSON vs HTML) |
| `agent-detection.test.ts` | Various User-Agents | 16 known AI agents detected, browser UAs get HTML |
| `oauth2.test.ts` | `GET /.well-known/oauth-authorization-server` | RFC 8414 metadata structure |
| `ag-ui.test.ts` | `POST /ag-ui` (SSE) | `text/event-stream`, Cache-Control, event format |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `BASE_URL` | `http://localhost:8080` | Server under test |
| `AG_UI_PATH` | `/ag-ui` | Path to the AG-UI SSE endpoint |

## CI Usage

```yaml
# GitHub Actions example
- name: Run conformance tests
  working-directory: conformance
  env:
    BASE_URL: http://localhost:8080
  run: |
    npm ci
    npm test
```

## Design Principles

1. **Black-box testing** — tests only use HTTP, no language-specific imports
2. **Graceful degradation** — optional features (OAuth2, AG-UI) skip cleanly when not configured
3. **No server dependency** — point `BASE_URL` at any running server
4. **CI-friendly** — exit code 0 on pass, non-zero on failure, verbose output
