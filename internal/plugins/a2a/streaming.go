package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// ── SSE Event Types ──────────────────────────────────────────────────────

// TaskStatusUpdateEvent is sent when a task's status changes.
type TaskStatusUpdateEvent struct {
	ID     string     `json:"id"`
	Status TaskStatus `json:"status"`
	Final  bool       `json:"final"`
}

// TaskArtifactUpdateEvent is sent when an artifact is added/updated.
type TaskArtifactUpdateEvent struct {
	ID       string   `json:"id"`
	Artifact Artifact `json:"artifact"`
}

// ── SSE Writer ───────────────────────────────────────────────────────────

// SSEWriter writes Server-Sent Events to an http.ResponseWriter.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewSSEWriter creates an SSE writer and sets appropriate headers.
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	f.Flush()

	return &SSEWriter{w: w, flusher: f}, nil
}

// WriteEvent writes a named SSE event with JSON data.
func (s *SSEWriter) WriteEvent(eventType string, data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "event: %s\n", eventType)
	fmt.Fprintf(&buf, "data: %s\n\n", jsonData)

	if _, err := s.w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write event: %w", err)
	}
	s.flusher.Flush()
	return nil
}

// StreamTask streams task events via SSE until the task reaches a terminal state.
func StreamTask(ctx context.Context, store *TaskStore, taskID string, sse *SSEWriter) error {
	// Subscribe BEFORE reading state to avoid missing events from concurrent updates.
	ch := store.Subscribe(taskID)
	defer store.Unsubscribe(taskID, ch)

	// Send current state first.
	task, ok := store.GetTask(taskID)
	if !ok {
		return fmt.Errorf("task not found: %s", taskID)
	}

	if err := sse.WriteEvent("TaskStatusUpdateEvent", TaskStatusUpdateEvent{
		ID:     taskID,
		Status: task.Status,
		Final:  task.Status.State.IsTerminal(),
	}); err != nil {
		return err
	}

	// Send existing artifacts.
	for _, a := range task.Artifacts {
		if err := sse.WriteEvent("TaskArtifactUpdateEvent", TaskArtifactUpdateEvent{
			ID:       taskID,
			Artifact: a,
		}); err != nil {
			return err
		}
	}

	// If already terminal, done.
	if task.Status.State.IsTerminal() {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-ch:
			if !ok {
				return nil
			}
			switch event.Type {
			case "status":
				if err := sse.WriteEvent("TaskStatusUpdateEvent", TaskStatusUpdateEvent{
					ID:     taskID,
					Status: *event.Status,
					Final:  event.Status.State.IsTerminal(),
				}); err != nil {
					return err
				}
				if event.Status.State.IsTerminal() {
					return nil
				}
			case "artifact":
				if err := sse.WriteEvent("TaskArtifactUpdateEvent", TaskArtifactUpdateEvent{
					ID:       taskID,
					Artifact: *event.Artifact,
				}); err != nil {
					return err
				}
			}
		}
	}
}

// ── Push Notifications ───────────────────────────────────────────────────

// PushNotifier delivers task events to a webhook URL.
type PushNotifier struct {
	client  *http.Client
	pushURL string
}

// NewPushNotifier creates a push notifier.
func NewPushNotifier(pushURL string) *PushNotifier {
	return &PushNotifier{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		pushURL: pushURL,
	}
}

// NotifyStatus sends a task status update to the configured push URL.
func (pn *PushNotifier) NotifyStatus(taskID string, status TaskStatus) {
	event := TaskStatusUpdateEvent{
		ID:     taskID,
		Status: status,
		Final:  status.State.IsTerminal(),
	}
	pn.send("TaskStatusUpdateEvent", event)
}

// NotifyArtifact sends a task artifact update to the configured push URL.
func (pn *PushNotifier) NotifyArtifact(taskID string, artifact Artifact) {
	event := TaskArtifactUpdateEvent{
		ID:       taskID,
		Artifact: artifact,
	}
	pn.send("TaskArtifactUpdateEvent", event)
}

func (pn *PushNotifier) send(eventType string, payload interface{}) {
	body, err := json.Marshal(map[string]interface{}{
		"type": eventType,
		"data": payload,
	})
	if err != nil {
		slog.Warn("a2a: push marshal failed", "error", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, pn.pushURL, bytes.NewReader(body))
	if err != nil {
		slog.Warn("a2a: push request create failed", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := pn.client.Do(req)
	if err != nil {
		slog.Warn("a2a: push delivery failed", "url", pn.pushURL, "error", err)
		return
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		slog.Warn("a2a: push delivery rejected", "url", pn.pushURL, "status", resp.StatusCode)
	}
}
