package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/lightlayer-dev/gateway/internal/config"
)

// ErrorResponse is a structured JSON error returned when the proxy cannot
// reach the origin or encounters a transport error.
type ErrorResponse struct {
	Type        string `json:"type"`
	Code        string `json:"code"`
	Message     string `json:"message"`
	Status      int    `json:"status"`
	IsRetriable bool   `json:"is_retriable"`
}

// Proxy wraps httputil.ReverseProxy with LightLayer-specific behaviour.
type Proxy struct {
	handler http.Handler
}

// NewProxy creates a configured reverse proxy for the given config.
func NewProxy(cfg *config.Config) (*Proxy, error) {
	originURL, err := url.Parse(cfg.Gateway.Origin.URL)
	if err != nil {
		return nil, err
	}

	transport := NewTransport(cfg)

	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(originURL)
			pr.Out.Host = originURL.Host

			SetForwardedHeaders(pr.Out, pr.In)
			SetRequestID(pr.Out)
			StripHopByHopHeaders(pr.Out.Header)
		},
		Transport:    transport,
		ErrorHandler: errorHandler,
		// Flush immediately for streaming (SSE, chunked).
		FlushInterval: -1,
	}

	return &Proxy{handler: rp}, nil
}

// ServeHTTP implements http.Handler. It validates the request before proxying
// and cleans up if the client disconnects mid-request.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := validateRequest(r); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), false)
		return
	}
	p.handler.ServeHTTP(w, r)
}

// validateRequest checks for malformed requests before they reach the proxy.
func validateRequest(r *http.Request) error {
	// Reject requests with missing or invalid host.
	if r.Host == "" && r.URL.Host == "" {
		return errors.New("missing Host header")
	}

	// Reject requests with invalid method.
	if r.Method == "" {
		return errors.New("missing HTTP method")
	}

	// Reject non-standard methods containing whitespace or control chars.
	for _, c := range r.Method {
		if c <= ' ' || c == 0x7f {
			return errors.New("invalid HTTP method")
		}
	}

	// Reject requests with null bytes in path.
	if strings.ContainsRune(r.URL.Path, 0) {
		return errors.New("invalid request path")
	}

	return nil
}

// errorHandler writes a structured JSON error response when the origin is
// unreachable or a transport-level error occurs.
func errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	// If the client disconnected, just log and bail — nothing to write.
	if r.Context().Err() == context.Canceled {
		slog.Info("client disconnected", "path", r.URL.Path)
		return
	}

	slog.Error("proxy error", "path", r.URL.Path, "error", err)

	status, code, message, retriable := classifyError(err)
	writeError(w, status, code, message, retriable)
}

// classifyError inspects the transport error and returns the appropriate
// HTTP status, error code, message, and retriability.
func classifyError(err error) (status int, code, message string, retriable bool) {
	if isTimeout(err) {
		return http.StatusGatewayTimeout, "gateway_timeout",
			"The origin server did not respond in time.", true
	}

	if isDNSError(err) {
		return http.StatusBadGateway, "bad_gateway",
			"DNS resolution failed for origin.", true
	}

	if isConnectionRefused(err) {
		return http.StatusBadGateway, "bad_gateway",
			"origin unreachable", true
	}

	// Default: generic bad gateway.
	return http.StatusBadGateway, "bad_gateway",
		"The origin server is unavailable.", true
}

// writeError sends a structured JSON error response.
func writeError(w http.ResponseWriter, status int, code, message string, retriable bool) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Type:        "proxy_error",
		Code:        code,
		Message:     message,
		Status:      status,
		IsRetriable: retriable,
	})
}

// isTimeout checks whether an error is a timeout error.
func isTimeout(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

// isDNSError checks whether the error is a DNS resolution failure.
func isDNSError(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr)
}

// isConnectionRefused checks whether the error is a connection refused error.
func isConnectionRefused(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		var sysErr *net.OpError
		if errors.As(opErr.Err, &sysErr) {
			return strings.Contains(sysErr.Error(), "connection refused")
		}
		return strings.Contains(opErr.Error(), "connection refused")
	}
	return strings.Contains(err.Error(), "connection refused")
}
