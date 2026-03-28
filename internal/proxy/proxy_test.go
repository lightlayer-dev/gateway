package proxy_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lightlayer-dev/gateway/internal/config"
	"github.com/lightlayer-dev/gateway/internal/proxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestConfig(originURL string) *config.Config {
	return &config.Config{
		Gateway: config.GatewayConfig{
			Origin: config.OriginConfig{
				URL:     originURL,
				Timeout: config.Duration{Duration: 5 * time.Second},
			},
		},
	}
}

func TestGETForwarded(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"method": r.Method,
			"path":   r.URL.Path,
		})
	}))
	defer origin.Close()

	p, err := proxy.NewProxy(newTestConfig(origin.URL))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/widgets", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "GET", body["method"])
	assert.Equal(t, "/api/widgets", body["path"])
}

func TestPOSTForwarded(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"method": r.Method,
			"body":   string(bodyBytes),
		})
	}))
	defer origin.Close()

	p, err := proxy.NewProxy(newTestConfig(origin.URL))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/widgets", strings.NewReader(`{"name":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "POST", body["method"])
	assert.Equal(t, `{"name":"test"}`, body["body"])
}

func TestForwardedHeaders(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"x-forwarded-for":   r.Header.Get("X-Forwarded-For"),
			"x-forwarded-proto": r.Header.Get("X-Forwarded-Proto"),
			"x-forwarded-host":  r.Header.Get("X-Forwarded-Host"),
		})
	}))
	defer origin.Close()

	p, err := proxy.NewProxy(newTestConfig(origin.URL))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "10.0.0.1", body["x-forwarded-for"])
	assert.Equal(t, "http", body["x-forwarded-proto"])
	assert.NotEmpty(t, body["x-forwarded-host"])
}

func TestRequestIDAdded(t *testing.T) {
	var receivedID string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedID = r.Header.Get(proxy.RequestIDHeader)
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()

	p, err := proxy.NewProxy(newTestConfig(origin.URL))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotEmpty(t, receivedID)
	// UUID v4 format: 8-4-4-4-12
	assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`, receivedID)
}

func TestOriginDown502(t *testing.T) {
	// Point to a closed server so the connection is refused.
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	originURL := origin.URL
	origin.Close()

	p, err := proxy.NewProxy(newTestConfig(originURL))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var errResp proxy.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	assert.Equal(t, "proxy_error", errResp.Type)
	assert.Equal(t, "bad_gateway", errResp.Code)
	assert.Equal(t, 502, errResp.Status)
	assert.True(t, errResp.IsRetriable)
}

func TestTimeout504(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()

	cfg := newTestConfig(origin.URL)
	cfg.Gateway.Origin.Timeout = config.Duration{Duration: 50 * time.Millisecond}

	p, err := proxy.NewProxy(cfg)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusGatewayTimeout, rec.Code)

	var errResp proxy.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	assert.Equal(t, "gateway_timeout", errResp.Code)
	assert.Equal(t, 504, errResp.Status)
}

func TestQueryStringsPreserved(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"query": r.URL.RawQuery,
		})
	}))
	defer origin.Close()

	p, err := proxy.NewProxy(newTestConfig(origin.URL))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/search?q=hello&page=2", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "q=hello&page=2", body["query"])
}

func TestStreamingResponse(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		for i := 0; i < 3; i++ {
			fmt.Fprintf(w, "data: event %d\n\n", i)
			flusher.Flush()
		}
	}))
	defer origin.Close()

	p, err := proxy.NewProxy(newTestConfig(origin.URL))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))

	body := rec.Body.String()
	assert.Contains(t, body, "data: event 0")
	assert.Contains(t, body, "data: event 1")
	assert.Contains(t, body, "data: event 2")
}

func TestOriginalHeadersPreserved(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"authorization": r.Header.Get("Authorization"),
			"custom":        r.Header.Get("X-Custom-Header"),
		})
	}))
	defer origin.Close()

	p, err := proxy.NewProxy(newTestConfig(origin.URL))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer token123")
	req.Header.Set("X-Custom-Header", "custom-value")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "Bearer token123", body["authorization"])
	assert.Equal(t, "custom-value", body["custom"])
}
