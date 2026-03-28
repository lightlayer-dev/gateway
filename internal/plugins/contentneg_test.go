package plugins

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrefersJSON(t *testing.T) {
	tests := []struct {
		name      string
		accept    string
		userAgent string
		expected  bool
	}{
		{"explicit json accept", "application/json", "Mozilla/5.0", true},
		{"explicit html accept", "text/html", "curl/7.68", false},
		{"curl user agent", "", "curl/7.68", true},
		{"python user agent", "", "python-requests/2.28", true},
		{"go http client", "", "Go-http-client/1.1", true},
		{"bot user agent", "", "GPTBot/1.0", true},
		{"browser user agent", "", "Mozilla/5.0 (Windows NT 10.0)", false},
		{"empty both", "", "", true},
		{"httpie", "", "HTTPie/3.2.1", true},
		{"node fetch", "", "node-fetch/3.0", true},
		{"spider", "", "Googlebot-spider", true},
		{"anthropic agent", "", "Anthropic-Agent/1.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, PrefersJSON(tt.accept, tt.userAgent))
		})
	}
}

func TestWriteNegotiatedErrorJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	WriteNegotiatedError(w, req, http.StatusNotFound, "not_found", "Resource not found")

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, w.Body.String(), "not_found")
}

func TestWriteNegotiatedErrorHTML(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0)")
	w := httptest.NewRecorder()

	WriteNegotiatedError(w, req, http.StatusNotFound, "not_found", "Resource not found")

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, w.Body.String(), "<!DOCTYPE html>")
	assert.Contains(t, w.Body.String(), "Resource not found")
}

func TestWriteNegotiatedErrorBotGetsJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("User-Agent", "GPTBot/1.0")
	w := httptest.NewRecorder()

	WriteNegotiatedError(w, req, http.StatusUnauthorized, "auth_required", "Authentication required")

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
}
