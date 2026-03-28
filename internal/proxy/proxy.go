package proxy

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/lightlayer-dev/gateway/internal/config"
)

// ErrorResponse is a structured JSON error returned when the proxy cannot
// reach the origin or encounters a transport error.
type ErrorResponse struct {
	Type       string `json:"type"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Status     int    `json:"status"`
	IsRetriable bool  `json:"is_retriable"`
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

// ServeHTTP implements http.Handler.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.handler.ServeHTTP(w, r)
}

// errorHandler writes a structured JSON error response when the origin is
// unreachable or a transport-level error occurs.
func errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("proxy error", "path", r.URL.Path, "error", err)

	status := http.StatusBadGateway
	code := "bad_gateway"
	message := "The origin server is unavailable."
	retriable := true

	if isTimeout(err) {
		status = http.StatusGatewayTimeout
		code = "gateway_timeout"
		message = "The origin server did not respond in time."
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Type:       "proxy_error",
		Code:       code,
		Message:    message,
		Status:     status,
		IsRetriable: retriable,
	})
}

// isTimeout checks whether an error is a timeout error.
func isTimeout(err error) bool {
	type timeouter interface {
		Timeout() bool
	}
	if t, ok := err.(timeouter); ok {
		return t.Timeout()
	}
	return false
}
