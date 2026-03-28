package plugins

import "net/http"

// Plugin is the interface all gateway plugins implement.
type Plugin interface {
	Name() string
	Init(cfg map[string]interface{}) error
	Middleware() func(http.Handler) http.Handler
	Close() error
}
