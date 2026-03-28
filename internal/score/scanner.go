package score

import (
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// DefaultTimeout is the per-request timeout for check probes.
const DefaultTimeout = 10 * time.Second

// DefaultUserAgent identifies the scanner.
const DefaultUserAgent = "LightLayerScore/0.1 (https://lightlayer.dev)"

// ScanConfig holds the configuration for a scan run.
type ScanConfig struct {
	URL       string
	Timeout   time.Duration
	UserAgent string
}

// CheckSeverity represents the result level of a check.
type CheckSeverity string

const (
	SeverityPass CheckSeverity = "pass"
	SeverityWarn CheckSeverity = "warn"
	SeverityFail CheckSeverity = "fail"
)

// CheckResult is the outcome of a single check.
type CheckResult struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Score      int                    `json:"score"`
	MaxScore   int                    `json:"max_score"`
	Severity   CheckSeverity          `json:"severity"`
	Message    string                 `json:"message"`
	Suggestion string                 `json:"suggestion,omitempty"`
	Details    map[string]interface{} `json:"details,omitempty"`
}

// ScoreReport is the full output of a scan.
type ScoreReport struct {
	URL        string        `json:"url"`
	Timestamp  string        `json:"timestamp"`
	Score      int           `json:"score"`
	Checks     []CheckResult `json:"checks"`
	DurationMs int64         `json:"duration_ms"`
}

// CheckFn is a function that performs a single check.
type CheckFn func(cfg ScanConfig) CheckResult

// allChecks is the ordered list of checks to run.
var allChecks = []CheckFn{
	checkStructuredErrors,
	checkDiscovery,
	checkLLMsTxt,
	checkRobotsTxt,
	checkRateLimitHeaders,
	checkOpenAPI,
	checkContentType,
	checkCORS,
	checkSecurityHeaders,
	checkResponseTime,
	checkX402,
	checkAgentsTxt,
	checkAGUI,
}

// Scan runs all checks against the given URL and returns a report.
func Scan(targetURL string, timeout time.Duration) (*ScoreReport, error) {
	if _, err := url.ParseRequestURI(targetURL); err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	if timeout == 0 {
		timeout = DefaultTimeout
	}

	cfg := ScanConfig{
		URL:       targetURL,
		Timeout:   timeout,
		UserAgent: DefaultUserAgent,
	}

	start := time.Now()

	// Run all checks concurrently.
	results := make([]CheckResult, len(allChecks))
	var wg sync.WaitGroup
	wg.Add(len(allChecks))
	for i, check := range allChecks {
		go func(idx int, fn CheckFn) {
			defer wg.Done()
			results[idx] = fn(cfg)
		}(i, check)
	}
	wg.Wait()

	// Compute normalized score.
	var totalScore, maxScore int
	for _, r := range results {
		totalScore += r.Score
		maxScore += r.MaxScore
	}

	normalized := 0
	if maxScore > 0 {
		normalized = (totalScore * 100) / maxScore
	}

	return &ScoreReport{
		URL:        targetURL,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Score:      normalized,
		Checks:     results,
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}

// EstimateWithGateway estimates what the score would be with LightLayer Gateway.
// The gateway automatically provides: discovery, llms.txt, agents.txt, rate limit headers,
// structured errors, CORS, security headers, and AG-UI.
func EstimateWithGateway(report *ScoreReport) int {
	gatewayProvides := map[string]int{
		"discovery":        10,
		"llms-txt":         10,
		"agents-txt":       10,
		"rate-limits":      10,
		"structured-errors": 10,
		"cors":             10,
		"security-headers": 10,
		"ag-ui":            5,
	}

	var totalScore, maxScore int
	for _, r := range report.Checks {
		if boost, ok := gatewayProvides[r.ID]; ok {
			totalScore += boost
		} else {
			totalScore += r.Score
		}
		maxScore += r.MaxScore
	}

	if maxScore == 0 {
		return 0
	}
	return (totalScore * 100) / maxScore
}

// resolveURL builds a full URL from a base and a path.
func resolveURL(base, path string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base + path
	}
	u.Path = path
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// safeFetch performs an HTTP GET with the scanner's timeout and user-agent.
func safeFetch(urlStr string, cfg ScanConfig) (*http.Response, error) {
	client := &http.Client{Timeout: cfg.Timeout}
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", cfg.UserAgent)
	return client.Do(req)
}

// safeFetchWithMethod performs an HTTP request with the given method.
func safeFetchWithMethod(method, urlStr string, cfg ScanConfig, headers map[string]string) (*http.Response, error) {
	client := &http.Client{Timeout: cfg.Timeout}
	req, err := http.NewRequest(method, urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", cfg.UserAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return client.Do(req)
}
