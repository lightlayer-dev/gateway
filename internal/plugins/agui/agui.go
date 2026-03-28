// Package agui implements the AG-UI protocol — Server-Sent Events streaming
// for agent UIs (CopilotKit, Google ADK, and other AG-UI-compatible frontends).
//
// Ported from agent-layer-ts ag-ui.ts.
//
// Event types:
//   - Lifecycle: RUN_STARTED, RUN_FINISHED, RUN_ERROR, STEP_STARTED, STEP_FINISHED
//   - Text: TEXT_MESSAGE_START, TEXT_MESSAGE_CONTENT, TEXT_MESSAGE_END
//   - Tool calls: TOOL_CALL_START, TOOL_CALL_ARGS, TOOL_CALL_END, TOOL_CALL_RESULT
//   - State: STATE_SNAPSHOT, STATE_DELTA
//   - Custom: CUSTOM
package agui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lightlayer-dev/gateway/internal/plugins"
)

func init() {
	plugins.Register("ag_ui", func() plugins.Plugin { return New() })
}

// ── Event Types ──────────────────────────────────────────────────────────

// EventType is an AG-UI event type string.
type EventType string

const (
	EventRunStarted         EventType = "RUN_STARTED"
	EventRunFinished        EventType = "RUN_FINISHED"
	EventRunError           EventType = "RUN_ERROR"
	EventStepStarted        EventType = "STEP_STARTED"
	EventStepFinished       EventType = "STEP_FINISHED"
	EventTextMessageStart   EventType = "TEXT_MESSAGE_START"
	EventTextMessageContent EventType = "TEXT_MESSAGE_CONTENT"
	EventTextMessageEnd     EventType = "TEXT_MESSAGE_END"
	EventToolCallStart      EventType = "TOOL_CALL_START"
	EventToolCallArgs       EventType = "TOOL_CALL_ARGS"
	EventToolCallEnd        EventType = "TOOL_CALL_END"
	EventToolCallResult     EventType = "TOOL_CALL_RESULT"
	EventStateSnapshot      EventType = "STATE_SNAPSHOT"
	EventStateDelta         EventType = "STATE_DELTA"
	EventCustom             EventType = "CUSTOM"
)

// Role is the message author role.
type Role string

const (
	RoleDeveloper Role = "developer"
	RoleSystem    Role = "system"
	RoleAssistant Role = "assistant"
	RoleUser      Role = "user"
	RoleTool      Role = "tool"
)

// ── Event Structs ────────────────────────────────────────────────────────

// BaseEvent is embedded in all AG-UI events.
type BaseEvent struct {
	Type      EventType `json:"type"`
	Timestamp int64     `json:"timestamp,omitempty"`
}

// RunStartedEvent signals a run has started.
type RunStartedEvent struct {
	BaseEvent
	ThreadID    string `json:"threadId"`
	RunID       string `json:"runId"`
	ParentRunID string `json:"parentRunId,omitempty"`
}

// RunFinishedEvent signals a run has finished.
type RunFinishedEvent struct {
	BaseEvent
	ThreadID string      `json:"threadId"`
	RunID    string      `json:"runId"`
	Result   interface{} `json:"result,omitempty"`
}

// RunErrorEvent signals a run error.
type RunErrorEvent struct {
	BaseEvent
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// StepStartedEvent signals a step has started.
type StepStartedEvent struct {
	BaseEvent
	StepName string `json:"stepName"`
}

// StepFinishedEvent signals a step has finished.
type StepFinishedEvent struct {
	BaseEvent
	StepName string `json:"stepName"`
}

// TextMessageStartEvent signals a text message is starting.
type TextMessageStartEvent struct {
	BaseEvent
	MessageID string `json:"messageId"`
	Role      Role   `json:"role"`
}

// TextMessageContentEvent carries a text delta.
type TextMessageContentEvent struct {
	BaseEvent
	MessageID string `json:"messageId"`
	Delta     string `json:"delta"`
}

// TextMessageEndEvent signals a text message is complete.
type TextMessageEndEvent struct {
	BaseEvent
	MessageID string `json:"messageId"`
}

// ToolCallStartEvent signals a tool call is starting.
type ToolCallStartEvent struct {
	BaseEvent
	ToolCallID      string `json:"toolCallId"`
	ToolCallName    string `json:"toolCallName"`
	ParentMessageID string `json:"parentMessageId,omitempty"`
}

// ToolCallArgsEvent carries tool call argument deltas.
type ToolCallArgsEvent struct {
	BaseEvent
	ToolCallID string `json:"toolCallId"`
	Delta      string `json:"delta"`
}

// ToolCallEndEvent signals a tool call is complete.
type ToolCallEndEvent struct {
	BaseEvent
	ToolCallID string `json:"toolCallId"`
}

// ToolCallResultEvent carries the tool call result.
type ToolCallResultEvent struct {
	BaseEvent
	ToolCallID string `json:"toolCallId"`
	Result     string `json:"result"`
}

// StateSnapshotEvent carries a full state snapshot.
type StateSnapshotEvent struct {
	BaseEvent
	Snapshot map[string]interface{} `json:"snapshot"`
}

// StateDeltaEvent carries a JSON Patch delta.
type StateDeltaEvent struct {
	BaseEvent
	Delta []interface{} `json:"delta"`
}

// CustomEvent carries a custom named event.
type CustomEvent struct {
	BaseEvent
	Name  string      `json:"name"`
	Value interface{} `json:"value"`
}

// ── SSE Encoding ─────────────────────────────────────────────────────────

// EncodeEvent formats an event as an SSE data line.
func EncodeEvent(event interface{}) ([]byte, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}

	// Extract the type field for the event: line.
	var base struct {
		Type EventType `json:"type"`
	}
	json.Unmarshal(data, &base)

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "event: %s\n", base.Type)
	fmt.Fprintf(&buf, "data: %s\n\n", data)
	return buf.Bytes(), nil
}

// ── Emitter ──────────────────────────────────────────────────────────────

// Emitter provides a high-level API for emitting AG-UI events.
type Emitter struct {
	mu               sync.Mutex
	write            func([]byte) error
	threadID         string
	runID            string
	currentMessageID string
	currentToolID    string
}

// EmitterOptions configures the emitter.
type EmitterOptions struct {
	ThreadID string
	RunID    string
}

// NewEmitter creates an AG-UI event emitter.
func NewEmitter(write func([]byte) error, opts EmitterOptions) *Emitter {
	threadID := opts.ThreadID
	if threadID == "" {
		threadID = uuid.New().String()
	}
	runID := opts.RunID
	if runID == "" {
		runID = uuid.New().String()
	}
	return &Emitter{
		write:    write,
		threadID: threadID,
		runID:    runID,
	}
}

func (e *Emitter) emit(event interface{}) error {
	data, err := EncodeEvent(event)
	if err != nil {
		return err
	}
	return e.write(data)
}

func now() int64 { return time.Now().UnixMilli() }

// ThreadID returns the thread ID.
func (e *Emitter) ThreadID() string { return e.threadID }

// RunID returns the run ID.
func (e *Emitter) RunID() string { return e.runID }

// RunStarted emits a RUN_STARTED event.
func (e *Emitter) RunStarted(parentRunID string) error {
	return e.emit(RunStartedEvent{
		BaseEvent:   BaseEvent{Type: EventRunStarted, Timestamp: now()},
		ThreadID:    e.threadID,
		RunID:       e.runID,
		ParentRunID: parentRunID,
	})
}

// RunFinished emits a RUN_FINISHED event.
func (e *Emitter) RunFinished(result interface{}) error {
	return e.emit(RunFinishedEvent{
		BaseEvent: BaseEvent{Type: EventRunFinished, Timestamp: now()},
		ThreadID:  e.threadID,
		RunID:     e.runID,
		Result:    result,
	})
}

// RunError emits a RUN_ERROR event.
func (e *Emitter) RunError(message, code string) error {
	return e.emit(RunErrorEvent{
		BaseEvent: BaseEvent{Type: EventRunError, Timestamp: now()},
		Message:   message,
		Code:      code,
	})
}

// StepStarted emits a STEP_STARTED event.
func (e *Emitter) StepStarted(stepName string) error {
	return e.emit(StepStartedEvent{
		BaseEvent: BaseEvent{Type: EventStepStarted, Timestamp: now()},
		StepName:  stepName,
	})
}

// StepFinished emits a STEP_FINISHED event.
func (e *Emitter) StepFinished(stepName string) error {
	return e.emit(StepFinishedEvent{
		BaseEvent: BaseEvent{Type: EventStepFinished, Timestamp: now()},
		StepName:  stepName,
	})
}

// TextStart emits a TEXT_MESSAGE_START event and returns the message ID.
func (e *Emitter) TextStart(role Role, messageID string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if messageID == "" {
		messageID = uuid.New().String()
	}
	e.currentMessageID = messageID

	return messageID, e.emit(TextMessageStartEvent{
		BaseEvent: BaseEvent{Type: EventTextMessageStart, Timestamp: now()},
		MessageID: messageID,
		Role:      role,
	})
}

// TextDelta emits a TEXT_MESSAGE_CONTENT event.
func (e *Emitter) TextDelta(delta string, messageID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if messageID == "" {
		messageID = e.currentMessageID
	}
	return e.emit(TextMessageContentEvent{
		BaseEvent: BaseEvent{Type: EventTextMessageContent, Timestamp: now()},
		MessageID: messageID,
		Delta:     delta,
	})
}

// TextEnd emits a TEXT_MESSAGE_END event.
func (e *Emitter) TextEnd(messageID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if messageID == "" {
		messageID = e.currentMessageID
	}
	err := e.emit(TextMessageEndEvent{
		BaseEvent: BaseEvent{Type: EventTextMessageEnd, Timestamp: now()},
		MessageID: messageID,
	})
	if messageID == e.currentMessageID {
		e.currentMessageID = ""
	}
	return err
}

// TextMessage emits a complete text message (start + content + end).
func (e *Emitter) TextMessage(text string, role Role) (string, error) {
	id, err := e.TextStart(role, "")
	if err != nil {
		return "", err
	}
	if err := e.TextDelta(text, id); err != nil {
		return "", err
	}
	return id, e.TextEnd(id)
}

// ToolCallStart emits a TOOL_CALL_START event.
func (e *Emitter) ToolCallStart(name string, toolCallID string, parentMsgID string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if toolCallID == "" {
		toolCallID = uuid.New().String()
	}
	e.currentToolID = toolCallID

	return toolCallID, e.emit(ToolCallStartEvent{
		BaseEvent:       BaseEvent{Type: EventToolCallStart, Timestamp: now()},
		ToolCallID:      toolCallID,
		ToolCallName:    name,
		ParentMessageID: parentMsgID,
	})
}

// ToolCallArgs emits a TOOL_CALL_ARGS event.
func (e *Emitter) ToolCallArgs(delta string, toolCallID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if toolCallID == "" {
		toolCallID = e.currentToolID
	}
	return e.emit(ToolCallArgsEvent{
		BaseEvent:  BaseEvent{Type: EventToolCallArgs, Timestamp: now()},
		ToolCallID: toolCallID,
		Delta:      delta,
	})
}

// ToolCallEnd emits a TOOL_CALL_END event.
func (e *Emitter) ToolCallEnd(toolCallID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if toolCallID == "" {
		toolCallID = e.currentToolID
	}
	return e.emit(ToolCallEndEvent{
		BaseEvent:  BaseEvent{Type: EventToolCallEnd, Timestamp: now()},
		ToolCallID: toolCallID,
	})
}

// ToolCallResult emits a TOOL_CALL_RESULT event.
func (e *Emitter) ToolCallResult(result string, toolCallID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if toolCallID == "" {
		toolCallID = e.currentToolID
	}
	err := e.emit(ToolCallResultEvent{
		BaseEvent:  BaseEvent{Type: EventToolCallResult, Timestamp: now()},
		ToolCallID: toolCallID,
		Result:     result,
	})
	if toolCallID == e.currentToolID {
		e.currentToolID = ""
	}
	return err
}

// StateSnapshot emits a STATE_SNAPSHOT event.
func (e *Emitter) StateSnapshot(snapshot map[string]interface{}) error {
	return e.emit(StateSnapshotEvent{
		BaseEvent: BaseEvent{Type: EventStateSnapshot, Timestamp: now()},
		Snapshot:  snapshot,
	})
}

// StateDelta emits a STATE_DELTA event.
func (e *Emitter) StateDelta(delta []interface{}) error {
	return e.emit(StateDeltaEvent{
		BaseEvent: BaseEvent{Type: EventStateDelta, Timestamp: now()},
		Delta:     delta,
	})
}

// Custom emits a CUSTOM event.
func (e *Emitter) Custom(name string, value interface{}) error {
	return e.emit(CustomEvent{
		BaseEvent: BaseEvent{Type: EventCustom, Timestamp: now()},
		Name:      name,
		Value:     value,
	})
}

// ── Plugin ───────────────────────────────────────────────────────────────

// Config holds AG-UI plugin configuration.
type Config struct {
	Endpoint string
}

// Plugin implements the AG-UI SSE streaming endpoint.
type Plugin struct {
	cfg Config
}

// New creates a new AG-UI plugin.
func New() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Name() string { return "ag_ui" }

func (p *Plugin) Init(cfg map[string]interface{}) error {
	p.cfg.Endpoint = "/ag-ui"
	if v, ok := cfg["endpoint"].(string); ok && v != "" {
		p.cfg.Endpoint = v
	}

	slog.Info("ag_ui: initialized", "endpoint", p.cfg.Endpoint)
	return nil
}

func (p *Plugin) Close() error { return nil }

func (p *Plugin) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == p.cfg.Endpoint && r.Method == http.MethodPost {
				p.handleStream(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (p *Plugin) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		plugins.WriteError(w, http.StatusInternalServerError, "streaming_error",
			"Streaming not supported")
		return
	}

	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Create emitter.
	emitter := NewEmitter(func(data []byte) error {
		if _, err := w.Write(data); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}, EmitterOptions{})

	// Emit a basic run lifecycle — the gateway translates origin responses.
	if err := emitter.RunStarted(""); err != nil {
		return
	}

	// Read the request body to get the user's message.
	body, err := readBody(r)
	if err != nil {
		emitter.RunError("Failed to read request body", "parse_error")
		return
	}

	responseText := "Request received"
	if msg, ok := body["message"].(string); ok && msg != "" {
		responseText = fmt.Sprintf("Processing: %s", msg)
	}

	if _, err := emitter.TextMessage(responseText, RoleAssistant); err != nil {
		return
	}

	emitter.RunFinished(nil)
}

func readBody(r *http.Request) (map[string]interface{}, error) {
	var body map[string]interface{}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&body); err != nil {
		return map[string]interface{}{}, nil
	}
	return body, nil
}
