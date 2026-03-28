package proxy_test

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lightlayer-dev/gateway/internal/config"
	"github.com/lightlayer-dev/gateway/internal/plugins"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/a2a"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/agentstxt"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/agui"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/analytics"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/apikeys"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/discovery"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/identity"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/mcp"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/oauth2"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/payments"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/ratelimit"
	_ "github.com/lightlayer-dev/gateway/internal/plugins/security"
	"github.com/lightlayer-dev/gateway/internal/proxy"
	"github.com/stretchr/testify/require"
)

func init() {
	// Suppress log output during benchmarks.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// BenchmarkBareProxy measures proxy latency overhead with no plugins enabled.
func BenchmarkBareProxy(b *testing.B) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer origin.Close()

	p, err := proxy.NewProxy(newTestConfig(origin.URL))
	require.NoError(b, err)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
	}
}

// BenchmarkWithAllPlugins measures proxy latency with all plugins enabled.
func BenchmarkWithAllPlugins(b *testing.B) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer origin.Close()

	cfg := &config.Config{
		Gateway: config.GatewayConfig{
			Origin: config.OriginConfig{
				URL:     origin.URL,
				Timeout: config.Duration{Duration: 5 * time.Second},
			},
		},
	}

	p, err := proxy.NewProxy(cfg)
	require.NoError(b, err)

	pipelineCfgs := []plugins.PluginConfig{
		{Name: "security", Enabled: true},
		{Name: "discovery", Enabled: true},
		{Name: "agents_txt", Enabled: true},
		{Name: "identity", Enabled: true, Config: map[string]interface{}{"mode": "log"}},
		{Name: "rate_limits", Enabled: true},
		{Name: "analytics", Enabled: true},
	}

	pl, err := plugins.BuildPipeline(pipelineCfgs)
	require.NoError(b, err)
	defer pl.Close()

	handler := pl.Wrap(p)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ClaudeBot/1.0)")

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

// BenchmarkConcurrent1000 measures throughput under 1000 concurrent requests.
func BenchmarkConcurrent1000(b *testing.B) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer origin.Close()

	cfg := &config.Config{
		Gateway: config.GatewayConfig{
			Origin: config.OriginConfig{
				URL:     origin.URL,
				Timeout: config.Duration{Duration: 5 * time.Second},
			},
		},
	}

	p, err := proxy.NewProxy(cfg)
	require.NoError(b, err)

	pipelineCfgs := []plugins.PluginConfig{
		{Name: "security", Enabled: true},
		{Name: "discovery", Enabled: true},
		{Name: "identity", Enabled: true, Config: map[string]interface{}{"mode": "log"}},
		{Name: "rate_limits", Enabled: true},
		{Name: "analytics", Enabled: true},
	}

	pl, err := plugins.BuildPipeline(pipelineCfgs)
	require.NoError(b, err)
	defer pl.Close()

	handler := pl.Wrap(p)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		const concurrency = 1000
		var wg sync.WaitGroup
		wg.Add(concurrency)
		for j := 0; j < concurrency; j++ {
			go func(id int) {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/item/%d", id), nil)
				req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ClaudeBot/1.0)")
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
			}(j)
		}
		wg.Wait()
	}
}

// BenchmarkProxyLatencyOverhead measures the raw overhead added by the proxy
// layer vs a direct handler call. This is the key metric — target <2ms.
func BenchmarkProxyLatencyOverhead(b *testing.B) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer origin.Close()

	p, err := proxy.NewProxy(newTestConfig(origin.URL))
	require.NoError(b, err)

	pipelineCfgs := []plugins.PluginConfig{
		{Name: "security", Enabled: true},
		{Name: "discovery", Enabled: true},
		{Name: "identity", Enabled: true, Config: map[string]interface{}{"mode": "log"}},
		{Name: "rate_limits", Enabled: true},
		{Name: "analytics", Enabled: true},
	}

	pl, err := plugins.BuildPipeline(pipelineCfgs)
	require.NoError(b, err)
	defer pl.Close()

	handler := pl.Wrap(p)

	// Warm up connections.
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/warmup", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// Measure with real timing.
	var totalOverhead int64
	var count int64

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ClaudeBot/1.0)")

		start := time.Now()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		elapsed := time.Since(start)

		atomic.AddInt64(&totalOverhead, int64(elapsed))
		atomic.AddInt64(&count, 1)
	}

	c := atomic.LoadInt64(&count)
	if c > 0 {
		avg := time.Duration(atomic.LoadInt64(&totalOverhead) / c)
		b.ReportMetric(float64(avg.Microseconds()), "µs/op")
	}
}
