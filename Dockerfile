# Stage 1: Build the dashboard UI
FROM node:20-alpine AS ui-builder
WORKDIR /build/ui
COPY ui/package.json ui/package-lock.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

# Stage 2: Build the Go binary
FROM golang:1.22-alpine AS go-builder
RUN apk add --no-cache git
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui-builder /build/ui/dist ./ui/dist
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X github.com/lightlayer-dev/gateway/internal/cli.Version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o /lightlayer-gateway ./cmd/gateway

# Stage 3: Minimal runtime image
FROM gcr.io/distroless/static-debian12
COPY --from=go-builder /lightlayer-gateway /lightlayer-gateway
COPY configs/gateway.yaml /etc/lightlayer/gateway.yaml

EXPOSE 8080 9090

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/lightlayer-gateway", "status", "--config", "/etc/lightlayer/gateway.yaml"]

ENTRYPOINT ["/lightlayer-gateway"]
CMD ["start", "--config", "/etc/lightlayer/gateway.yaml"]
