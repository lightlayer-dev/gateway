package analytics

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lightlayer-dev/gateway/internal/detection"
	"github.com/lightlayer-dev/gateway/internal/plugins"
	"github.com/lightlayer-dev/gateway/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// injectContext wraps a handler with a RequestContext containing agent info.
func injectContext(next http.Handler, agentName string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := &plugins.RequestContext{
			RequestID: "test-req-1",
			StartTime: time.Now(),
			Metadata:  map[string]interface{}{},
		}
		if agentName != "" {
			rc.AgentInfo = &detection.AgentInfo{
				Detected: true,
				Name:     agentName,
				Provider: "Test",
			}
		}
		ctx := plugins.WithRequestContext(r.Context(), rc)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func TestAnalytics_NonBlocking(t *testing.T) {
	p := New()
	require.NoError(t, p.Init(map[string]interface{}{}))
	defer p.Close()

	// Handler that sleeps — analytics should not add significant overhead.
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	handler := injectContext(p.Middleware()(backend), "TestBot")

	start := time.Now()
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("User-Agent", "TestBot/1.0")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	assert.Equal(t, http.StatusOK, rec.Code)
	// The handler itself is instant; analytics should add negligible overhead.
	assert.Less(t, elapsed, 50*time.Millisecond)
}

func TestAnalytics_JSONLFormat(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.jsonl")

	p := New()
	require.NoError(t, p.Init(map[string]interface{}{
		"log_file":       logPath,
		"flush_interval": "100ms",
	}))

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	})

	handler := injectContext(p.Middleware()(backend), "ClaudeBot")

	req := httptest.NewRequest("GET", "/api/widgets", nil)
	req.Header.Set("User-Agent", "ClaudeBot/1.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Wait for flush.
	time.Sleep(200 * time.Millisecond)
	p.Close()

	// Read and parse JSONL.
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.Len(t, lines, 1)

	var event store.AgentEvent
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &event))

	assert.Equal(t, "ClaudeBot", event.Agent)
	assert.Equal(t, "GET", event.Method)
	assert.Equal(t, "/api/widgets", event.Path)
	assert.Equal(t, 200, event.StatusCode)
	assert.Greater(t, event.DurationMs, 0.0)
	assert.Equal(t, "application/json", event.ContentType)
	assert.Greater(t, event.ResponseSize, int64(0))
}

func TestAnalytics_SQLiteIntegration(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "analytics.db")

	p := New()
	require.NoError(t, p.Init(map[string]interface{}{
		"db_path":        dbPath,
		"flush_interval": "100ms",
	}))

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := injectContext(p.Middleware()(backend), "GPTBot")

	// Send 3 requests.
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.Header.Set("User-Agent", "GPTBot/1.0")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// Wait for flush.
	time.Sleep(200 * time.Millisecond)
	p.Close()

	// Open the DB directly and verify.
	s, err := store.NewSQLiteStore(dbPath)
	require.NoError(t, err)
	defer s.Close()

	events, err := s.QueryEvents(store.EventFilter{Agent: "GPTBot"})
	require.NoError(t, err)
	assert.Len(t, events, 3)
}

func TestAnalytics_SkipsNonAgentTraffic(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.jsonl")

	p := New()
	require.NoError(t, p.Init(map[string]interface{}{
		"log_file":       logPath,
		"flush_interval": "100ms",
	}))

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// No agent context — should be skipped.
	handler := injectContext(p.Middleware()(backend), "")

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	time.Sleep(200 * time.Millisecond)
	p.Close()

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(string(data)))
}

func TestAnalytics_TrackAll(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.jsonl")

	p := New()
	require.NoError(t, p.Init(map[string]interface{}{
		"log_file":       logPath,
		"track_all":      true,
		"flush_interval": "100ms",
	}))

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// No agent context but trackAll is on.
	handler := injectContext(p.Middleware()(backend), "")

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	time.Sleep(200 * time.Millisecond)
	p.Close()

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.NotEmpty(t, strings.TrimSpace(string(data)))
}

func TestAnalytics_RemoteEndpoint(t *testing.T) {
	var mu sync.Mutex
	var received []store.AgentEvent

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Events []store.AgentEvent `json:"events"`
		}
		json.Unmarshal(body, &payload)
		mu.Lock()
		received = append(received, payload.Events...)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := New()
	require.NoError(t, p.Init(map[string]interface{}{
		"endpoint":       srv.URL,
		"api_key":        "test-key",
		"flush_interval": "100ms",
	}))

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := injectContext(p.Middleware()(backend), "ClaudeBot")

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("User-Agent", "ClaudeBot/1.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	time.Sleep(300 * time.Millisecond)
	p.Close()

	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, received, 1)
	assert.Equal(t, "ClaudeBot", received[0].Agent)
}

func TestAnalytics_CapturesStatusAndSize(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.jsonl")

	p := New()
	require.NoError(t, p.Init(map[string]interface{}{
		"log_file":       logPath,
		"flush_interval": "100ms",
	}))

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	})

	handler := injectContext(p.Middleware()(backend), "TestBot")

	req := httptest.NewRequest("POST", "/missing", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	time.Sleep(200 * time.Millisecond)
	p.Close()

	data, _ := os.ReadFile(logPath)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.Len(t, lines, 1)

	var event store.AgentEvent
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &event))
	assert.Equal(t, 404, event.StatusCode)
	assert.Equal(t, "POST", event.Method)
	assert.Equal(t, int64(9), event.ResponseSize) // len("not found")
}

func TestAnalytics_PluginRegistry(t *testing.T) {
	ctor := plugins.GetConstructor("analytics")
	require.NotNil(t, ctor, "analytics plugin should be registered")
	p := ctor()
	assert.Equal(t, "analytics", p.Name())
}
