# LightLayer Gateway

[![CI](https://github.com/lightlayer-dev/gateway/actions/workflows/ci.yml/badge.svg)](https://github.com/lightlayer-dev/gateway/actions/workflows/ci.yml)
![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)

A standalone reverse proxy for AI agent traffic. Zero code changes for the API owner — configure via YAML, point agent traffic through the gateway, and it handles identity verification, payment negotiation, discovery serving, rate limiting, and analytics automatically.

## Quick Start

```bash
go install github.com/lightlayer-dev/gateway/cmd/gateway@latest

lightlayer-gateway init
lightlayer-gateway start
```

## Development

```bash
make build    # Build binary
make test     # Run tests
make lint     # Run go vet
make run      # Build and start
```

## Architecture

See [DESIGN.md](DESIGN.md) for full design document.
