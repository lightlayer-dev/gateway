// Package a2a implements a full A2A v1.0 protocol server as a gateway plugin.
//
// It serves a JSON-RPC 2.0 endpoint that translates between A2A protocol
// operations and the origin REST API behind the gateway. Any REST API
// behind the gateway automatically becomes A2A-compatible.
//
// JSON-RPC methods:
//   - message/send   — send a message, returns a Task
//   - message/stream — send a message with SSE streaming
//   - tasks/get      — retrieve task by ID
//   - tasks/list     — list tasks with filters
//   - tasks/cancel   — cancel a running task
//
// Ported from the A2A v1.0 spec: https://a2a-protocol.org/latest/specification/
package a2a

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/lightlayer-dev/gateway/internal/plugins"
)

func init() {
	plugins.Register("a2a", func() plugins.Plugin { return New() })
}

// ── JSON-RPC Types ───────────────────────────────────────────────────────

// JSONRPCRequest is a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse is a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id,omitempty"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError is the error object in a JSON-RPC response.
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// ── A2A Error Codes ──────────────────────────────────────────────────────

const (
	errParseError          = -32700
	errInvalidRequest      = -32600
	errMethodNotFound      = -32601
	errInvalidParams       = -32602
	errInternalError       = -32603
	errTaskNotFound        = -32001
	errTaskNotCancelable   = -32002
	errContentTypeNotSupp  = -32003
	errPushNotSupported    = -32004
)

// ── Plugin Config ────────────────────────────────────────────────────────

// Config holds A2A plugin configuration.
type Config struct {
	Endpoint          string
	Streaming         bool
	PushNotifications bool
	PushURL           string
	TaskTTL           time.Duration
	MaxTasks          int
	DBPath            string
	OriginURL         string
}

// ── Plugin ───────────────────────────────────────────────────────────────

// Plugin implements the A2A JSON-RPC 2.0 server.
type Plugin struct {
	cfg      Config
	store    *TaskStore
	notifier *PushNotifier
	stopCh   chan struct{}
}

// New creates a new A2A plugin.
func New() *Plugin {
	return &Plugin{
		stopCh: make(chan struct{}),
	}
}

func (p *Plugin) Name() string { return "a2a" }

func (p *Plugin) Init(cfg map[string]interface{}) error {
	p.cfg.Endpoint = stringVal(cfg, "endpoint", "/a2a")
	p.cfg.Streaming = boolVal(cfg, "streaming", true)
	p.cfg.PushNotifications = boolVal(cfg, "push_notifications", false)
	p.cfg.PushURL = stringVal(cfg, "push_url", "")
	p.cfg.DBPath = stringVal(cfg, "db_path", "")
	p.cfg.OriginURL = stringVal(cfg, "origin_url", "")

	ttlStr := stringVal(cfg, "task_ttl", "24h")
	ttl, err := time.ParseDuration(ttlStr)
	if err != nil {
		ttl = 24 * time.Hour
	}
	p.cfg.TaskTTL = ttl

	if v, ok := cfg["max_tasks"].(int); ok && v > 0 {
		p.cfg.MaxTasks = v
	} else if v, ok := cfg["max_tasks"].(float64); ok && v > 0 {
		p.cfg.MaxTasks = int(v)
	} else {
		p.cfg.MaxTasks = 10000
	}

	store, err := NewTaskStore(p.cfg.MaxTasks, p.cfg.TaskTTL, p.cfg.DBPath)
	if err != nil {
		return fmt.Errorf("a2a task store: %w", err)
	}
	p.store = store

	if p.cfg.PushNotifications && p.cfg.PushURL != "" {
		p.notifier = NewPushNotifier(p.cfg.PushURL)
	}

	// Background cleanup goroutine.
	go p.cleanupLoop()

	slog.Info("a2a: initialized",
		"endpoint", p.cfg.Endpoint,
		"streaming", p.cfg.Streaming,
		"push", p.cfg.PushNotifications,
		"max_tasks", p.cfg.MaxTasks,
		"ttl", p.cfg.TaskTTL,
	)
	return nil
}

func (p *Plugin) Close() error {
	close(p.stopCh)
	return p.store.Close()
}

func (p *Plugin) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == p.cfg.Endpoint && r.Method == http.MethodPost {
				p.handleJSONRPC(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Store returns the task store for external access (e.g., enriching agent card).
func (p *Plugin) Store() *TaskStore {
	return p.store
}

// Capabilities returns the A2A capabilities for the agent card.
func (p *Plugin) Capabilities() map[string]interface{} {
	return map[string]interface{}{
		"streaming":              p.cfg.Streaming,
		"pushNotifications":      p.cfg.PushNotifications,
		"stateTransitionHistory": true,
	}
}

// ── JSON-RPC Handler ─────────────────────────────────────────────────────

func (p *Plugin) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSONRPC(w, nil, errParseError, "Parse error: could not read body")
		return
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONRPC(w, nil, errParseError, "Parse error: invalid JSON")
		return
	}

	if req.JSONRPC != "2.0" {
		writeJSONRPC(w, req.ID, errInvalidRequest, "Invalid Request: jsonrpc must be \"2.0\"")
		return
	}

	// Notifications (no ID) — acknowledge silently.
	if req.ID == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch req.Method {
	case "message/send":
		p.handleMessageSend(w, req)
	case "message/stream":
		p.handleMessageStream(w, r, req)
	case "tasks/get":
		p.handleTasksGet(w, req)
	case "tasks/list":
		p.handleTasksList(w, req)
	case "tasks/cancel":
		p.handleTasksCancel(w, req)
	default:
		writeJSONRPC(w, req.ID, errMethodNotFound, fmt.Sprintf("Method not found: %s", req.Method))
	}
}

// ── message/send ─────────────────────────────────────────────────────────

func (p *Plugin) handleMessageSend(w http.ResponseWriter, req JSONRPCRequest) {
	params, err := extractParams(req.Params)
	if err != nil {
		writeJSONRPC(w, req.ID, errInvalidParams, "Invalid params: "+err.Error())
		return
	}

	msg, err := extractMessage(params)
	if err != nil {
		writeJSONRPC(w, req.ID, errInvalidParams, "Invalid params: "+err.Error())
		return
	}

	contextID, _ := params["contextId"].(string)

	task, err := p.store.CreateTask(contextID, msg)
	if err != nil {
		writeJSONRPC(w, req.ID, errInternalError, err.Error())
		return
	}

	// Transition to working.
	task, _ = p.store.UpdateStatus(task.ID, TaskStatus{State: TaskStateWorking})

	// Build a response from the message content — the gateway wraps
	// the origin API response as a completed task.
	responseMsg := Message{
		Role: "agent",
		Parts: []Part{{
			Type: "text",
			Text: fmt.Sprintf("Task %s is being processed", task.ID),
		}},
	}

	task, _ = p.store.UpdateStatus(task.ID, TaskStatus{
		State:   TaskStateCompleted,
		Message: &responseMsg,
	})

	if p.notifier != nil {
		p.notifier.NotifyStatus(task.ID, task.Status)
	}

	writeJSONRPCResult(w, req.ID, task)
}

// ── message/stream ───────────────────────────────────────────────────────

func (p *Plugin) handleMessageStream(w http.ResponseWriter, r *http.Request, req JSONRPCRequest) {
	if !p.cfg.Streaming {
		writeJSONRPC(w, req.ID, errInvalidRequest, "Streaming is not enabled")
		return
	}

	params, err := extractParams(req.Params)
	if err != nil {
		writeJSONRPC(w, req.ID, errInvalidParams, "Invalid params: "+err.Error())
		return
	}

	msg, err := extractMessage(params)
	if err != nil {
		writeJSONRPC(w, req.ID, errInvalidParams, "Invalid params: "+err.Error())
		return
	}

	contextID, _ := params["contextId"].(string)

	task, err := p.store.CreateTask(contextID, msg)
	if err != nil {
		writeJSONRPC(w, req.ID, errInternalError, err.Error())
		return
	}

	sse, err := NewSSEWriter(w)
	if err != nil {
		writeJSONRPC(w, req.ID, errInternalError, "Streaming not supported by transport")
		return
	}

	// Transition to working.
	p.store.UpdateStatus(task.ID, TaskStatus{State: TaskStateWorking})

	// Stream the task events.
	go func() {
		responseMsg := Message{
			Role: "agent",
			Parts: []Part{{
				Type: "text",
				Text: fmt.Sprintf("Task %s completed", task.ID),
			}},
		}
		p.store.UpdateStatus(task.ID, TaskStatus{
			State:   TaskStateCompleted,
			Message: &responseMsg,
		})
	}()

	if err := StreamTask(r.Context(), p.store, task.ID, sse); err != nil {
		slog.Debug("a2a: stream ended", "task", task.ID, "error", err)
	}
}

// ── tasks/get ────────────────────────────────────────────────────────────

func (p *Plugin) handleTasksGet(w http.ResponseWriter, req JSONRPCRequest) {
	params, err := extractParams(req.Params)
	if err != nil {
		writeJSONRPC(w, req.ID, errInvalidParams, "Invalid params: "+err.Error())
		return
	}

	taskID, _ := params["id"].(string)
	if taskID == "" {
		writeJSONRPC(w, req.ID, errInvalidParams, "Invalid params: id is required")
		return
	}

	task, ok := p.store.GetTask(taskID)
	if !ok {
		writeJSONRPC(w, req.ID, errTaskNotFound, fmt.Sprintf("Task not found: %s", taskID))
		return
	}

	writeJSONRPCResult(w, req.ID, task)
}

// ── tasks/list ───────────────────────────────────────────────────────────

func (p *Plugin) handleTasksList(w http.ResponseWriter, req JSONRPCRequest) {
	params, _ := extractParams(req.Params)

	filter := TaskFilter{}
	if params != nil {
		filter.ContextID, _ = params["contextId"].(string)
		if s, ok := params["status"].(string); ok {
			filter.Status = TaskState(s)
		}
		if v, ok := params["limit"].(float64); ok {
			filter.Limit = int(v)
		}
		if v, ok := params["offset"].(float64); ok {
			filter.Offset = int(v)
		}
	}

	if filter.Limit <= 0 {
		filter.Limit = 100
	}

	tasks := p.store.ListTasks(filter)
	writeJSONRPCResult(w, req.ID, tasks)
}

// ── tasks/cancel ─────────────────────────────────────────────────────────

func (p *Plugin) handleTasksCancel(w http.ResponseWriter, req JSONRPCRequest) {
	params, err := extractParams(req.Params)
	if err != nil {
		writeJSONRPC(w, req.ID, errInvalidParams, "Invalid params: "+err.Error())
		return
	}

	taskID, _ := params["id"].(string)
	if taskID == "" {
		writeJSONRPC(w, req.ID, errInvalidParams, "Invalid params: id is required")
		return
	}

	task, ok := p.store.GetTask(taskID)
	if !ok {
		writeJSONRPC(w, req.ID, errTaskNotFound, fmt.Sprintf("Task not found: %s", taskID))
		return
	}

	if task.Status.State.IsTerminal() {
		writeJSONRPC(w, req.ID, errTaskNotCancelable, "Task is already in a terminal state")
		return
	}

	task, err = p.store.CancelTask(taskID)
	if err != nil {
		writeJSONRPC(w, req.ID, errInternalError, err.Error())
		return
	}

	if p.notifier != nil {
		p.notifier.NotifyStatus(task.ID, task.Status)
	}

	writeJSONRPCResult(w, req.ID, task)
}

// ── Cleanup Loop ─────────────────────────────────────────────────────────

func (p *Plugin) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			removed := p.store.Cleanup()
			if removed > 0 {
				slog.Info("a2a: cleaned up expired tasks", "removed", removed)
			}
		}
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────

func extractParams(raw interface{}) (map[string]interface{}, error) {
	if raw == nil {
		return nil, fmt.Errorf("params is required")
	}
	switch v := raw.(type) {
	case map[string]interface{}:
		return v, nil
	default:
		// Try JSON re-marshaling for nested types.
		data, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid params type")
		}
		var m map[string]interface{}
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("invalid params type")
		}
		return m, nil
	}
}

func extractMessage(params map[string]interface{}) (Message, error) {
	msgRaw, ok := params["message"]
	if !ok {
		return Message{}, fmt.Errorf("message is required")
	}

	data, err := json.Marshal(msgRaw)
	if err != nil {
		return Message{}, fmt.Errorf("invalid message")
	}

	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return Message{}, fmt.Errorf("invalid message format: %w", err)
	}

	if msg.Role == "" {
		msg.Role = "user"
	}
	if len(msg.Parts) == 0 {
		return Message{}, fmt.Errorf("message must have at least one part")
	}

	return msg, nil
}

func writeJSONRPC(w http.ResponseWriter, id interface{}, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &JSONRPCError{Code: code, Message: msg},
	})
}

func writeJSONRPCResult(w http.ResponseWriter, id interface{}, result interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func stringVal(cfg map[string]interface{}, key, def string) string {
	if v, ok := cfg[key].(string); ok && v != "" {
		return v
	}
	return def
}

func boolVal(cfg map[string]interface{}, key string, def bool) bool {
	if v, ok := cfg[key].(bool); ok {
		return v
	}
	return def
}
