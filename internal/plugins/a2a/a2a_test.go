package a2a

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func initPlugin(t *testing.T) *Plugin {
	t.Helper()
	p := New()
	err := p.Init(map[string]interface{}{
		"endpoint":  "/a2a",
		"streaming": true,
		"max_tasks": 100,
		"task_ttl":  "1h",
	})
	require.NoError(t, err)
	t.Cleanup(func() { p.Close() })
	return p
}

func rpcRequest(method string, params interface{}) []byte {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	}
	data, _ := json.Marshal(req)
	return data
}

func doRPC(t *testing.T, handler http.Handler, body []byte) *JSONRPCResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var resp JSONRPCResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return &resp
}

func TestMessageSendRoundTrip(t *testing.T) {
	p := initPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	body := rpcRequest("message/send", map[string]interface{}{
		"message": map[string]interface{}{
			"role": "user",
			"parts": []map[string]interface{}{
				{"type": "text", "text": "Hello A2A"},
			},
		},
	})

	resp := doRPC(t, handler, body)
	assert.Nil(t, resp.Error, "expected no error")
	assert.NotNil(t, resp.Result)

	// Result should be a Task.
	taskData, _ := json.Marshal(resp.Result)
	var task Task
	require.NoError(t, json.Unmarshal(taskData, &task))

	assert.NotEmpty(t, task.ID)
	assert.Equal(t, TaskStateCompleted, task.Status.State)
	assert.GreaterOrEqual(t, len(task.Messages), 2) // user msg + agent response
}

func TestTaskLifecycleStateTransitions(t *testing.T) {
	store, err := NewTaskStore(100, time.Hour, "")
	require.NoError(t, err)

	msg := Message{
		Role:  "user",
		Parts: []Part{{Type: "text", Text: "test"}},
	}

	// Create → submitted.
	task, err := store.CreateTask("ctx-1", msg)
	require.NoError(t, err)
	assert.Equal(t, TaskStateSubmitted, task.Status.State)
	assert.Equal(t, "ctx-1", task.ContextID)

	// Transition to working.
	task, err = store.UpdateStatus(task.ID, TaskStatus{State: TaskStateWorking})
	require.NoError(t, err)
	assert.Equal(t, TaskStateWorking, task.Status.State)

	// Add artifact.
	task, err = store.AddArtifact(task.ID, Artifact{
		Name:  "result",
		Parts: []Part{{Type: "text", Text: "output"}},
	})
	require.NoError(t, err)
	assert.Len(t, task.Artifacts, 1)
	assert.Equal(t, "result", task.Artifacts[0].Name)

	// Complete.
	task, err = store.UpdateStatus(task.ID, TaskStatus{State: TaskStateCompleted})
	require.NoError(t, err)
	assert.Equal(t, TaskStateCompleted, task.Status.State)
	assert.True(t, task.Status.State.IsTerminal())
}

func TestTasksListWithFilters(t *testing.T) {
	store, err := NewTaskStore(100, time.Hour, "")
	require.NoError(t, err)

	msg := Message{Role: "user", Parts: []Part{{Type: "text", Text: "test"}}}

	// Create tasks with different contexts.
	t1, _ := store.CreateTask("ctx-a", msg)
	t2, _ := store.CreateTask("ctx-b", msg)
	store.CreateTask("ctx-a", msg)

	// Complete t1.
	store.UpdateStatus(t1.ID, TaskStatus{State: TaskStateCompleted})

	// Filter by context.
	tasks := store.ListTasks(TaskFilter{ContextID: "ctx-a"})
	assert.Len(t, tasks, 2)

	tasks = store.ListTasks(TaskFilter{ContextID: "ctx-b"})
	assert.Len(t, tasks, 1)
	assert.Equal(t, t2.ID, tasks[0].ID)

	// Filter by status.
	tasks = store.ListTasks(TaskFilter{Status: TaskStateCompleted})
	assert.Len(t, tasks, 1)
	assert.Equal(t, t1.ID, tasks[0].ID)

	// Pagination.
	tasks = store.ListTasks(TaskFilter{Limit: 2})
	assert.Len(t, tasks, 2)
}

func TestTasksGetAndCancel(t *testing.T) {
	p := initPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	// Create a task via message/send.
	body := rpcRequest("message/send", map[string]interface{}{
		"message": map[string]interface{}{
			"role":  "user",
			"parts": []map[string]interface{}{{"type": "text", "text": "hi"}},
		},
	})
	resp := doRPC(t, handler, body)
	taskData, _ := json.Marshal(resp.Result)
	var task Task
	json.Unmarshal(taskData, &task)

	// tasks/get.
	getBody := rpcRequest("tasks/get", map[string]interface{}{"id": task.ID})
	getResp := doRPC(t, handler, getBody)
	assert.Nil(t, getResp.Error)

	// tasks/cancel — should fail because task is already completed.
	cancelBody := rpcRequest("tasks/cancel", map[string]interface{}{"id": task.ID})
	cancelResp := doRPC(t, handler, cancelBody)
	assert.NotNil(t, cancelResp.Error)
	assert.Equal(t, errTaskNotCancelable, cancelResp.Error.Code)
}

func TestTasksGetNotFound(t *testing.T) {
	p := initPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	body := rpcRequest("tasks/get", map[string]interface{}{"id": "nonexistent"})
	resp := doRPC(t, handler, body)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, errTaskNotFound, resp.Error.Code)
}

func TestMethodNotFound(t *testing.T) {
	p := initPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	body := rpcRequest("unknown/method", nil)
	resp := doRPC(t, handler, body)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, errMethodNotFound, resp.Error.Code)
}

func TestStreamingSSEEvents(t *testing.T) {
	p := initPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	body := rpcRequest("message/stream", map[string]interface{}{
		"message": map[string]interface{}{
			"role":  "user",
			"parts": []map[string]interface{}{{"type": "text", "text": "stream test"}},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	result := rec.Result()
	assert.Equal(t, "text/event-stream", result.Header.Get("Content-Type"))
	assert.Equal(t, "no-cache, no-transform", result.Header.Get("Cache-Control"))
	assert.Equal(t, "no", result.Header.Get("X-Accel-Buffering"))

	// Body should contain SSE events.
	sseBody, _ := io.ReadAll(result.Body)
	sseStr := string(sseBody)
	assert.Contains(t, sseStr, "event: TaskStatusUpdateEvent")
	assert.Contains(t, sseStr, "\"final\":true")
}

func TestNonA2APathPassesThrough(t *testing.T) {
	p := initPlugin(t)
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

func TestPushNotification(t *testing.T) {
	// Set up a mock webhook server.
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	pn := NewPushNotifier(srv.URL)
	pn.NotifyStatus("task-1", TaskStatus{
		State: TaskStateCompleted,
	})

	// Give the async HTTP call a moment.
	time.Sleep(100 * time.Millisecond)
	assert.NotEmpty(t, received)
	assert.True(t, strings.Contains(string(received), "TaskStatusUpdateEvent"))
}

func TestTaskCleanup(t *testing.T) {
	store, err := NewTaskStore(100, 1*time.Millisecond, "")
	require.NoError(t, err)

	msg := Message{Role: "user", Parts: []Part{{Type: "text", Text: "test"}}}
	task, _ := store.CreateTask("", msg)
	store.UpdateStatus(task.ID, TaskStatus{State: TaskStateCompleted})

	// Wait for TTL to expire.
	time.Sleep(10 * time.Millisecond)

	removed := store.Cleanup()
	assert.Equal(t, 1, removed)

	_, ok := store.GetTask(task.ID)
	assert.False(t, ok)
}

func TestInvalidJSON(t *testing.T) {
	p := initPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewReader([]byte("not json")))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var resp JSONRPCResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, errParseError, resp.Error.Code)
}
