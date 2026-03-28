package plugins_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/lightlayer-dev/gateway/internal/detection"
	"github.com/lightlayer-dev/gateway/internal/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers — mock plugins
// ---------------------------------------------------------------------------

// orderTracker records the order in which plugins execute.
type orderTracker struct {
	mu    sync.Mutex
	order []string
}

func (ot *orderTracker) record(name string) {
	ot.mu.Lock()
	defer ot.mu.Unlock()
	ot.order = append(ot.order, name)
}

func (ot *orderTracker) get() []string {
	ot.mu.Lock()
	defer ot.mu.Unlock()
	cp := make([]string, len(ot.order))
	copy(cp, ot.order)
	return cp
}

// mockPlugin is a configurable test plugin.
type mockPlugin struct {
	name      string
	initErr   error
	middleware func(http.Handler) http.Handler
}

func (m *mockPlugin) Name() string                                   { return m.name }
func (m *mockPlugin) Init(_ map[string]interface{}) error            { return m.initErr }
func (m *mockPlugin) Middleware() func(http.Handler) http.Handler     { return m.middleware }
func (m *mockPlugin) Close() error                                   { return nil }

// orderPlugin records when it runs in the tracker.
func orderPlugin(name string, tracker *orderTracker) *mockPlugin {
	return &mockPlugin{
		name: name,
		middleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tracker.record(name)
				next.ServeHTTP(w, r)
			})
		},
	}
}

// panicPlugin panics during request handling.
func panicPlugin(name string) *mockPlugin {
	return &mockPlugin{
		name: name,
		middleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic("plugin exploded!")
			})
		},
	}
}

// shortCircuitPlugin writes a response without calling next.
func shortCircuitPlugin(name string, status int, body string) *mockPlugin {
	return &mockPlugin{
		name: name,
		middleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				w.Write([]byte(body))
				// Does NOT call next.ServeHTTP
			})
		},
	}
}

// headerPlugin adds a response header and passes through.
func headerPlugin(name, headerKey, headerVal string) *mockPlugin {
	return &mockPlugin{
		name: name,
		middleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(headerKey, headerVal)
				next.ServeHTTP(w, r)
			})
		},
	}
}

// ---------------------------------------------------------------------------
// Test setup: register mock plugins
// ---------------------------------------------------------------------------

func init() {
	// We'll register test plugins dynamically per test via local registries.
	// For pipeline tests that use BuildPipeline, we register some defaults.
	plugins.Register("order_a", func() plugins.Plugin {
		return &mockPlugin{name: "order_a", middleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Add("X-Plugin-Order", "a")
				next.ServeHTTP(w, r)
			})
		}}
	})
	plugins.Register("order_b", func() plugins.Plugin {
		return &mockPlugin{name: "order_b", middleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Add("X-Plugin-Order", "b")
				next.ServeHTTP(w, r)
			})
		}}
	})
	plugins.Register("order_c", func() plugins.Plugin {
		return &mockPlugin{name: "order_c", middleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Add("X-Plugin-Order", "c")
				next.ServeHTTP(w, r)
			})
		}}
	})
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestBuildPipeline_EnabledOnly(t *testing.T) {
	configs := []plugins.PluginConfig{
		{Name: "order_a", Enabled: true},
		{Name: "order_b", Enabled: false},
		{Name: "order_c", Enabled: true},
	}

	pl, err := plugins.BuildPipeline(configs)
	require.NoError(t, err)
	defer pl.Close()

	assert.Len(t, pl.Plugins(), 2)
	assert.Equal(t, "order_a", pl.Plugins()[0].Name())
	assert.Equal(t, "order_c", pl.Plugins()[1].Name())
}

func TestBuildPipeline_UnknownPluginSkipped(t *testing.T) {
	configs := []plugins.PluginConfig{
		{Name: "nonexistent_plugin", Enabled: true},
		{Name: "order_a", Enabled: true},
	}

	pl, err := plugins.BuildPipeline(configs)
	require.NoError(t, err)
	defer pl.Close()

	assert.Len(t, pl.Plugins(), 1)
	assert.Equal(t, "order_a", pl.Plugins()[0].Name())
}

func TestPipeline_MiddlewareOrdering(t *testing.T) {
	configs := []plugins.PluginConfig{
		{Name: "order_a", Enabled: true},
		{Name: "order_b", Enabled: true},
		{Name: "order_c", Enabled: true},
	}

	pl, err := plugins.BuildPipeline(configs)
	require.NoError(t, err)
	defer pl.Close()

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := pl.Wrap(final)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Plugins should execute in config order: a, b, c.
	order := rec.Header().Values("X-Plugin-Order")
	assert.Equal(t, []string{"a", "b", "c"}, order)
}

func TestPipeline_PanicRecovery(t *testing.T) {
	// Register a panicking plugin.
	plugins.Register("panicker", func() plugins.Plugin { return panicPlugin("panicker") })

	configs := []plugins.PluginConfig{
		{Name: "panicker", Enabled: true},
	}

	pl, err := plugins.BuildPipeline(configs)
	require.NoError(t, err)
	defer pl.Close()

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach final handler after panic")
	})

	handler := pl.Wrap(final)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	// Should not panic.
	assert.NotPanics(t, func() {
		handler.ServeHTTP(rec, req)
	})

	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	var env plugins.AgentErrorEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.Equal(t, "internal_error", env.Code)
}

func TestPipeline_ShortCircuit(t *testing.T) {
	// Register a short-circuit plugin.
	plugins.Register("blocker", func() plugins.Plugin {
		return shortCircuitPlugin("blocker", http.StatusForbidden, `{"blocked":true}`)
	})

	configs := []plugins.PluginConfig{
		{Name: "blocker", Enabled: true},
		{Name: "order_a", Enabled: true},
	}

	pl, err := plugins.BuildPipeline(configs)
	require.NoError(t, err)
	defer pl.Close()

	finalReached := false
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		finalReached = true
		w.WriteHeader(http.StatusOK)
	})

	handler := pl.Wrap(final)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.False(t, finalReached, "final handler should not be reached when plugin short-circuits")
}

func TestPipeline_ContextInjection(t *testing.T) {
	configs := []plugins.PluginConfig{}

	pl, err := plugins.BuildPipeline(configs)
	require.NoError(t, err)
	defer pl.Close()

	var capturedRC *plugins.RequestContext
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRC = plugins.GetRequestContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := pl.Wrap(final)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, capturedRC)
	assert.NotEmpty(t, capturedRC.RequestID)
	assert.False(t, capturedRC.StartTime.IsZero())
	assert.NotNil(t, capturedRC.Metadata)
	assert.Nil(t, capturedRC.AgentInfo, "no agent UA header, should be nil")
}

func TestPipeline_AgentDetectionInContext(t *testing.T) {
	configs := []plugins.PluginConfig{}

	pl, err := plugins.BuildPipeline(configs)
	require.NoError(t, err)
	defer pl.Close()

	var capturedInfo *detection.AgentInfo
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := plugins.GetRequestContext(r.Context())
		if rc != nil {
			capturedInfo = rc.AgentInfo
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := pl.Wrap(final)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ClaudeBot/1.0)")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, capturedInfo)
	assert.True(t, capturedInfo.Detected)
	assert.Equal(t, "ClaudeBot", capturedInfo.Name)
	assert.Equal(t, "Anthropic", capturedInfo.Provider)
}

func TestPipeline_NonAgentUA(t *testing.T) {
	configs := []plugins.PluginConfig{}
	pl, err := plugins.BuildPipeline(configs)
	require.NoError(t, err)
	defer pl.Close()

	var capturedRC *plugins.RequestContext
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRC = plugins.GetRequestContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := pl.Wrap(final)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("User-Agent", "curl/7.68.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.NotNil(t, capturedRC)
	assert.Nil(t, capturedRC.AgentInfo, "curl should not be detected as an agent")
}

func TestPipeline_RequestIDUnique(t *testing.T) {
	configs := []plugins.PluginConfig{}
	pl, err := plugins.BuildPipeline(configs)
	require.NoError(t, err)
	defer pl.Close()

	ids := make(map[string]bool)
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := plugins.GetRequestContext(r.Context())
		if rc != nil {
			ids[rc.RequestID] = true
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := pl.Wrap(final)

	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	assert.Len(t, ids, 100, "each request should get a unique ID")
}

func TestPipeline_PluginCanReadContext(t *testing.T) {
	// Register a plugin that reads the RequestContext.
	plugins.Register("ctx_reader", func() plugins.Plugin {
		return &mockPlugin{
			name: "ctx_reader",
			middleware: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					rc := plugins.GetRequestContext(r.Context())
					if rc != nil {
						w.Header().Set("X-Request-ID", rc.RequestID)
						if rc.AgentInfo != nil {
							w.Header().Set("X-Agent-Name", rc.AgentInfo.Name)
						}
					}
					next.ServeHTTP(w, r)
				})
			},
		}
	})

	configs := []plugins.PluginConfig{
		{Name: "ctx_reader", Enabled: true},
	}

	pl, err := plugins.BuildPipeline(configs)
	require.NoError(t, err)
	defer pl.Close()

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := pl.Wrap(final)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("User-Agent", "GPTBot/1.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("X-Request-ID"))
	assert.Equal(t, "GPTBot", rec.Header().Get("X-Agent-Name"))
}

func TestPipeline_EmptyPipeline(t *testing.T) {
	pl, err := plugins.BuildPipeline(nil)
	require.NoError(t, err)
	defer pl.Close()

	assert.Empty(t, pl.Plugins())

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	handler := pl.Wrap(final)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

func TestGetRequestContext_NoContext(t *testing.T) {
	// Without the pipeline injector, context should be nil.
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rc := plugins.GetRequestContext(req.Context())
	assert.Nil(t, rc)
}

func TestPipeline_PluginMetadataSharing(t *testing.T) {
	// Plugin A writes metadata, Plugin B reads it.
	plugins.Register("meta_writer", func() plugins.Plugin {
		return &mockPlugin{
			name: "meta_writer",
			middleware: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					rc := plugins.GetRequestContext(r.Context())
					if rc != nil {
						rc.Metadata["wrote_by"] = "meta_writer"
					}
					next.ServeHTTP(w, r)
				})
			},
		}
	})
	plugins.Register("meta_reader", func() plugins.Plugin {
		return &mockPlugin{
			name: "meta_reader",
			middleware: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					rc := plugins.GetRequestContext(r.Context())
					if rc != nil {
						if v, ok := rc.Metadata["wrote_by"].(string); ok {
							w.Header().Set("X-Meta-From", v)
						}
					}
					next.ServeHTTP(w, r)
				})
			},
		}
	})

	configs := []plugins.PluginConfig{
		{Name: "meta_writer", Enabled: true},
		{Name: "meta_reader", Enabled: true},
	}

	pl, err := plugins.BuildPipeline(configs)
	require.NoError(t, err)
	defer pl.Close()

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := pl.Wrap(final)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, "meta_writer", rec.Header().Get("X-Meta-From"))
}
