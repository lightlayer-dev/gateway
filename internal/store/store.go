// Package store defines the storage interface for analytics events and
// gateway configuration persistence.
package store

import "time"

// AgentEvent represents a single request event captured by the analytics plugin.
// Ported from agent-layer-ts AgentEvent with additional gateway-specific fields.
type AgentEvent struct {
	ID           string    `json:"id"`
	Timestamp    time.Time `json:"timestamp"`
	Agent        string    `json:"agent"`
	UserAgent    string    `json:"user_agent"`
	Method       string    `json:"method"`
	Path         string    `json:"path"`
	StatusCode   int       `json:"status_code"`
	DurationMs   float64   `json:"duration_ms"`
	ContentType  string    `json:"content_type,omitempty"`
	ResponseSize int64     `json:"response_size,omitempty"`
	IdentityInfo string    `json:"identity_info,omitempty"` // JSON-encoded identity claims
	PaymentInfo  string    `json:"payment_info,omitempty"`  // JSON-encoded payment info
	RateLimit    string    `json:"rate_limit,omitempty"`    // JSON-encoded rate limit status
}

// EventFilter specifies criteria for querying events.
type EventFilter struct {
	Agent      string    `json:"agent,omitempty"`
	Method     string    `json:"method,omitempty"`
	Path       string    `json:"path,omitempty"`
	MinStatus  int       `json:"min_status,omitempty"`
	MaxStatus  int       `json:"max_status,omitempty"`
	From       time.Time `json:"from,omitempty"`
	To         time.Time `json:"to,omitempty"`
	Limit      int       `json:"limit,omitempty"`
	Offset     int       `json:"offset,omitempty"`
}

// TimeRange specifies a time window for aggregated metrics.
type TimeRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// Metrics holds aggregated analytics data for a time range.
type Metrics struct {
	TotalRequests      int64              `json:"total_requests"`
	UniqueAgents       int                `json:"unique_agents"`
	AvgLatencyMs       float64            `json:"avg_latency_ms"`
	ErrorRate          float64            `json:"error_rate"` // fraction of 4xx+5xx
	TopAgents          []AgentCount       `json:"top_agents"`
	TopPaths           []PathCount        `json:"top_paths"`
	StatusDistribution map[int]int64      `json:"status_distribution"`
	RequestsByHour     []HourlyCount      `json:"requests_by_hour"`
}

// HourlyCount pairs an hour label with its request count.
type HourlyCount struct {
	Hour  string `json:"hour"`
	Count int64  `json:"count"`
}

// AgentCount pairs an agent name with its request count.
type AgentCount struct {
	Agent    string `json:"agent"`
	Count    int64  `json:"count"`
	LastSeen string `json:"last_seen,omitempty"`
}

// PathCount pairs a request path with its request count.
type PathCount struct {
	Path         string  `json:"path"`
	Method       string  `json:"method,omitempty"`
	Count        int64   `json:"count"`
	AvgLatencyMs float64 `json:"avg_latency_ms,omitempty"`
}

// AgentRecord tracks a known agent for the dashboard agent list.
type AgentRecord struct {
	Name          string    `json:"name"`
	FirstSeen     time.Time `json:"first_seen"`
	LastSeen      time.Time `json:"last_seen"`
	TotalRequests int64     `json:"total_requests"`
	Verified      bool      `json:"verified"`
}

// Store is the persistence interface for analytics events and configuration.
type Store interface {
	// SaveEvent persists a single analytics event.
	SaveEvent(event AgentEvent) error

	// QueryEvents retrieves events matching the given filter.
	QueryEvents(filter EventFilter) ([]AgentEvent, error)

	// GetMetrics returns aggregated metrics for the given time range.
	GetMetrics(timeRange TimeRange) (*Metrics, error)

	// SaveConfig persists a configuration key-value pair.
	SaveConfig(key, value string) error

	// GetConfig retrieves a configuration value by key.
	GetConfig(key string) (string, error)

	// Cleanup removes events older than the given retention period.
	Cleanup(retention time.Duration) (int64, error)

	// Close releases any resources held by the store.
	Close() error
}
