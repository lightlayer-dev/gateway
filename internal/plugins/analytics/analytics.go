// Package analytics implements non-blocking agent traffic telemetry.
// Ported from agent-layer-ts analytics.ts with additions for JSONL logging,
// SQLite persistence, and optional remote endpoint flushing.
package analytics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lightlayer-dev/gateway/internal/plugins"
	"github.com/lightlayer-dev/gateway/internal/store"
)

func init() {
	plugins.Register("analytics", func() plugins.Plugin { return New() })
}

// Default configuration values matching agent-layer-ts EventBuffer defaults.
const (
	defaultBufferSize    = 50
	defaultFlushInterval = 30 * time.Second
	defaultRetention     = 30 * 24 * time.Hour // 30 days
	defaultCleanupEvery  = 1 * time.Hour
)

// Plugin implements non-blocking analytics collection with batch flushing.
type Plugin struct {
	logFile       string
	endpoint      string
	apiKey        string
	dbPath        string
	bufferSize    int
	flushInterval time.Duration
	retention     time.Duration
	trackAll      bool

	eventCh chan store.AgentEvent
	store   store.Store
	logFd   *os.File

	stopOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// New creates a new analytics plugin.
func New() *Plugin {
	return &Plugin{
		bufferSize:    defaultBufferSize,
		flushInterval: defaultFlushInterval,
		retention:     defaultRetention,
		stopCh:        make(chan struct{}),
	}
}

func (p *Plugin) Name() string { return "analytics" }

func (p *Plugin) Init(cfg map[string]interface{}) error {
	if v, ok := cfg["log_file"].(string); ok && v != "" {
		p.logFile = v
	}
	if v, ok := cfg["endpoint"].(string); ok && v != "" {
		p.endpoint = v
	}
	if v, ok := cfg["api_key"].(string); ok && v != "" {
		p.apiKey = v
	}
	if v, ok := cfg["db_path"].(string); ok && v != "" {
		p.dbPath = v
	}
	if v, ok := cfg["buffer_size"]; ok {
		p.bufferSize = toInt(v)
	}
	if v, ok := cfg["flush_interval"].(string); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			p.flushInterval = d
		}
	}
	if v, ok := cfg["retention"].(string); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			p.retention = d
		}
	}
	if v, ok := cfg["track_all"].(bool); ok {
		p.trackAll = v
	}

	if p.bufferSize <= 0 {
		p.bufferSize = defaultBufferSize
	}

	// Open JSONL log file.
	if p.logFile != "" {
		fd, err := os.OpenFile(p.logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		p.logFd = fd
	}

	// Open SQLite store.
	if p.dbPath != "" {
		s, err := store.NewSQLiteStore(p.dbPath)
		if err != nil {
			return fmt.Errorf("open sqlite store: %w", err)
		}
		p.store = s
	}

	// Buffered channel — writers never block.
	p.eventCh = make(chan store.AgentEvent, p.bufferSize*3)

	// Background goroutine drains events.
	p.wg.Add(1)
	go p.drainLoop()

	return nil
}

// SetStore allows injecting a store (used in tests).
func (p *Plugin) SetStore(s store.Store) {
	p.store = s
}

func (p *Plugin) Close() error {
	p.stopOnce.Do(func() {
		close(p.stopCh)
	})
	p.wg.Wait()

	var firstErr error
	if p.logFd != nil {
		if err := p.logFd.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if p.store != nil {
		if err := p.store.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// responseCapture wraps http.ResponseWriter to capture status code and bytes written.
type responseCapture struct {
	http.ResponseWriter
	statusCode int
	written    int64
}

func (rc *responseCapture) WriteHeader(code int) {
	rc.statusCode = code
	rc.ResponseWriter.WriteHeader(code)
}

func (rc *responseCapture) Write(b []byte) (int, error) {
	n, err := rc.ResponseWriter.Write(b)
	rc.written += int64(n)
	return n, err
}

// Flush implements http.Flusher for streaming support.
func (rc *responseCapture) Flush() {
	if f, ok := rc.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (p *Plugin) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rc := plugins.GetRequestContext(r.Context())

			// Unless trackAll is set, only log detected agent traffic.
			if !p.trackAll && (rc == nil || rc.AgentInfo == nil || !rc.AgentInfo.Detected) {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			capture := &responseCapture{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(capture, r)

			agentName := ""
			if rc != nil && rc.AgentInfo != nil {
				agentName = rc.AgentInfo.Name
			}

			event := store.AgentEvent{
				ID:           uuid.New().String(),
				Timestamp:    start,
				Agent:        agentName,
				UserAgent:    r.Header.Get("User-Agent"),
				Method:       r.Method,
				Path:         r.URL.Path,
				StatusCode:   capture.statusCode,
				DurationMs:   float64(time.Since(start).Microseconds()) / 1000.0,
				ContentType:  capture.Header().Get("Content-Type"),
				ResponseSize: capture.written,
			}

			// Capture identity and payment metadata if present.
			if rc != nil {
				if v, ok := rc.Metadata["identity_info"].(string); ok {
					event.IdentityInfo = v
				}
				if v, ok := rc.Metadata["payment_info"].(string); ok {
					event.PaymentInfo = v
				}
				if v, ok := rc.Metadata["rate_limit"].(string); ok {
					event.RateLimit = v
				}
			}

			// Non-blocking send — drop event if channel is full rather than
			// slowing down the request.
			select {
			case p.eventCh <- event:
			default:
				slog.Warn("analytics event dropped, buffer full")
			}
		})
	}
}

// drainLoop is the background goroutine that consumes events from the channel,
// batches them, and flushes to all configured sinks.
func (p *Plugin) drainLoop() {
	defer p.wg.Done()

	batch := make([]store.AgentEvent, 0, p.bufferSize)
	flushTicker := time.NewTicker(p.flushInterval)
	defer flushTicker.Stop()

	cleanupTicker := time.NewTicker(defaultCleanupEvery)
	defer cleanupTicker.Stop()

	for {
		select {
		case event, ok := <-p.eventCh:
			if !ok {
				// Channel closed — flush remaining.
				p.flushBatch(batch)
				return
			}
			batch = append(batch, event)
			if len(batch) >= p.bufferSize {
				p.flushBatch(batch)
				batch = batch[:0]
			}

		case <-flushTicker.C:
			if len(batch) > 0 {
				p.flushBatch(batch)
				batch = batch[:0]
			}

		case <-cleanupTicker.C:
			p.runCleanup()

		case <-p.stopCh:
			// Drain remaining events from the channel.
			for {
				select {
				case event := <-p.eventCh:
					batch = append(batch, event)
				default:
					p.flushBatch(batch)
					return
				}
			}
		}
	}
}

// flushBatch writes a batch of events to all configured sinks.
func (p *Plugin) flushBatch(batch []store.AgentEvent) {
	if len(batch) == 0 {
		return
	}

	// Write to JSONL log file.
	if p.logFd != nil {
		for i := range batch {
			data, err := json.Marshal(&batch[i])
			if err != nil {
				slog.Warn("analytics: marshal event failed", "error", err)
				continue
			}
			data = append(data, '\n')
			if _, err := p.logFd.Write(data); err != nil {
				slog.Warn("analytics: write log failed", "error", err)
			}
		}
	}

	// Write to SQLite store.
	if p.store != nil {
		for i := range batch {
			if err := p.store.SaveEvent(batch[i]); err != nil {
				slog.Warn("analytics: save event failed", "error", err)
			}
		}
	}

	// POST to remote endpoint.
	if p.endpoint != "" {
		p.postBatch(batch)
	}
}

// postBatch sends a batch of events to the configured remote endpoint.
func (p *Plugin) postBatch(batch []store.AgentEvent) {
	payload, err := json.Marshal(map[string]interface{}{"events": batch})
	if err != nil {
		slog.Warn("analytics: marshal batch failed", "error", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		slog.Warn("analytics: create request failed", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("X-API-Key", p.apiKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("analytics: post batch failed", "error", err)
		return
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		slog.Warn("analytics: remote endpoint returned error", "status", resp.StatusCode)
	}
}

// runCleanup removes events older than the retention period.
func (p *Plugin) runCleanup() {
	if p.store == nil {
		return
	}
	deleted, err := p.store.Cleanup(p.retention)
	if err != nil {
		slog.Warn("analytics: cleanup failed", "error", err)
		return
	}
	if deleted > 0 {
		slog.Info("analytics: cleaned up old events", "deleted", deleted)
	}
}

// toInt converts various numeric types to int.
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
