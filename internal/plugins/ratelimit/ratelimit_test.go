package ratelimit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lightlayer-dev/gateway/internal/detection"
	"github.com/lightlayer-dev/gateway/internal/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPlugin(t *testing.T, cfg map[string]interface{}) *Plugin {
	t.Helper()
	p := New()
	err := p.Init(cfg)
	require.NoError(t, err)
	return p
}

func makeRequest(p *Plugin, agentInfo *detection.AgentInfo) *httptest.ResponseRecorder {
	handler := p.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rc := &plugins.RequestContext{
		RequestID: "test-123",
		StartTime: time.Now(),
		AgentInfo: agentInfo,
		Metadata:  make(map[string]interface{}),
	}
	ctx := plugins.WithRequestContext(req.Context(), rc)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestDefaultRateLimiting(t *testing.T) {
	p := setupPlugin(t, map[string]interface{}{
		"default": map[string]interface{}{
			"requests": 3,
			"window":   "1m",
		},
	})

	// First 3 requests should succeed.
	for i := 0; i < 3; i++ {
		rr := makeRequest(p, nil)
		assert.Equal(t, http.StatusOK, rr.Code, "request %d should succeed", i+1)
	}

	// 4th request should be rate limited.
	rr := makeRequest(p, nil)
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)
}

func TestRateLimitHeaders(t *testing.T) {
	p := setupPlugin(t, map[string]interface{}{
		"default": map[string]interface{}{
			"requests": 10,
			"window":   "1m",
		},
	})

	rr := makeRequest(p, nil)
	assert.Equal(t, "10", rr.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "9", rr.Header().Get("X-RateLimit-Remaining"))
	assert.NotEmpty(t, rr.Header().Get("X-RateLimit-Reset"))
}

func TestRateLimitHeadersOnExhaust(t *testing.T) {
	p := setupPlugin(t, map[string]interface{}{
		"default": map[string]interface{}{
			"requests": 1,
			"window":   "1m",
		},
	})

	// First request uses up the limit.
	makeRequest(p, nil)

	// Second request should be limited and still have headers.
	rr := makeRequest(p, nil)
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)
	assert.Equal(t, "1", rr.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "0", rr.Header().Get("X-RateLimit-Remaining"))
}

func TestPerAgentOverrides(t *testing.T) {
	p := setupPlugin(t, map[string]interface{}{
		"default": map[string]interface{}{
			"requests": 2,
			"window":   "1m",
		},
		"per_agent": map[string]interface{}{
			"ClaudeBot": map[string]interface{}{
				"requests": 5,
				"window":   "1m",
			},
		},
	})

	claude := &detection.AgentInfo{Detected: true, Name: "ClaudeBot", Provider: "Anthropic"}

	// ClaudeBot should get 5 requests.
	for i := 0; i < 5; i++ {
		rr := makeRequest(p, claude)
		assert.Equal(t, http.StatusOK, rr.Code, "ClaudeBot request %d should succeed", i+1)
	}

	// 6th ClaudeBot request should be limited.
	rr := makeRequest(p, claude)
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)

	// Default agent should still only get 2.
	for i := 0; i < 2; i++ {
		rr := makeRequest(p, nil)
		assert.Equal(t, http.StatusOK, rr.Code)
	}
	rr = makeRequest(p, nil)
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)
}

func TestRateLimitErrorEnvelope(t *testing.T) {
	p := setupPlugin(t, map[string]interface{}{
		"default": map[string]interface{}{
			"requests": 1,
			"window":   "1m",
		},
	})

	makeRequest(p, nil) // use up limit

	rr := makeRequest(p, nil)
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var env plugins.AgentErrorEnvelope
	err := json.NewDecoder(rr.Body).Decode(&env)
	require.NoError(t, err)

	assert.Equal(t, "rate_limit_error", env.Type)
	assert.Equal(t, "rate_limit_exceeded", env.Code)
	assert.True(t, env.IsRetriable)
	assert.NotNil(t, env.RetryAfter)
	assert.Equal(t, 60, *env.RetryAfter)
	assert.NotEmpty(t, rr.Header().Get("Retry-After"))
}

func TestWindowExpiry(t *testing.T) {
	p := setupPlugin(t, map[string]interface{}{
		"default": map[string]interface{}{
			"requests": 1,
			"window":   "1m",
		},
	})

	now := time.Now()
	p.NowFunc = func() time.Time { return now }

	makeRequest(p, nil) // use up limit

	// Advance time past window.
	p.NowFunc = func() time.Time { return now.Add(61 * time.Second) }

	rr := makeRequest(p, nil)
	assert.Equal(t, http.StatusOK, rr.Code, "request after window expiry should succeed")
}

func TestCleanup(t *testing.T) {
	p := setupPlugin(t, map[string]interface{}{
		"default": map[string]interface{}{
			"requests": 10,
			"window":   "1m",
		},
	})

	now := time.Now()
	p.NowFunc = func() time.Time { return now }

	makeRequest(p, nil)

	// Advance past window and cleanup.
	p.NowFunc = func() time.Time { return now.Add(61 * time.Second) }
	p.Cleanup()

	// Verify window was cleaned.
	count := 0
	p.windows.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	assert.Equal(t, 0, count)
}

func TestDifferentAgentsGetSeparateWindows(t *testing.T) {
	p := setupPlugin(t, map[string]interface{}{
		"default": map[string]interface{}{
			"requests": 2,
			"window":   "1m",
		},
	})

	claude := &detection.AgentInfo{Detected: true, Name: "ClaudeBot", Provider: "Anthropic"}
	gpt := &detection.AgentInfo{Detected: true, Name: "GPTBot", Provider: "OpenAI"}

	// Use up ClaudeBot's limit.
	for i := 0; i < 2; i++ {
		makeRequest(p, claude)
	}
	rr := makeRequest(p, claude)
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)

	// GPTBot should still have its own window.
	rr = makeRequest(p, gpt)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestPluginName(t *testing.T) {
	p := New()
	assert.Equal(t, "rate_limits", p.Name())
}

func TestDefaultConfig(t *testing.T) {
	p := setupPlugin(t, map[string]interface{}{})
	assert.Equal(t, 100, p.defaultLimit)
	assert.Equal(t, 60*time.Second, p.defaultWindow)
}
