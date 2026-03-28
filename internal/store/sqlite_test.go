package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tempDB(t *testing.T) *SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func sampleEvent(id, agent, path string, status int) AgentEvent {
	return AgentEvent{
		ID:           id,
		Timestamp:    time.Now().UTC(),
		Agent:        agent,
		UserAgent:    "TestBot/1.0",
		Method:       "GET",
		Path:         path,
		StatusCode:   status,
		DurationMs:   12.5,
		ContentType:  "application/json",
		ResponseSize: 256,
	}
}

func TestSQLiteStore_SaveAndQueryEvents(t *testing.T) {
	s := tempDB(t)

	// Save events.
	require.NoError(t, s.SaveEvent(sampleEvent("e1", "ClaudeBot", "/api/v1", 200)))
	require.NoError(t, s.SaveEvent(sampleEvent("e2", "GPTBot", "/api/v2", 404)))
	require.NoError(t, s.SaveEvent(sampleEvent("e3", "ClaudeBot", "/api/v1", 500)))

	// Query all.
	events, err := s.QueryEvents(EventFilter{})
	require.NoError(t, err)
	assert.Len(t, events, 3)

	// Query by agent.
	events, err = s.QueryEvents(EventFilter{Agent: "ClaudeBot"})
	require.NoError(t, err)
	assert.Len(t, events, 2)

	// Query by status range.
	events, err = s.QueryEvents(EventFilter{MinStatus: 400})
	require.NoError(t, err)
	assert.Len(t, events, 2)

	// Query with limit.
	events, err = s.QueryEvents(EventFilter{Limit: 1})
	require.NoError(t, err)
	assert.Len(t, events, 1)
}

func TestSQLiteStore_GetMetrics(t *testing.T) {
	s := tempDB(t)

	now := time.Now().UTC()
	events := []AgentEvent{
		{ID: "m1", Timestamp: now, Agent: "ClaudeBot", Method: "GET", Path: "/a", StatusCode: 200, DurationMs: 10},
		{ID: "m2", Timestamp: now, Agent: "ClaudeBot", Method: "GET", Path: "/a", StatusCode: 200, DurationMs: 20},
		{ID: "m3", Timestamp: now, Agent: "GPTBot", Method: "POST", Path: "/b", StatusCode: 500, DurationMs: 30},
		{ID: "m4", Timestamp: now, Agent: "", Method: "GET", Path: "/c", StatusCode: 200, DurationMs: 40},
	}
	for _, e := range events {
		require.NoError(t, s.SaveEvent(e))
	}

	tr := TimeRange{From: now.Add(-time.Minute), To: now.Add(time.Minute)}
	m, err := s.GetMetrics(tr)
	require.NoError(t, err)

	assert.Equal(t, int64(4), m.TotalRequests)
	assert.Equal(t, 3, m.UniqueAgents) // "ClaudeBot", "GPTBot", ""
	assert.InDelta(t, 25.0, m.AvgLatencyMs, 0.01)
	assert.InDelta(t, 0.25, m.ErrorRate, 0.01)

	// Top agents.
	require.NotEmpty(t, m.TopAgents)
	assert.Equal(t, "ClaudeBot", m.TopAgents[0].Agent)
	assert.Equal(t, int64(2), m.TopAgents[0].Count)

	// Status distribution.
	assert.Equal(t, int64(3), m.StatusDistribution[200])
	assert.Equal(t, int64(1), m.StatusDistribution[500])
}

func TestSQLiteStore_ConfigCRUD(t *testing.T) {
	s := tempDB(t)

	// Get non-existent key.
	val, err := s.GetConfig("missing")
	require.NoError(t, err)
	assert.Equal(t, "", val)

	// Save and get.
	require.NoError(t, s.SaveConfig("theme", "dark"))
	val, err = s.GetConfig("theme")
	require.NoError(t, err)
	assert.Equal(t, "dark", val)

	// Upsert.
	require.NoError(t, s.SaveConfig("theme", "light"))
	val, err = s.GetConfig("theme")
	require.NoError(t, err)
	assert.Equal(t, "light", val)
}

func TestSQLiteStore_Cleanup(t *testing.T) {
	s := tempDB(t)

	old := AgentEvent{
		ID:        "old1",
		Timestamp: time.Now().UTC().Add(-60 * 24 * time.Hour), // 60 days ago
		Agent:     "OldBot",
		Method:    "GET",
		Path:      "/old",
	}
	recent := sampleEvent("new1", "NewBot", "/new", 200)

	require.NoError(t, s.SaveEvent(old))
	require.NoError(t, s.SaveEvent(recent))

	deleted, err := s.Cleanup(30 * 24 * time.Hour) // 30 day retention
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	events, err := s.QueryEvents(EventFilter{})
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, "new1", events[0].ID)
}

func TestSQLiteStore_AgentsTable(t *testing.T) {
	s := tempDB(t)

	// Save two events for same agent.
	require.NoError(t, s.SaveEvent(sampleEvent("a1", "ClaudeBot", "/x", 200)))
	require.NoError(t, s.SaveEvent(sampleEvent("a2", "ClaudeBot", "/y", 200)))

	// Check agents table via direct query.
	var name string
	var total int64
	err := s.db.QueryRow(`SELECT name, total_requests FROM agents WHERE name = ?`, "ClaudeBot").Scan(&name, &total)
	require.NoError(t, err)
	assert.Equal(t, "ClaudeBot", name)
	assert.Equal(t, int64(2), total)
}

func TestNewSQLiteStore_InvalidPath(t *testing.T) {
	_, err := NewSQLiteStore(filepath.Join(os.DevNull, "impossible", "test.db"))
	assert.Error(t, err)
}
