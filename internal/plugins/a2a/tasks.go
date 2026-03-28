// Package a2a implements the A2A v1.0 protocol task lifecycle.
//
// Task states: submitted → working → completed | failed | canceled | rejected
// Tasks hold messages, artifacts, and metadata with automatic TTL cleanup.
package a2a

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// ── Task State ───────────────────────────────────────────────────────────

// TaskState represents the lifecycle state of an A2A task.
type TaskState string

const (
	TaskStateSubmitted TaskState = "submitted"
	TaskStateWorking   TaskState = "working"
	TaskStateCompleted TaskState = "completed"
	TaskStateFailed    TaskState = "failed"
	TaskStateCanceled  TaskState = "canceled"
	TaskStateRejected  TaskState = "rejected"
)

// IsTerminal returns true if the state is a final state.
func (s TaskState) IsTerminal() bool {
	return s == TaskStateCompleted || s == TaskStateFailed ||
		s == TaskStateCanceled || s == TaskStateRejected
}

// ── Parts ────────────────────────────────────────────────────────────────

// TextPart holds plain text content.
type TextPart struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
}

// FilePart holds a file reference.
type FilePart struct {
	Type     string `json:"type"` // "file"
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	URI      string `json:"uri,omitempty"`
	Data     string `json:"data,omitempty"` // base64-encoded inline data
}

// DataPart holds structured JSON data.
type DataPart struct {
	Type string      `json:"type"` // "data"
	Data interface{} `json:"data"`
}

// Part is a union type for message/artifact content.
type Part struct {
	Type     string      `json:"type"`
	Text     string      `json:"text,omitempty"`
	Name     string      `json:"name,omitempty"`
	MimeType string      `json:"mimeType,omitempty"`
	URI      string      `json:"uri,omitempty"`
	Data     interface{} `json:"data,omitempty"`
}

// ── Message ──────────────────────────────────────────────────────────────

// Message represents a message in the A2A protocol.
type Message struct {
	Role       string                 `json:"role"` // "user" or "agent"
	Parts      []Part                 `json:"parts"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	Extensions map[string]interface{} `json:"extensions,omitempty"`
}

// ── Artifact ─────────────────────────────────────────────────────────────

// Artifact is a named output with parts.
type Artifact struct {
	Name        string                 `json:"name,omitempty"`
	Description string                 `json:"description,omitempty"`
	Parts       []Part                 `json:"parts"`
	Index       int                    `json:"index"`
	Append      bool                   `json:"append,omitempty"`
	LastChunk   bool                   `json:"lastChunk,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ── Task Status ──────────────────────────────────────────────────────────

// TaskStatus holds the current status of a task.
type TaskStatus struct {
	State     TaskState `json:"state"`
	Message   *Message  `json:"message,omitempty"`
	Timestamp string    `json:"timestamp,omitempty"`
}

// ── Task ─────────────────────────────────────────────────────────────────

// Task is the core A2A task object.
type Task struct {
	ID        string                 `json:"id"`
	ContextID string                 `json:"contextId,omitempty"`
	Status    TaskStatus             `json:"status"`
	Messages  []Message              `json:"messages,omitempty"`
	Artifacts []Artifact             `json:"artifacts,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt string                 `json:"createdAt"`
	UpdatedAt string                 `json:"updatedAt"`
}

// ── Task Store ───────────────────────────────────────────────────────────

// TaskStore manages in-memory tasks with optional SQLite persistence.
type TaskStore struct {
	mu       sync.RWMutex
	tasks    map[string]*Task
	maxTasks int
	ttl      time.Duration
	db       *sql.DB

	// subscribers maps taskID → list of channels for status updates.
	subscribers   map[string][]chan TaskEvent
	subscribersMu sync.RWMutex
}

// TaskEvent is emitted on task state changes.
type TaskEvent struct {
	Type     string    `json:"type"` // "status" or "artifact"
	TaskID   string    `json:"taskId"`
	Status   *TaskStatus `json:"status,omitempty"`
	Artifact *Artifact   `json:"artifact,omitempty"`
}

// TaskFilter specifies criteria for listing tasks.
type TaskFilter struct {
	ContextID string
	Status    TaskState
	Limit     int
	Offset    int
}

// NewTaskStore creates a new task store.
func NewTaskStore(maxTasks int, ttl time.Duration, dbPath string) (*TaskStore, error) {
	ts := &TaskStore{
		tasks:       make(map[string]*Task),
		maxTasks:    maxTasks,
		ttl:         ttl,
		subscribers: make(map[string][]chan TaskEvent),
	}

	if dbPath != "" {
		db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
		if err != nil {
			return nil, fmt.Errorf("open task db: %w", err)
		}
		db.SetMaxOpenConns(1)
		ts.db = db
		if err := ts.migrate(); err != nil {
			db.Close()
			return nil, fmt.Errorf("migrate task db: %w", err)
		}
		if err := ts.loadFromDB(); err != nil {
			slog.Warn("a2a: failed to load tasks from db", "error", err)
		}
	}

	return ts, nil
}

func (ts *TaskStore) migrate() error {
	_, err := ts.db.Exec(`CREATE TABLE IF NOT EXISTS a2a_tasks (
		id         TEXT PRIMARY KEY,
		context_id TEXT NOT NULL DEFAULT '',
		data       TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	_, err = ts.db.Exec(`CREATE INDEX IF NOT EXISTS idx_a2a_tasks_context ON a2a_tasks(context_id)`)
	if err != nil {
		return err
	}
	_, err = ts.db.Exec(`CREATE INDEX IF NOT EXISTS idx_a2a_tasks_updated ON a2a_tasks(updated_at)`)
	return err
}

func (ts *TaskStore) loadFromDB() error {
	if ts.db == nil {
		return nil
	}
	rows, err := ts.db.Query(`SELECT data FROM a2a_tasks ORDER BY updated_at DESC LIMIT ?`, ts.maxTasks)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			continue
		}
		var t Task
		if err := json.Unmarshal([]byte(data), &t); err != nil {
			continue
		}
		ts.tasks[t.ID] = &t
	}
	return rows.Err()
}

func (ts *TaskStore) persistTask(t *Task) {
	if ts.db == nil {
		return
	}
	data, err := json.Marshal(t)
	if err != nil {
		return
	}
	_, err = ts.db.Exec(`INSERT INTO a2a_tasks (id, context_id, data, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET data = ?, updated_at = ?`,
		t.ID, t.ContextID, string(data), t.CreatedAt, t.UpdatedAt, string(data), t.UpdatedAt)
	if err != nil {
		slog.Warn("a2a: persist task failed", "id", t.ID, "error", err)
	}
}

// CreateTask creates a new task from a user message.
func (ts *TaskStore) CreateTask(contextID string, msg Message) (*Task, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if len(ts.tasks) >= ts.maxTasks {
		// Evict oldest completed task.
		if !ts.evictOne() {
			return nil, fmt.Errorf("max tasks reached (%d)", ts.maxTasks)
		}
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	t := &Task{
		ID:        uuid.New().String(),
		ContextID: contextID,
		Status: TaskStatus{
			State:     TaskStateSubmitted,
			Timestamp: now,
		},
		Messages:  []Message{msg},
		Artifacts: []Artifact{},
		CreatedAt: now,
		UpdatedAt: now,
	}

	ts.tasks[t.ID] = t
	ts.persistTask(t)
	return t, nil
}

// GetTask retrieves a task by ID.
func (ts *TaskStore) GetTask(id string) (*Task, bool) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	t, ok := ts.tasks[id]
	return t, ok
}

// UpdateStatus transitions task state and notifies subscribers.
func (ts *TaskStore) UpdateStatus(id string, status TaskStatus) (*Task, error) {
	ts.mu.Lock()
	t, ok := ts.tasks[id]
	if !ok {
		ts.mu.Unlock()
		return nil, fmt.Errorf("task not found: %s", id)
	}
	t.Status = status
	t.Status.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	t.UpdatedAt = t.Status.Timestamp
	if status.Message != nil {
		t.Messages = append(t.Messages, *status.Message)
	}
	ts.persistTask(t)
	ts.mu.Unlock()

	ts.notify(id, TaskEvent{
		Type:   "status",
		TaskID: id,
		Status: &t.Status,
	})

	return t, nil
}

// AddArtifact adds an artifact to a task and notifies subscribers.
func (ts *TaskStore) AddArtifact(id string, artifact Artifact) (*Task, error) {
	ts.mu.Lock()
	t, ok := ts.tasks[id]
	if !ok {
		ts.mu.Unlock()
		return nil, fmt.Errorf("task not found: %s", id)
	}
	artifact.Index = len(t.Artifacts)
	t.Artifacts = append(t.Artifacts, artifact)
	t.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	ts.persistTask(t)
	ts.mu.Unlock()

	ts.notify(id, TaskEvent{
		Type:     "artifact",
		TaskID:   id,
		Artifact: &artifact,
	})

	return t, nil
}

// CancelTask transitions task to canceled state.
func (ts *TaskStore) CancelTask(id string) (*Task, error) {
	return ts.UpdateStatus(id, TaskStatus{State: TaskStateCanceled})
}

// ListTasks returns tasks matching the filter.
func (ts *TaskStore) ListTasks(filter TaskFilter) []*Task {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	var result []*Task
	for _, t := range ts.tasks {
		if filter.ContextID != "" && t.ContextID != filter.ContextID {
			continue
		}
		if filter.Status != "" && t.Status.State != filter.Status {
			continue
		}
		result = append(result, t)
	}

	// Sort by CreatedAt descending.
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].CreatedAt > result[i].CreatedAt {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	if filter.Offset > 0 && filter.Offset < len(result) {
		result = result[filter.Offset:]
	} else if filter.Offset >= len(result) {
		return nil
	}
	if filter.Limit > 0 && filter.Limit < len(result) {
		result = result[:filter.Limit]
	}
	return result
}

// Subscribe returns a channel that receives events for a task.
func (ts *TaskStore) Subscribe(taskID string) chan TaskEvent {
	ch := make(chan TaskEvent, 32)
	ts.subscribersMu.Lock()
	ts.subscribers[taskID] = append(ts.subscribers[taskID], ch)
	ts.subscribersMu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel for a task.
func (ts *TaskStore) Unsubscribe(taskID string, ch chan TaskEvent) {
	ts.subscribersMu.Lock()
	defer ts.subscribersMu.Unlock()
	subs := ts.subscribers[taskID]
	for i, sub := range subs {
		if sub == ch {
			ts.subscribers[taskID] = append(subs[:i], subs[i+1:]...)
			close(ch)
			return
		}
	}
}

func (ts *TaskStore) notify(taskID string, event TaskEvent) {
	ts.subscribersMu.RLock()
	subs := ts.subscribers[taskID]
	ts.subscribersMu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- event:
		default:
			// Slow consumer — drop event to avoid blocking.
		}
	}
}

// evictOne removes the oldest terminal task. Returns false if none found.
func (ts *TaskStore) evictOne() bool {
	var oldest *Task
	for _, t := range ts.tasks {
		if !t.Status.State.IsTerminal() {
			continue
		}
		if oldest == nil || t.UpdatedAt < oldest.UpdatedAt {
			oldest = t
		}
	}
	if oldest == nil {
		return false
	}
	delete(ts.tasks, oldest.ID)
	if ts.db != nil {
		ts.db.Exec(`DELETE FROM a2a_tasks WHERE id = ?`, oldest.ID)
	}
	return true
}

// Cleanup removes completed tasks older than the TTL.
func (ts *TaskStore) Cleanup() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	cutoff := time.Now().Add(-ts.ttl)
	var removed int
	for id, t := range ts.tasks {
		if !t.Status.State.IsTerminal() {
			continue
		}
		updated, err := time.Parse(time.RFC3339Nano, t.UpdatedAt)
		if err != nil {
			continue
		}
		if updated.Before(cutoff) {
			delete(ts.tasks, id)
			if ts.db != nil {
				ts.db.Exec(`DELETE FROM a2a_tasks WHERE id = ?`, id)
			}
			removed++
		}
	}
	return removed
}

// Close cleans up resources.
func (ts *TaskStore) Close() error {
	if ts.db != nil {
		return ts.db.Close()
	}
	return nil
}
