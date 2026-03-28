package proxy

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"github.com/lightlayer-dev/gateway/internal/config"
)

// NewTransport creates a custom http.RoundTripper with configurable timeouts
// and connection pooling based on the gateway configuration.
func NewTransport(cfg *config.Config) http.RoundTripper {
	timeout := cfg.Gateway.Origin.Timeout.Duration
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: timeout,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.Gateway.Origin.TLSSkipVerify,
		},
	}
}
