package plugins

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// botPattern matches User-Agent strings from bots, agents, and CLI tools.
// Ported from agent-layer-ts error-handler.ts prefersJson().
var botPattern = regexp.MustCompile(`(?i)bot|crawl|spider|agent|curl|httpie|python|node|go-http`)

// PrefersJSON detects if a client wants JSON or HTML responses.
// Agents, bots, curl, and programmatic clients get JSON; browsers get HTML.
func PrefersJSON(accept, userAgent string) bool {
	// Explicit Accept header takes precedence.
	if strings.Contains(accept, "application/json") {
		return true
	}
	if strings.Contains(accept, "text/html") {
		return false
	}

	// Fall back to User-Agent detection.
	if userAgent == "" {
		return true // No UA likely means programmatic client.
	}
	return botPattern.MatchString(userAgent)
}

// WriteNegotiatedError writes an error response using content negotiation.
// JSON for agents/bots, HTML for browsers.
func WriteNegotiatedError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	accept := r.Header.Get("Accept")
	ua := r.Header.Get("User-Agent")

	if PrefersJSON(accept, ua) {
		WriteError(w, status, code, message)
		return
	}

	// HTML error page for browsers.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write([]byte(renderHTMLError(status, code, message)))
}

func renderHTMLError(status int, code, message string) string {
	return `<!DOCTYPE html>
<html>
<head><title>` + statusText(status) + `</title>
<style>body{font-family:system-ui,sans-serif;max-width:600px;margin:80px auto;padding:0 20px;color:#333}
h1{color:#e53e3e}code{background:#f7fafc;padding:2px 6px;border-radius:3px;font-size:0.9em}</style>
</head>
<body>
<h1>` + statusText(status) + `</h1>
<p>` + message + `</p>
<p><code>` + code + `</code></p>
</body>
</html>`
}

func statusText(status int) string {
	switch status {
	case 400:
		return "400 Bad Request"
	case 401:
		return "401 Unauthorized"
	case 403:
		return "403 Forbidden"
	case 404:
		return "404 Not Found"
	case 429:
		return "429 Too Many Requests"
	case 500:
		return "500 Internal Server Error"
	case 502:
		return "502 Bad Gateway"
	case 503:
		return "503 Service Unavailable"
	case 504:
		return "504 Gateway Timeout"
	default:
		return fmt.Sprintf("%d Error", status)
	}
}
