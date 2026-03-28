package proxy

import (
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// hopByHopHeaders are headers that should not be forwarded by proxies.
// Per RFC 2616 Section 13.5.1.
var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
}

// RequestIDHeader is the header name used for per-request tracing.
const RequestIDHeader = "X-LightLayer-Request-Id"

// SetForwardedHeaders adds X-Forwarded-For, X-Forwarded-Proto, and
// X-Forwarded-Host headers to the outgoing request based on the incoming
// request metadata.
func SetForwardedHeaders(req *http.Request, incomingReq *http.Request) {
	clientIP := clientAddr(incomingReq)
	if prior := incomingReq.Header.Get("X-Forwarded-For"); prior != "" {
		clientIP = prior + ", " + clientIP
	}
	req.Header.Set("X-Forwarded-For", clientIP)

	proto := "http"
	if incomingReq.TLS != nil {
		proto = "https"
	}
	if fp := incomingReq.Header.Get("X-Forwarded-Proto"); fp != "" {
		proto = fp
	}
	req.Header.Set("X-Forwarded-Proto", proto)

	req.Header.Set("X-Forwarded-Host", incomingReq.Host)
}

// SetRequestID generates a UUID and sets it on the request headers.
// Returns the generated ID.
func SetRequestID(req *http.Request) string {
	id := uuid.New().String()
	req.Header.Set(RequestIDHeader, id)
	return id
}

// StripHopByHopHeaders removes hop-by-hop headers from the request.
func StripHopByHopHeaders(header http.Header) {
	// Also strip headers listed in the Connection header itself.
	if conn := header.Get("Connection"); conn != "" {
		for _, h := range strings.Split(conn, ",") {
			header.Del(strings.TrimSpace(h))
		}
	}
	for _, h := range hopByHopHeaders {
		header.Del(h)
	}
}

// clientAddr extracts the client IP from the request, stripping the port.
func clientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
