package score

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanInvalidURL(t *testing.T) {
	_, err := Scan("not-a-url", 5*time.Second)
	assert.Error(t, err)
}

// newTestOrigin creates a mock origin server that responds to various
// discovery and probe paths.
func newTestOrigin(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// Discovery endpoints.
	mux.HandleFunc("/.well-known/ai", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"name": "Test API"})
	})
	mux.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"name": "Test API"})
	})

	// llms.txt
	mux.HandleFunc("/llms.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("# Test API\n\n> A test API for agents.\n\nThis is a test API with some content that goes over 200 characters to get a better score. We add more text here to ensure we pass the length threshold for the llms.txt check."))
	})

	// agents.txt
	mux.HandleFunc("/agents.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("User-Agent: *\nAllow: /api/\nRate-Limit: 100/60\nAuth: bearer\n"))
	})

	// robots.txt
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("User-agent: *\nAllow: /\n\nUser-agent: GPTBot\nAllow: /api/\n\nUser-agent: ClaudeBot\nAllow: /api/\n\nUser-agent: Anthropic\nAllow: /api/\n\nSitemap: /sitemap.xml\n"))
	})

	// Default handler with good headers.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-RateLimit-Limit", "100")
		w.Header().Set("X-RateLimit-Remaining", "99")
		w.Header().Set("X-RateLimit-Reset", "1700000000")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization")
		w.Header().Set("Access-Control-Max-Age", "3600")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")

		// Return JSON for 404 probes too.
		if r.URL.Path != "/" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"type":    "not_found",
				"code":    "NOT_FOUND",
				"message": "Resource not found",
				"status":  404,
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	return httptest.NewServer(mux)
}

func TestScanFullPipeline(t *testing.T) {
	srv := newTestOrigin(t)
	defer srv.Close()

	report, err := Scan(srv.URL, 5*time.Second)
	require.NoError(t, err)

	assert.Equal(t, srv.URL, report.URL)
	assert.NotEmpty(t, report.Timestamp)
	assert.True(t, report.Score > 0, "score should be > 0")
	assert.True(t, report.DurationMs >= 0)
	assert.Len(t, report.Checks, 12)

	// Verify some specific checks passed.
	checkMap := make(map[string]CheckResult)
	for _, c := range report.Checks {
		checkMap[c.ID] = c
	}

	assert.Equal(t, SeverityPass, checkMap["discovery"].Severity, "discovery should pass")
	assert.Equal(t, SeverityPass, checkMap["rate-limits"].Severity, "rate limits should pass")
	assert.Equal(t, SeverityPass, checkMap["content-type"].Severity, "content-type should pass")
	assert.Equal(t, SeverityPass, checkMap["cors"].Severity, "cors should pass")
	assert.Equal(t, SeverityPass, checkMap["security-headers"].Severity, "security-headers should pass")
	assert.Equal(t, SeverityPass, checkMap["agents-txt"].Severity, "agents-txt should pass")
}

func TestEstimateWithGateway(t *testing.T) {
	report := &ScoreReport{
		Score: 20,
		Checks: []CheckResult{
			{ID: "discovery", Score: 0, MaxScore: 10},
			{ID: "llms-txt", Score: 0, MaxScore: 10},
			{ID: "agents-txt", Score: 0, MaxScore: 10},
			{ID: "rate-limits", Score: 0, MaxScore: 10},
			{ID: "structured-errors", Score: 0, MaxScore: 10},
			{ID: "cors", Score: 0, MaxScore: 10},
			{ID: "security-headers", Score: 0, MaxScore: 10},
			{ID: "response-time", Score: 8, MaxScore: 10},
			{ID: "openapi", Score: 0, MaxScore: 10},
			{ID: "content-type", Score: 5, MaxScore: 10},
			{ID: "robots-txt", Score: 4, MaxScore: 10},
			{ID: "x402", Score: 0, MaxScore: 10},
		},
	}

	estimated := EstimateWithGateway(report)
	assert.True(t, estimated > report.Score, "gateway estimate should be higher")
}

func TestFormatJSON(t *testing.T) {
	report := &ScoreReport{
		URL:       "https://example.com",
		Timestamp: "2026-01-01T00:00:00Z",
		Score:     42,
		Checks:    []CheckResult{{ID: "test", Name: "Test", Score: 5, MaxScore: 10, Severity: SeverityWarn, Message: "ok"}},
	}

	out, err := FormatJSON(report)
	require.NoError(t, err)
	assert.Contains(t, out, `"score": 42`)
}

func TestFormatReport(t *testing.T) {
	report := &ScoreReport{
		URL:        "https://example.com",
		Timestamp:  "2026-01-01T00:00:00Z",
		Score:      85,
		DurationMs: 150,
		Checks: []CheckResult{
			{ID: "test", Name: "Test Check", Score: 8, MaxScore: 10, Severity: SeverityPass, Message: "All good"},
		},
	}

	out := FormatReport(report, false)
	assert.Contains(t, out, "85/100")
	assert.Contains(t, out, "Test Check")
}

func TestGrade(t *testing.T) {
	assert.Equal(t, "A", Grade(95))
	assert.Equal(t, "B", Grade(85))
	assert.Equal(t, "C", Grade(75))
	assert.Equal(t, "D", Grade(55))
	assert.Equal(t, "F", Grade(30))
}
