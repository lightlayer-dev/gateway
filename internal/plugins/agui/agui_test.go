package agui

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeEvent(t *testing.T) {
	event := RunStartedEvent{
		BaseEvent: BaseEvent{Type: EventRunStarted, Timestamp: 1700000000000},
		ThreadID:  "thread-1",
		RunID:     "run-1",
	}

	data, err := EncodeEvent(event)
	require.NoError(t, err)

	str := string(data)
	assert.Contains(t, str, "event: RUN_STARTED\n")
	assert.Contains(t, str, "data: ")
	assert.Contains(t, str, `"threadId":"thread-1"`)
	assert.Contains(t, str, `"runId":"run-1"`)
	assert.True(t, strings.HasSuffix(str, "\n\n"))
}

func TestEmitterLifecycle(t *testing.T) {
	var buf bytes.Buffer
	emitter := NewEmitter(func(data []byte) error {
		buf.Write(data)
		return nil
	}, EmitterOptions{ThreadID: "t1", RunID: "r1"})

	assert.Equal(t, "t1", emitter.ThreadID())
	assert.Equal(t, "r1", emitter.RunID())

	// Full lifecycle.
	require.NoError(t, emitter.RunStarted(""))

	msgID, err := emitter.TextStart(RoleAssistant, "")
	require.NoError(t, err)
	assert.NotEmpty(t, msgID)

	require.NoError(t, emitter.TextDelta("Hello ", ""))
	require.NoError(t, emitter.TextDelta("world!", ""))
	require.NoError(t, emitter.TextEnd(""))
	require.NoError(t, emitter.RunFinished(nil))

	output := buf.String()
	assert.Contains(t, output, "event: RUN_STARTED")
	assert.Contains(t, output, "event: TEXT_MESSAGE_START")
	assert.Contains(t, output, "event: TEXT_MESSAGE_CONTENT")
	assert.Contains(t, output, "event: TEXT_MESSAGE_END")
	assert.Contains(t, output, "event: RUN_FINISHED")
}

func TestEmitterToolCall(t *testing.T) {
	var buf bytes.Buffer
	emitter := NewEmitter(func(data []byte) error {
		buf.Write(data)
		return nil
	}, EmitterOptions{})

	toolID, err := emitter.ToolCallStart("search", "", "")
	require.NoError(t, err)
	assert.NotEmpty(t, toolID)

	require.NoError(t, emitter.ToolCallArgs(`{"query":"test"}`, ""))
	require.NoError(t, emitter.ToolCallEnd(""))
	require.NoError(t, emitter.ToolCallResult(`[{"id":1}]`, ""))

	output := buf.String()
	assert.Contains(t, output, "event: TOOL_CALL_START")
	assert.Contains(t, output, "event: TOOL_CALL_ARGS")
	assert.Contains(t, output, "event: TOOL_CALL_END")
	assert.Contains(t, output, "event: TOOL_CALL_RESULT")
}

func TestEmitterStateAndCustom(t *testing.T) {
	var buf bytes.Buffer
	emitter := NewEmitter(func(data []byte) error {
		buf.Write(data)
		return nil
	}, EmitterOptions{})

	require.NoError(t, emitter.StateSnapshot(map[string]interface{}{"count": 42}))
	require.NoError(t, emitter.StateDelta([]interface{}{
		map[string]interface{}{"op": "replace", "path": "/count", "value": 43},
	}))
	require.NoError(t, emitter.Custom("heartbeat", map[string]interface{}{"alive": true}))

	output := buf.String()
	assert.Contains(t, output, "event: STATE_SNAPSHOT")
	assert.Contains(t, output, "event: STATE_DELTA")
	assert.Contains(t, output, "event: CUSTOM")
	assert.Contains(t, output, `"count":42`)
}

func TestEmitterTextMessage(t *testing.T) {
	var buf bytes.Buffer
	emitter := NewEmitter(func(data []byte) error {
		buf.Write(data)
		return nil
	}, EmitterOptions{})

	id, err := emitter.TextMessage("Complete message", RoleAssistant)
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	output := buf.String()
	assert.Contains(t, output, "TEXT_MESSAGE_START")
	assert.Contains(t, output, "TEXT_MESSAGE_CONTENT")
	assert.Contains(t, output, "TEXT_MESSAGE_END")
	assert.Contains(t, output, "Complete message")
}

func TestPluginSSEHeaders(t *testing.T) {
	p := New()
	require.NoError(t, p.Init(map[string]interface{}{"endpoint": "/ag-ui"}))

	handler := p.Middleware()(http.NotFoundHandler())

	body, _ := json.Marshal(map[string]interface{}{"message": "hello"})
	req := httptest.NewRequest(http.MethodPost, "/ag-ui", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	result := rec.Result()
	assert.Equal(t, "text/event-stream", result.Header.Get("Content-Type"))
	assert.Equal(t, "no-cache, no-transform", result.Header.Get("Cache-Control"))
	assert.Equal(t, "keep-alive", result.Header.Get("Connection"))
	assert.Equal(t, "no", result.Header.Get("X-Accel-Buffering"))

	sseBody, _ := io.ReadAll(result.Body)
	sseStr := string(sseBody)
	assert.Contains(t, sseStr, "event: RUN_STARTED")
	assert.Contains(t, sseStr, "event: TEXT_MESSAGE_START")
	assert.Contains(t, sseStr, "event: RUN_FINISHED")
	assert.Contains(t, sseStr, "Processing: hello")
}

func TestPluginNonAgUIPathPassesThrough(t *testing.T) {
	p := New()
	require.NoError(t, p.Init(map[string]interface{}{}))

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := p.Middleware()(next)

	req := httptest.NewRequest(http.MethodGet, "/other", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.True(t, called)
}

func TestEmitterRunError(t *testing.T) {
	var buf bytes.Buffer
	emitter := NewEmitter(func(data []byte) error {
		buf.Write(data)
		return nil
	}, EmitterOptions{})

	require.NoError(t, emitter.RunStarted(""))
	require.NoError(t, emitter.RunError("something went wrong", "internal_error"))

	output := buf.String()
	assert.Contains(t, output, "event: RUN_ERROR")
	assert.Contains(t, output, "something went wrong")
}

func TestEmitterStepEvents(t *testing.T) {
	var buf bytes.Buffer
	emitter := NewEmitter(func(data []byte) error {
		buf.Write(data)
		return nil
	}, EmitterOptions{})

	require.NoError(t, emitter.StepStarted("fetch_data"))
	require.NoError(t, emitter.StepFinished("fetch_data"))

	output := buf.String()
	assert.Contains(t, output, "event: STEP_STARTED")
	assert.Contains(t, output, "event: STEP_FINISHED")
	assert.Contains(t, output, "fetch_data")
}
