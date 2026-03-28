package plugins

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/lightlayer-dev/gateway/internal/detection"
)

// Pipeline holds initialized plugins and wraps the proxy handler.
type Pipeline struct {
	plugins []Plugin
}

// PluginConfig represents the config for a single plugin in the pipeline.
type PluginConfig struct {
	Name    string
	Enabled bool
	Config  map[string]interface{}
}

// BuildPipeline creates a Pipeline from ordered plugin configs.
// It instantiates only enabled plugins and initializes them.
// Plugins that fail Init are logged and skipped (not fatal).
func BuildPipeline(configs []PluginConfig) (*Pipeline, error) {
	var active []Plugin

	for _, pc := range configs {
		if !pc.Enabled {
			continue
		}

		ctor := GetConstructor(pc.Name)
		if ctor == nil {
			slog.Warn("unknown plugin, skipping", "plugin", pc.Name)
			continue
		}

		p := ctor()
		if err := p.Init(pc.Config); err != nil {
			slog.Error("plugin init failed, skipping", "plugin", pc.Name, "error", err)
			continue
		}

		active = append(active, p)
		slog.Info("plugin initialized", "plugin", pc.Name)
	}

	return &Pipeline{plugins: active}, nil
}

// Wrap takes the final handler (reverse proxy) and wraps it with the plugin
// middleware chain. The outermost middleware runs first.
// It also injects a RequestContext with agent detection at the start.
func (pl *Pipeline) Wrap(handler http.Handler) http.Handler {
	// Start from the innermost handler and wrap outward.
	h := handler
	for i := len(pl.plugins) - 1; i >= 0; i-- {
		p := pl.plugins[i]
		mw := p.Middleware()
		if mw == nil {
			continue
		}
		h = wrapWithRecovery(p.Name(), mw(h))
	}

	// Outermost: inject RequestContext with agent detection.
	return contextInjector(h)
}

// Close shuts down all active plugins.
func (pl *Pipeline) Close() error {
	var firstErr error
	for _, p := range pl.plugins {
		if err := p.Close(); err != nil {
			slog.Error("plugin close error", "plugin", p.Name(), "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// Plugins returns the list of active plugins (for inspection/testing).
func (pl *Pipeline) Plugins() []Plugin {
	return pl.plugins
}

// contextInjector creates a RequestContext for each request, runs agent
// detection, and attaches the context.
func contextInjector(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := &RequestContext{
			RequestID: uuid.New().String(),
			StartTime: time.Now(),
			Metadata:  make(map[string]interface{}),
		}

		// Run agent detection on User-Agent header.
		if ua := r.Header.Get("User-Agent"); ua != "" {
			rc.AgentInfo = detection.DetectAgent(ua)
		}

		ctx := WithRequestContext(r.Context(), rc)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// wrapWithRecovery catches panics from a plugin middleware so one bad plugin
// doesn't crash the entire gateway.
func wrapWithRecovery(name string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rv := recover(); rv != nil {
				slog.Error("plugin panic recovered",
					"plugin", name,
					"panic", fmt.Sprintf("%v", rv),
					"path", r.URL.Path,
				)
				// If headers haven't been sent, write an error.
				WriteError(w, http.StatusInternalServerError, "internal_error",
					"An internal error occurred.")
			}
		}()
		handler.ServeHTTP(w, r)
	})
}
