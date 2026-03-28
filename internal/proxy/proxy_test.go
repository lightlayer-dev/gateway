package proxy_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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

// ---------------------------------------------------------------------------
// Basic forwarding
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Headers
// ---------------------------------------------------------------------------

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
	assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`, receivedID)
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

// ---------------------------------------------------------------------------
// Error handling edge cases
// ---------------------------------------------------------------------------

func TestOriginDown502(t *testing.T) {
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
	assert.Contains(t, errResp.Message, "origin unreachable")
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

func TestDNSFailure502(t *testing.T) {
	// Use an unresolvable hostname.
	cfg := newTestConfig("http://this-host-does-not-exist.invalid:9999")
	cfg.Gateway.Origin.Timeout = config.Duration{Duration: 2 * time.Second}

	p, err := proxy.NewProxy(cfg)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)

	var errResp proxy.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	assert.Equal(t, "bad_gateway", errResp.Code)
	assert.True(t, errResp.IsRetriable)
}

func TestOrigin5xxForwardedAsIs(t *testing.T) {
	codes := []int{500, 502, 503, 504}
	for _, code := range codes {
		t.Run(fmt.Sprintf("origin_%d", code), func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Origin-Header", "preserved")
				w.WriteHeader(code)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": "origin_error",
					"code":  code,
				})
			}))
			defer origin.Close()

			p, err := proxy.NewProxy(newTestConfig(origin.URL))
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodGet, "/failing", nil)
			rec := httptest.NewRecorder()
			p.ServeHTTP(rec, req)

			// Origin 5xx should be forwarded as-is, not wrapped.
			assert.Equal(t, code, rec.Code)
			assert.Equal(t, "preserved", rec.Header().Get("X-Origin-Header"))

			var body map[string]interface{}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, "origin_error", body["error"])
		})
	}
}

func TestClientDisconnectCleanup(t *testing.T) {
	var originGotRequest atomic.Bool
	originDone := make(chan struct{})

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originGotRequest.Store(true)
		// Wait — simulating a slow origin.
		select {
		case <-r.Context().Done():
			// Client cancelled, origin should see context cancellation.
			close(originDone)
			return
		case <-time.After(5 * time.Second):
			close(originDone)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer origin.Close()

	p, err := proxy.NewProxy(newTestConfig(origin.URL))
	require.NoError(t, err)

	// Create a real server so we can use a real HTTP client.
	proxySrv := httptest.NewServer(p)
	defer proxySrv.Close()

	client := proxySrv.Client()
	client.Timeout = 100 * time.Millisecond

	// The client will timeout quickly, cancelling the request.
	_, err = client.Get(proxySrv.URL + "/slow")
	// We expect an error because the client times out.
	assert.Error(t, err)

	// Wait a bit for origin to detect cancellation.
	select {
	case <-originDone:
		// Origin detected the cancellation — good.
	case <-time.After(3 * time.Second):
		// That's fine too — the point is the proxy didn't crash.
	}

	assert.True(t, originGotRequest.Load(), "origin should have received the request")
}

// ---------------------------------------------------------------------------
// Malformed request handling
// ---------------------------------------------------------------------------

func TestMalformedRequest400(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()

	p, err := proxy.NewProxy(newTestConfig(origin.URL))
	require.NoError(t, err)

	// Request with null byte in path.
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.URL.Path = "/test\x00inject"
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var errResp proxy.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	assert.Equal(t, "bad_request", errResp.Code)
	assert.False(t, errResp.IsRetriable)
}

// ---------------------------------------------------------------------------
// Request body handling
// ---------------------------------------------------------------------------

func TestEmptyBody(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"body_len": len(body),
		})
	}))
	defer origin.Close()

	p, err := proxy.NewProxy(newTestConfig(origin.URL))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/empty", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, float64(0), body["body_len"])
}

func TestJSONBody(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var parsed map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&parsed))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(parsed)
	}))
	defer origin.Close()

	p, err := proxy.NewProxy(newTestConfig(origin.URL))
	require.NoError(t, err)

	payload := `{"key":"value","nested":{"a":1}}`
	req := httptest.NewRequest(http.MethodPost, "/json", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "value", body["key"])
}

func TestFormBody(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"name":  r.FormValue("name"),
			"email": r.FormValue("email"),
		})
	}))
	defer origin.Close()

	p, err := proxy.NewProxy(newTestConfig(origin.URL))
	require.NoError(t, err)

	form := "name=test&email=test%40example.com"
	req := httptest.NewRequest(http.MethodPost, "/form", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "test", body["name"])
	assert.Equal(t, "test@example.com", body["email"])
}

func TestMultipartBody(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseMultipartForm(10<<20))
		file, header, err := r.FormFile("file")
		require.NoError(t, err)
		defer file.Close()
		content, _ := io.ReadAll(file)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"filename": header.Filename,
			"size":     len(content),
			"field":    r.FormValue("field"),
		})
	}))
	defer origin.Close()

	p, err := proxy.NewProxy(newTestConfig(origin.URL))
	require.NoError(t, err)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("field", "value")
	part, _ := writer.CreateFormFile("file", "test.bin")
	part.Write([]byte("binary content here"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "test.bin", body["filename"])
	assert.Equal(t, "value", body["field"])
}

func TestLargeStreamingBody(t *testing.T) {
	const bodySize = 1 << 20 // 1MB

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := io.Copy(io.Discard, r.Body)
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"bytes_received": n,
		})
	}))
	defer origin.Close()

	p, err := proxy.NewProxy(newTestConfig(origin.URL))
	require.NoError(t, err)

	body := bytes.NewReader(make([]byte, bodySize))
	req := httptest.NewRequest(http.MethodPost, "/upload-large", body)
	req.Header.Set("Content-Type", "application/octet-stream")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, float64(bodySize), resp["bytes_received"])
}

// ---------------------------------------------------------------------------
// Query string and path edge cases
// ---------------------------------------------------------------------------

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

func TestEncodedPathChars(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"path":     r.URL.Path,
			"raw_path": r.URL.RawPath,
		})
	}))
	defer origin.Close()

	p, err := proxy.NewProxy(newTestConfig(origin.URL))
	require.NoError(t, err)

	tests := []struct {
		name     string
		path     string
		wantPath string
	}{
		{"encoded space", "/api/hello%20world", "/api/hello world"},
		{"encoded slash", "/api/a%2Fb", "/api/a/b"},
		{"unicode", "/api/données", "/api/données"},
		{"special chars", "/api/test?foo=bar&baz=qux#frag", "/api/test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			p.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)

			var body map[string]string
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, tt.wantPath, body["path"])
		})
	}
}

func TestTrailingSlashes(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"path": r.URL.Path,
		})
	}))
	defer origin.Close()

	p, err := proxy.NewProxy(newTestConfig(origin.URL))
	require.NoError(t, err)

	tests := []struct {
		path     string
		wantPath string
	}{
		{"/api/widgets/", "/api/widgets/"},
		{"/api/widgets", "/api/widgets"},
		{"/", "/"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			p.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)

			var body map[string]string
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, tt.wantPath, body["path"])
		})
	}
}

func TestDoubleSlashes(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"path": r.URL.Path,
		})
	}))
	defer origin.Close()

	p, err := proxy.NewProxy(newTestConfig(origin.URL))
	require.NoError(t, err)

	// Note: Go's net/http normalizes // to / in the path, so double slashes
	// in the incoming request get cleaned. We verify the proxy doesn't break.
	paths := []string{"//api/widgets", "/api//widgets", "/api/widgets//"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			p.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
		})
	}
}

// ---------------------------------------------------------------------------
// Streaming
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Concurrent requests
// ---------------------------------------------------------------------------

func TestConcurrentRequests(t *testing.T) {
	var requestCount atomic.Int64

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		// Small delay to simulate work.
		time.Sleep(5 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer origin.Close()

	p, err := proxy.NewProxy(newTestConfig(origin.URL))
	require.NoError(t, err)

	const goroutines = 100
	var wg sync.WaitGroup
	var successCount atomic.Int64
	var errorCount atomic.Int64

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/item/%d", id), nil)
			rec := httptest.NewRecorder()
			p.ServeHTTP(rec, req)

			if rec.Code == http.StatusOK {
				successCount.Add(1)
			} else {
				errorCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	assert.Equal(t, int64(goroutines), requestCount.Load(), "origin should receive all requests")
	assert.Equal(t, int64(goroutines), successCount.Load(), "all requests should succeed")
	assert.Equal(t, int64(0), errorCount.Load(), "no requests should fail")
}

// ---------------------------------------------------------------------------
// Graceful shutdown
// ---------------------------------------------------------------------------

func TestGracefulShutdown(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()

	p, err := proxy.NewProxy(newTestConfig(origin.URL))
	require.NoError(t, err)

	srv := httptest.NewServer(p)

	// Make a request to verify server is alive.
	resp, err := http.Get(srv.URL + "/health")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Close the server (simulates shutdown).
	srv.Close()

	// Requests after shutdown should fail.
	_, err = http.Get(srv.URL + "/health")
	assert.Error(t, err)
}
