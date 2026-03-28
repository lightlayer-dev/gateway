package plugins

import (
	"context"
	"net/http"
	"time"

	"github.com/lightlayer-dev/gateway/internal/detection"
)

// Plugin is the interface all gateway plugins implement.
type Plugin interface {
	Name() string
	Init(cfg map[string]interface{}) error
	Middleware() func(http.Handler) http.Handler
	Close() error
}

// RequestContext carries per-request metadata through the plugin pipeline.
type RequestContext struct {
	RequestID string
	StartTime time.Time
	AgentInfo *detection.AgentInfo
	Metadata  map[string]interface{}
}

// contextKey is an unexported type for context keys to avoid collisions.
type contextKey struct{}

// reqCtxKey is the context key for RequestContext.
var reqCtxKey = contextKey{}

// WithRequestContext returns a new context carrying the RequestContext.
func WithRequestContext(ctx context.Context, rc *RequestContext) context.Context {
	return context.WithValue(ctx, reqCtxKey, rc)
}

// GetRequestContext retrieves the RequestContext from the context, or nil.
func GetRequestContext(ctx context.Context) *RequestContext {
	rc, _ := ctx.Value(reqCtxKey).(*RequestContext)
	return rc
}

// PluginConstructor is a factory function that creates a new Plugin instance.
type PluginConstructor func() Plugin

// registry maps plugin names to their constructors.
var registry = map[string]PluginConstructor{}

// Register adds a plugin constructor to the global registry.
func Register(name string, ctor PluginConstructor) {
	registry[name] = ctor
}

// GetConstructor returns the constructor for a named plugin, or nil.
func GetConstructor(name string) PluginConstructor {
	return registry[name]
}

// RegisteredPlugins returns the names of all registered plugins.
func RegisteredPlugins() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}
