// Package ratelimit implements a sliding window rate limiter plugin.
// Ported from agent-layer-ts rate-limit.ts.
package ratelimit

import (
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/lightlayer-dev/gateway/internal/plugins"
)

func init() {
	plugins.Register("rate_limits", func() plugins.Plugin { return New() })
}

// window tracks request counts within a sliding window.
type window struct {
	mu        sync.Mutex
	count     int
	expiresAt time.Time
}

// Plugin implements per-agent sliding window rate limiting.
type Plugin struct {
	defaultLimit int
	defaultWindow time.Duration
	perAgent     map[string]limitConfig
	windows      sync.Map // map[string]*window

	// NowFunc allows overriding time in tests.
	NowFunc func() time.Time
}

type limitConfig struct {
	requests int
	window   time.Duration
}

// New creates a new rate limit plugin.
func New() *Plugin {
	return &Plugin{
		perAgent: make(map[string]limitConfig),
		NowFunc:  time.Now,
	}
}

func (p *Plugin) Name() string { return "rate_limits" }

func (p *Plugin) Init(cfg map[string]interface{}) error {
	// Parse default limits.
	if def, ok := cfg["default"].(map[string]interface{}); ok {
		if r, ok := def["requests"]; ok {
			p.defaultLimit = toInt(r)
		}
		if w, ok := def["window"]; ok {
			d, err := parseDuration(w)
			if err != nil {
				return fmt.Errorf("invalid default window: %w", err)
			}
			p.defaultWindow = d
		}
	}

	if p.defaultLimit <= 0 {
		p.defaultLimit = 100
	}
	if p.defaultWindow <= 0 {
		p.defaultWindow = 60 * time.Second
	}

	// Parse per-agent overrides.
	if pa, ok := cfg["per_agent"].(map[string]interface{}); ok {
		for agent, v := range pa {
			lc := limitConfig{
				requests: p.defaultLimit,
				window:   p.defaultWindow,
			}
			if m, ok := v.(map[string]interface{}); ok {
				if r, ok := m["requests"]; ok {
					lc.requests = toInt(r)
				}
				if w, ok := m["window"]; ok {
					d, err := parseDuration(w)
					if err != nil {
						slog.Warn("invalid per_agent window, using default", "agent", agent, "error", err)
						continue
					}
					lc.window = d
				}
			}
			p.perAgent[agent] = lc
		}
	}

	slog.Info("rate_limits plugin initialized",
		"default_requests", p.defaultLimit,
		"default_window", p.defaultWindow,
		"per_agent_overrides", len(p.perAgent),
	)
	return nil
}

func (p *Plugin) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := p.resolveKey(r)
			limit, windowDur := p.resolveLimit(key)

			count := p.increment(key, windowDur)
			remaining := limit - count
			if remaining < 0 {
				remaining = 0
			}

			now := p.NowFunc()
			resetTime := now.Add(windowDur)

			// Always set rate limit headers.
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime.Unix(), 10))

			if count > limit {
				retryAfter := int(math.Ceil(windowDur.Seconds()))
				plugins.WriteErrorFull(w, plugins.AgentErrorEnvelope{
					Type:        "rate_limit_error",
					Code:        "rate_limit_exceeded",
					Message:     fmt.Sprintf("Rate limit exceeded. Try again in %d seconds.", retryAfter),
					Status:      http.StatusTooManyRequests,
					IsRetriable: true,
					RetryAfter:  &retryAfter,
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (p *Plugin) Close() error { return nil }

// resolveKey returns the rate limit key for the request (agent name or "__default__").
func (p *Plugin) resolveKey(r *http.Request) string {
	rc := plugins.GetRequestContext(r.Context())
	if rc != nil && rc.AgentInfo != nil && rc.AgentInfo.Detected && rc.AgentInfo.Name != "" {
		return rc.AgentInfo.Name
	}
	return "__default__"
}

// resolveLimit returns the (requests, window) for a given key.
func (p *Plugin) resolveLimit(key string) (int, time.Duration) {
	if lc, ok := p.perAgent[key]; ok {
		return lc.requests, lc.window
	}
	return p.defaultLimit, p.defaultWindow
}

// increment atomically increments the counter for the given key, returning the new count.
// If the window has expired, a new window is started.
func (p *Plugin) increment(key string, windowDur time.Duration) int {
	now := p.NowFunc()

	val, loaded := p.windows.Load(key)
	if loaded {
		w := val.(*window)
		w.mu.Lock()
		if now.Before(w.expiresAt) {
			w.count++
			c := w.count
			w.mu.Unlock()
			return c
		}
		// Window expired — reset it under the lock.
		w.count = 1
		w.expiresAt = now.Add(windowDur)
		w.mu.Unlock()
		return 1
	}

	// New window.
	w := &window{
		count:     1,
		expiresAt: now.Add(windowDur),
	}
	p.windows.Store(key, w)
	return 1
}

// Cleanup removes expired windows. Useful for long-running processes.
func (p *Plugin) Cleanup() {
	now := p.NowFunc()
	p.windows.Range(func(key, val interface{}) bool {
		w := val.(*window)
		if now.After(w.expiresAt) || now.Equal(w.expiresAt) {
			p.windows.Delete(key)
		}
		return true
	})
}

// helpers

func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func parseDuration(v interface{}) (time.Duration, error) {
	switch d := v.(type) {
	case string:
		return time.ParseDuration(d)
	case int:
		return time.Duration(d) * time.Second, nil
	case int64:
		return time.Duration(d) * time.Second, nil
	case float64:
		return time.Duration(d) * float64ToDuration(d), nil
	case time.Duration:
		return d, nil
	default:
		return 0, fmt.Errorf("unsupported duration type: %T", v)
	}
}

func float64ToDuration(d float64) time.Duration {
	_ = d
	return time.Second
}
