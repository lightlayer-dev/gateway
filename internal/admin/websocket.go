package admin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lightlayer-dev/gateway/internal/store"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true // Admin API is already auth-gated.
	},
}

// LogEntry is a request log entry broadcast to WebSocket clients.
type LogEntry struct {
	Timestamp    time.Time `json:"timestamp"`
	RequestID    string    `json:"request_id,omitempty"`
	Agent        string    `json:"agent,omitempty"`
	Method       string    `json:"method"`
	Path         string    `json:"path"`
	StatusCode   int       `json:"status_code"`
	DurationMs   float64   `json:"duration_ms"`
	UserAgent    string    `json:"user_agent,omitempty"`
	ContentType  string    `json:"content_type,omitempty"`
	ResponseSize int64     `json:"response_size,omitempty"`
}

// LogEntryFromEvent converts a store.AgentEvent to a LogEntry.
func LogEntryFromEvent(e store.AgentEvent) LogEntry {
	return LogEntry{
		Timestamp:    e.Timestamp,
		RequestID:    e.ID,
		Agent:        e.Agent,
		Method:       e.Method,
		Path:         e.Path,
		StatusCode:   e.StatusCode,
		DurationMs:   e.DurationMs,
		UserAgent:    e.UserAgent,
		ContentType:  e.ContentType,
		ResponseSize: e.ResponseSize,
	}
}

// LogHub manages WebSocket subscribers for live log streaming.
type LogHub struct {
	mu      sync.RWMutex
	clients map[*wsClient]struct{}
	closed  bool
}

// wsClient represents a connected WebSocket client.
type wsClient struct {
	conn   *websocket.Conn
	send   chan []byte
	filter wsFilter
	done   chan struct{}
}

// wsFilter specifies per-client filtering.
type wsFilter struct {
	Agent     string
	Path      string
	MinStatus int
	MaxStatus int
}

// NewLogHub creates a new LogHub.
func NewLogHub() *LogHub {
	return &LogHub{
		clients: make(map[*wsClient]struct{}),
	}
}

// Broadcast sends a log entry to all connected WebSocket clients.
// Entries are dropped for slow clients (backpressure).
func (h *LogHub) Broadcast(entry LogEntry) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.closed || len(h.clients) == 0 {
		return
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	for c := range h.clients {
		if !matchesFilter(entry, c.filter) {
			continue
		}
		select {
		case c.send <- data:
		default:
			// Slow client — drop to avoid blocking.
		}
	}
}

// BroadcastEvent converts a store event and broadcasts it.
func (h *LogHub) BroadcastEvent(e store.AgentEvent) {
	h.Broadcast(LogEntryFromEvent(e))
}

// Close shuts down the hub and disconnects all clients.
func (h *LogHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for c := range h.clients {
		close(c.done)
		delete(h.clients, c)
	}
}

// ClientCount returns the number of connected WebSocket clients.
func (h *LogHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *LogHub) addClient(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = struct{}{}
}

func (h *LogHub) removeClient(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
}

// handleWSLogs upgrades to WebSocket and streams live request logs.
func (s *Server) handleWSLogs(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("admin: websocket upgrade failed", "error", err)
		return
	}

	q := r.URL.Query()
	minStatus := 0
	maxStatus := 0
	if v := q.Get("min_status"); v != "" {
		n, _ := parseIntDefault(v, 0)
		minStatus = n
	}
	if v := q.Get("max_status"); v != "" {
		n, _ := parseIntDefault(v, 0)
		maxStatus = n
	}

	client := &wsClient{
		conn: conn,
		send: make(chan []byte, 256),
		filter: wsFilter{
			Agent:     q.Get("agent"),
			Path:      q.Get("path"),
			MinStatus: minStatus,
			MaxStatus: maxStatus,
		},
		done: make(chan struct{}),
	}

	s.logHub.addClient(client)

	// Writer goroutine.
	go func() {
		defer func() {
			s.logHub.removeClient(client)
			conn.Close()
		}()

		for {
			select {
			case msg, ok := <-client.send:
				if !ok {
					return
				}
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					return
				}
			case <-client.done:
				return
			}
		}
	}()

	// Reader goroutine — just reads to detect disconnects.
	go func() {
		defer close(client.done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
}

func matchesFilter(entry LogEntry, f wsFilter) bool {
	if f.Agent != "" && entry.Agent != f.Agent {
		return false
	}
	if f.Path != "" && entry.Path != f.Path {
		return false
	}
	if f.MinStatus > 0 && entry.StatusCode < f.MinStatus {
		return false
	}
	if f.MaxStatus > 0 && entry.StatusCode > f.MaxStatus {
		return false
	}
	return true
}

func parseIntDefault(s string, def int) (int, error) {
	if s == "" {
		return def, nil
	}
	n, err := parseInt(s)
	if err != nil {
		return def, err
	}
	return n, nil
}

func parseInt(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, &json.InvalidUnmarshalError{}
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
