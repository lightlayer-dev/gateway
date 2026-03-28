package store

import (
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements Store using a pure-Go SQLite driver (no CGO).
type SQLiteStore struct {
	db *sql.DB
	mu sync.Mutex // serialise writes for WAL mode safety
}

// NewSQLiteStore opens (or creates) a SQLite database at path and
// initialises the schema.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Single writer, multiple readers.
	db.SetMaxOpenConns(1)

	s := &SQLiteStore{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// migrate creates tables and indexes if they don't exist.
func (s *SQLiteStore) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS events (
			id            TEXT PRIMARY KEY,
			timestamp     TEXT NOT NULL,
			agent         TEXT NOT NULL DEFAULT '',
			user_agent    TEXT NOT NULL DEFAULT '',
			method        TEXT NOT NULL DEFAULT '',
			path          TEXT NOT NULL DEFAULT '',
			status_code   INTEGER NOT NULL DEFAULT 0,
			duration_ms   REAL NOT NULL DEFAULT 0,
			content_type  TEXT NOT NULL DEFAULT '',
			response_size INTEGER NOT NULL DEFAULT 0,
			identity_info TEXT NOT NULL DEFAULT '',
			payment_info  TEXT NOT NULL DEFAULT '',
			rate_limit    TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_events_agent ON events(agent)`,
		`CREATE INDEX IF NOT EXISTS idx_events_path ON events(path)`,

		`CREATE TABLE IF NOT EXISTS agents (
			name           TEXT PRIMARY KEY,
			first_seen     TEXT NOT NULL,
			last_seen      TEXT NOT NULL,
			total_requests INTEGER NOT NULL DEFAULT 0,
			verified       INTEGER NOT NULL DEFAULT 0
		)`,

		`CREATE TABLE IF NOT EXISTS config (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT ''
		)`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt[:40], err)
		}
	}
	return nil
}

// SaveEvent inserts an event and upserts the agents table.
func (s *SQLiteStore) SaveEvent(event AgentEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ts := event.Timestamp.UTC().Format(time.RFC3339Nano)

	_, err := s.db.Exec(`INSERT INTO events
		(id, timestamp, agent, user_agent, method, path, status_code, duration_ms, content_type, response_size, identity_info, payment_info, rate_limit)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, ts, event.Agent, event.UserAgent, event.Method, event.Path,
		event.StatusCode, event.DurationMs, event.ContentType, event.ResponseSize,
		event.IdentityInfo, event.PaymentInfo, event.RateLimit,
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}

	// Upsert agent record.
	if event.Agent != "" {
		_, err = s.db.Exec(`INSERT INTO agents (name, first_seen, last_seen, total_requests, verified)
			VALUES (?, ?, ?, 1, 0)
			ON CONFLICT(name) DO UPDATE SET
				last_seen = ?,
				total_requests = total_requests + 1`,
			event.Agent, ts, ts, ts,
		)
		if err != nil {
			slog.Warn("upsert agent failed", "agent", event.Agent, "error", err)
		}
	}

	return nil
}

// QueryEvents returns events matching the filter.
func (s *SQLiteStore) QueryEvents(filter EventFilter) ([]AgentEvent, error) {
	query := `SELECT id, timestamp, agent, user_agent, method, path, status_code,
		duration_ms, content_type, response_size, identity_info, payment_info, rate_limit
		FROM events WHERE 1=1`
	var args []interface{}

	if filter.Agent != "" {
		query += " AND agent = ?"
		args = append(args, filter.Agent)
	}
	if filter.Method != "" {
		query += " AND method = ?"
		args = append(args, filter.Method)
	}
	if filter.Path != "" {
		query += " AND path = ?"
		args = append(args, filter.Path)
	}
	if filter.MinStatus > 0 {
		query += " AND status_code >= ?"
		args = append(args, filter.MinStatus)
	}
	if filter.MaxStatus > 0 {
		query += " AND status_code <= ?"
		args = append(args, filter.MaxStatus)
	}
	if !filter.From.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, filter.From.UTC().Format(time.RFC3339Nano))
	}
	if !filter.To.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, filter.To.UTC().Format(time.RFC3339Nano))
	}

	query += " ORDER BY timestamp DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []AgentEvent
	for rows.Next() {
		var e AgentEvent
		var ts string
		if err := rows.Scan(&e.ID, &ts, &e.Agent, &e.UserAgent, &e.Method, &e.Path,
			&e.StatusCode, &e.DurationMs, &e.ContentType, &e.ResponseSize,
			&e.IdentityInfo, &e.PaymentInfo, &e.RateLimit); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		events = append(events, e)
	}
	return events, rows.Err()
}

// GetMetrics returns aggregated analytics for the given time range.
func (s *SQLiteStore) GetMetrics(tr TimeRange) (*Metrics, error) {
	from := tr.From.UTC().Format(time.RFC3339Nano)
	to := tr.To.UTC().Format(time.RFC3339Nano)

	m := &Metrics{StatusDistribution: make(map[int]int64)}

	// Total requests, unique agents, avg latency.
	row := s.db.QueryRow(`SELECT
		COUNT(*), COUNT(DISTINCT agent), COALESCE(AVG(duration_ms), 0)
		FROM events WHERE timestamp >= ? AND timestamp <= ?`, from, to)
	if err := row.Scan(&m.TotalRequests, &m.UniqueAgents, &m.AvgLatencyMs); err != nil {
		return nil, fmt.Errorf("metrics aggregate: %w", err)
	}

	// Error rate (4xx + 5xx).
	if m.TotalRequests > 0 {
		var errCount int64
		row = s.db.QueryRow(`SELECT COUNT(*) FROM events
			WHERE timestamp >= ? AND timestamp <= ? AND status_code >= 400`, from, to)
		if err := row.Scan(&errCount); err != nil {
			return nil, fmt.Errorf("metrics error count: %w", err)
		}
		m.ErrorRate = float64(errCount) / float64(m.TotalRequests)
	}

	// Top agents (top 10).
	rows, err := s.db.Query(`SELECT agent, COUNT(*) as cnt FROM events
		WHERE timestamp >= ? AND timestamp <= ? AND agent != ''
		GROUP BY agent ORDER BY cnt DESC LIMIT 10`, from, to)
	if err != nil {
		return nil, fmt.Errorf("metrics top agents: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ac AgentCount
		if err := rows.Scan(&ac.Agent, &ac.Count); err != nil {
			return nil, fmt.Errorf("scan agent count: %w", err)
		}
		m.TopAgents = append(m.TopAgents, ac)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Top paths (top 10).
	rows2, err := s.db.Query(`SELECT path, COUNT(*) as cnt FROM events
		WHERE timestamp >= ? AND timestamp <= ?
		GROUP BY path ORDER BY cnt DESC LIMIT 10`, from, to)
	if err != nil {
		return nil, fmt.Errorf("metrics top paths: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var pc PathCount
		if err := rows2.Scan(&pc.Path, &pc.Count); err != nil {
			return nil, fmt.Errorf("scan path count: %w", err)
		}
		m.TopPaths = append(m.TopPaths, pc)
	}
	if err := rows2.Err(); err != nil {
		return nil, err
	}

	// Status code distribution.
	rows3, err := s.db.Query(`SELECT status_code, COUNT(*) FROM events
		WHERE timestamp >= ? AND timestamp <= ?
		GROUP BY status_code`, from, to)
	if err != nil {
		return nil, fmt.Errorf("metrics status dist: %w", err)
	}
	defer rows3.Close()
	for rows3.Next() {
		var code int
		var cnt int64
		if err := rows3.Scan(&code, &cnt); err != nil {
			return nil, fmt.Errorf("scan status dist: %w", err)
		}
		m.StatusDistribution[code] = cnt
	}
	return m, rows3.Err()
}

// SaveConfig persists a key-value pair.
func (s *SQLiteStore) SaveConfig(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`INSERT INTO config (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = ?`, key, value, value)
	if err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}

// GetConfig retrieves a config value. Returns "" if not found.
func (s *SQLiteStore) GetConfig(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM config WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get config: %w", err)
	}
	return value, nil
}

// Cleanup deletes events older than retention and returns the count deleted.
func (s *SQLiteStore) Cleanup(retention time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().UTC().Add(-retention).Format(time.RFC3339Nano)
	res, err := s.db.Exec(`DELETE FROM events WHERE timestamp < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("cleanup: %w", err)
	}
	return res.RowsAffected()
}

// Close closes the database.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
