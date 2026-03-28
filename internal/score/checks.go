package score

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ── Structured JSON Errors ──────────────────────────────────────────────────

func checkStructuredErrors(cfg ScanConfig) CheckResult {
	result := CheckResult{
		ID:       "structured-errors",
		Name:     "Structured JSON Errors",
		MaxScore: 10,
		Severity: SeverityFail,
	}

	probePaths := []string{"/__agent_layer_probe_404__", "/api/__nonexistent__", "/v1/__nonexistent__"}
	jsonCount := 0

	for _, path := range probePaths {
		resp, err := safeFetch(resolveURL(cfg.URL, path), cfg)
		if err != nil {
			continue
		}
		resp.Body.Close()
		ct := resp.Header.Get("Content-Type")
		if strings.Contains(ct, "json") {
			jsonCount++
		}
	}

	switch {
	case jsonCount == len(probePaths):
		result.Score = 10
		result.Severity = SeverityPass
		result.Message = "All error responses return JSON"
	case jsonCount > 0:
		result.Score = 5
		result.Severity = SeverityWarn
		result.Message = "Some error responses return JSON, some return HTML"
	default:
		result.Score = 0
		result.Message = "Error responses return HTML instead of structured JSON"
	}
	result.Suggestion = "Return JSON error envelopes with type, code, message, and is_retriable fields"
	return result
}

// ── Agent Discovery Endpoints ───────────────────────────────────────────────

func checkDiscovery(cfg ScanConfig) CheckResult {
	result := CheckResult{
		ID:       "discovery",
		Name:     "Agent Discovery Endpoints",
		MaxScore: 10,
		Severity: SeverityFail,
	}

	paths := []string{
		"/.well-known/agent.json",
		"/.well-known/ai",
		"/.well-known/ai-plugin.json",
	}
	found := 0

	for _, path := range paths {
		resp, err := safeFetch(resolveURL(cfg.URL, path), cfg)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			found++
		}
	}

	switch {
	case found >= 2:
		result.Score = 10
		result.Severity = SeverityPass
		result.Message = "Multiple agent discovery endpoints found"
	case found == 1:
		result.Score = 7
		result.Severity = SeverityWarn
		result.Message = "One discovery endpoint found"
	default:
		result.Score = 0
		result.Message = "No agent discovery endpoints found"
	}
	result.Suggestion = "Serve /.well-known/ai and /.well-known/agent.json for agent discoverability"
	return result
}

// ── llms.txt ────────────────────────────────────────────────────────────────

func checkLLMsTxt(cfg ScanConfig) CheckResult {
	result := CheckResult{
		ID:       "llms-txt",
		Name:     "llms.txt",
		MaxScore: 10,
		Severity: SeverityFail,
	}

	score := 0
	hasLLMs := false
	hasLLMsFull := false

	for _, path := range []string{"/llms.txt", "/llms-full.txt"} {
		resp, err := safeFetch(resolveURL(cfg.URL, path), cfg)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		if resp.StatusCode != 200 {
			continue
		}

		content := string(body)
		if path == "/llms.txt" {
			hasLLMs = true
			score += 5
			if strings.Contains(content, "#") || strings.Contains(content, ">") {
				score += 2
			}
			if len(content) > 200 {
				score++
			}
		} else {
			hasLLMsFull = true
		}
	}

	if hasLLMs && hasLLMsFull {
		score += 2
	}
	if score > 10 {
		score = 10
	}

	result.Score = score
	switch {
	case score >= 8:
		result.Severity = SeverityPass
		result.Message = "llms.txt present with structured content"
	case score > 0:
		result.Severity = SeverityWarn
		result.Message = "llms.txt present but could be improved"
	default:
		result.Message = "No /llms.txt found"
	}
	result.Suggestion = "Add /llms.txt with structured markdown describing your API for LLMs"
	return result
}

// ── robots.txt Agent Rules ──────────────────────────────────────────────────

func checkRobotsTxt(cfg ScanConfig) CheckResult {
	result := CheckResult{
		ID:       "robots-txt",
		Name:     "robots.txt Agent Rules",
		MaxScore: 10,
		Severity: SeverityFail,
	}

	aiAgents := []string{
		"GPTBot", "ChatGPT-User", "Google-Extended", "Anthropic", "ClaudeBot",
		"CCBot", "Amazonbot", "Bytespider", "Applebot-Extended", "PerplexityBot", "Cohere-ai",
	}

	resp, err := safeFetch(resolveURL(cfg.URL, "/robots.txt"), cfg)
	if err != nil {
		result.Score = 0
		result.Message = "Could not fetch robots.txt"
		result.Suggestion = "Add a robots.txt with explicit rules for AI agents"
		return result
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()

	if resp.StatusCode != 200 {
		result.Score = 3
		result.Severity = SeverityWarn
		result.Message = "No robots.txt found"
		result.Suggestion = "Add a robots.txt with explicit rules for AI agents"
		return result
	}

	content := string(body)
	score := 4 // Base: robots.txt exists
	mentions := 0
	for _, agent := range aiAgents {
		if strings.Contains(content, agent) {
			mentions++
		}
	}
	if mentions > 3 {
		mentions = 3
	}
	score += mentions

	if strings.Contains(strings.ToLower(content), "sitemap") {
		score++
	}
	if strings.Contains(content, "User-agent: *") {
		score++
	}
	if mentions >= 3 {
		score++
	}
	if score > 10 {
		score = 10
	}

	result.Score = score
	switch {
	case score >= 8:
		result.Severity = SeverityPass
		result.Message = "robots.txt has explicit AI agent rules"
	case score >= 5:
		result.Severity = SeverityWarn
		result.Message = "robots.txt exists but has few AI agent rules"
	default:
		result.Severity = SeverityFail
		result.Message = "robots.txt has no AI agent rules"
	}
	result.Suggestion = "Add explicit User-agent rules for GPTBot, ClaudeBot, and other AI agents"
	return result
}

// ── Rate Limit Headers ──────────────────────────────────────────────────────

func checkRateLimitHeaders(cfg ScanConfig) CheckResult {
	result := CheckResult{
		ID:       "rate-limits",
		Name:     "Rate Limit Headers",
		MaxScore: 10,
		Severity: SeverityFail,
	}

	resp, err := safeFetch(cfg.URL, cfg)
	if err != nil {
		result.Score = 0
		result.Message = "Could not fetch URL"
		result.Suggestion = "Include X-RateLimit-Limit, X-RateLimit-Remaining, and X-RateLimit-Reset headers"
		return result
	}
	resp.Body.Close()

	limitHeaders := []string{"x-ratelimit-limit", "ratelimit-limit", "x-rate-limit-limit"}
	remainingHeaders := []string{"x-ratelimit-remaining", "ratelimit-remaining", "x-rate-limit-remaining"}
	resetHeaders := []string{"x-ratelimit-reset", "ratelimit-reset", "retry-after"}

	hasAny := false
	hasLimit := false
	hasRemaining := false
	hasReset := false

	for _, h := range limitHeaders {
		if resp.Header.Get(h) != "" {
			hasAny = true
			hasLimit = true
			break
		}
	}
	for _, h := range remainingHeaders {
		if resp.Header.Get(h) != "" {
			hasAny = true
			hasRemaining = true
			break
		}
	}
	for _, h := range resetHeaders {
		if resp.Header.Get(h) != "" {
			hasAny = true
			hasReset = true
			break
		}
	}
	if resp.Header.Get("ratelimit-policy") != "" {
		hasAny = true
	}

	score := 0
	if hasAny {
		score += 4
	}
	if hasLimit {
		score += 2
	}
	if hasRemaining {
		score += 2
	}
	if hasReset {
		score += 2
	}

	result.Score = score
	switch {
	case score >= 8:
		result.Severity = SeverityPass
		result.Message = "Rate limit headers present (limit, remaining, reset)"
	case score > 0:
		result.Severity = SeverityWarn
		result.Message = "Some rate limit headers present"
	default:
		result.Message = "No rate limit headers found"
	}
	result.Suggestion = "Include X-RateLimit-Limit, X-RateLimit-Remaining, and X-RateLimit-Reset headers"
	return result
}

// ── OpenAPI / Swagger ───────────────────────────────────────────────────────

func checkOpenAPI(cfg ScanConfig) CheckResult {
	result := CheckResult{
		ID:       "openapi",
		Name:     "OpenAPI Specification",
		MaxScore: 10,
		Severity: SeverityFail,
	}

	paths := []string{
		"/openapi.json", "/openapi.yaml", "/swagger.json", "/api-docs",
		"/docs/openapi.json", "/v1/openapi.json", "/api/openapi.json", "/.well-known/openapi.json",
	}

	for _, path := range paths {
		resp, err := safeFetch(resolveURL(cfg.URL, path), cfg)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()

		if resp.StatusCode != 200 {
			continue
		}

		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "json") && !strings.Contains(ct, "yaml") && !strings.Contains(ct, "text") {
			continue
		}

		content := string(body)
		score := 5 // Spec found

		if strings.Contains(content, `"openapi":"3`) || strings.Contains(content, `"openapi": "3`) || strings.Contains(content, "openapi: \"3") || strings.Contains(content, "openapi: '3") {
			score += 2
		} else if strings.Contains(content, "swagger") || strings.Contains(content, "openapi") {
			score++
		}

		if strings.Contains(content, "description") {
			score += 2
		}

		if score > 10 {
			score = 10
		}
		result.Score = score
		if score >= 8 {
			result.Severity = SeverityPass
			result.Message = "OpenAPI specification found with good documentation"
		} else {
			result.Severity = SeverityWarn
			result.Message = "OpenAPI specification found but could be improved"
		}
		result.Suggestion = "Use OpenAPI 3.x with descriptions on all endpoints"
		return result
	}

	result.Score = 0
	result.Message = "No OpenAPI/Swagger specification found"
	result.Suggestion = "Add an OpenAPI specification at /openapi.json"
	return result
}

// ── Content-Type Headers ────────────────────────────────────────────────────

func checkContentType(cfg ScanConfig) CheckResult {
	result := CheckResult{
		ID:       "content-type",
		Name:     "Content-Type Headers",
		MaxScore: 10,
		Severity: SeverityFail,
	}

	resp, err := safeFetch(cfg.URL, cfg)
	if err != nil {
		result.Score = 0
		result.Message = "Could not fetch URL"
		result.Suggestion = "Include Content-Type header with charset"
		return result
	}
	resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		result.Score = 0
		result.Message = "No Content-Type header"
		result.Suggestion = "Include Content-Type: application/json; charset=utf-8"
		return result
	}

	score := 5 // Has Content-Type
	if strings.Contains(ct, "charset") {
		score += 3
	}
	if !strings.Contains(ct, "octet-stream") {
		score += 2
	}

	result.Score = score
	if score >= 8 {
		result.Severity = SeverityPass
		result.Message = "Content-Type header present with charset"
	} else {
		result.Severity = SeverityWarn
		result.Message = "Content-Type header present but missing charset"
	}
	result.Suggestion = "Include charset in Content-Type (e.g., application/json; charset=utf-8)"
	return result
}

// ── CORS for Agents ─────────────────────────────────────────────────────────

func checkCORS(cfg ScanConfig) CheckResult {
	result := CheckResult{
		ID:       "cors",
		Name:     "CORS for Agents",
		MaxScore: 10,
		Severity: SeverityFail,
	}

	headers := map[string]string{
		"Origin":                        "https://agent.example.com",
		"Access-Control-Request-Method": "GET",
	}

	resp, err := safeFetchWithMethod("OPTIONS", cfg.URL, cfg, headers)
	if err != nil {
		// Try a normal GET with Origin header.
		resp, err = safeFetchWithMethod("GET", cfg.URL, cfg, map[string]string{"Origin": "https://agent.example.com"})
		if err != nil {
			result.Score = 0
			result.Message = "Could not fetch URL"
			result.Suggestion = "Configure CORS to allow agent access"
			return result
		}
	}
	resp.Body.Close()

	score := 0
	acao := resp.Header.Get("Access-Control-Allow-Origin")
	if acao != "" {
		score += 5
		if acao == "*" || strings.Contains(acao, "agent") {
			score += 2
		}
	}
	if resp.Header.Get("Access-Control-Allow-Methods") != "" {
		score++
	}
	if resp.Header.Get("Access-Control-Allow-Headers") != "" {
		score++
	}
	if resp.Header.Get("Access-Control-Max-Age") != "" {
		score++
	}
	if score > 10 {
		score = 10
	}

	result.Score = score
	switch {
	case score >= 8:
		result.Severity = SeverityPass
		result.Message = "CORS configured for agent access"
	case score > 0:
		result.Severity = SeverityWarn
		result.Message = "Partial CORS configuration"
	default:
		result.Message = "No CORS headers found"
	}
	result.Suggestion = "Configure CORS with Access-Control-Allow-Origin, Allow-Methods, Allow-Headers, and Max-Age"
	return result
}

// ── Security Headers ────────────────────────────────────────────────────────

func checkSecurityHeaders(cfg ScanConfig) CheckResult {
	result := CheckResult{
		ID:       "security-headers",
		Name:     "Security Headers",
		MaxScore: 10,
		Severity: SeverityFail,
	}

	resp, err := safeFetch(cfg.URL, cfg)
	if err != nil {
		result.Score = 0
		result.Message = "Could not fetch URL"
		result.Suggestion = "Add HSTS, X-Content-Type-Options, and other security headers"
		return result
	}
	resp.Body.Close()

	score := 0
	var missing []string

	if resp.Header.Get("Strict-Transport-Security") != "" {
		score += 3
	} else {
		missing = append(missing, "HSTS")
	}
	if resp.Header.Get("X-Content-Type-Options") != "" {
		score += 2
	} else {
		missing = append(missing, "X-Content-Type-Options")
	}
	if resp.Header.Get("X-Frame-Options") != "" {
		score++
	} else {
		missing = append(missing, "X-Frame-Options")
	}
	if resp.Header.Get("Referrer-Policy") != "" {
		score += 2
	} else {
		missing = append(missing, "Referrer-Policy")
	}
	if resp.Header.Get("Content-Security-Policy") != "" {
		score += 2
	} else {
		missing = append(missing, "CSP")
	}

	result.Score = score
	switch {
	case score >= 8:
		result.Severity = SeverityPass
		result.Message = "Security headers present"
	case score >= 4:
		result.Severity = SeverityWarn
		result.Message = "Some security headers present"
	default:
		result.Message = "Few or no security headers"
	}
	if len(missing) > 0 {
		result.Suggestion = "Add missing headers: " + strings.Join(missing, ", ")
	}
	return result
}

// ── Response Time ───────────────────────────────────────────────────────────

func checkResponseTime(cfg ScanConfig) CheckResult {
	result := CheckResult{
		ID:       "response-time",
		Name:     "Response Time",
		MaxScore: 10,
		Severity: SeverityFail,
	}

	var totalMs int64
	probes := 3
	successes := 0

	for range probes {
		start := time.Now()
		resp, err := safeFetch(cfg.URL, cfg)
		elapsed := time.Since(start).Milliseconds()
		if err != nil {
			continue
		}
		resp.Body.Close()
		totalMs += elapsed
		successes++
	}

	if successes == 0 {
		result.Score = 0
		result.Message = "Could not measure response time"
		result.Suggestion = "Ensure the server is reachable"
		return result
	}

	avg := totalMs / int64(successes)
	result.Details = map[string]interface{}{
		"avg_ms":  avg,
		"samples": successes,
	}

	switch {
	case avg <= 200:
		result.Score = 10
		result.Severity = SeverityPass
		result.Message = fmt.Sprintf("Excellent response time (%dms avg)", avg)
	case avg <= 500:
		result.Score = 8
		result.Severity = SeverityPass
		result.Message = fmt.Sprintf("Good response time (%dms avg)", avg)
	case avg <= 1000:
		result.Score = 6
		result.Severity = SeverityWarn
		result.Message = fmt.Sprintf("Moderate response time (%dms avg)", avg)
	case avg <= 2000:
		result.Score = 4
		result.Severity = SeverityWarn
		result.Message = fmt.Sprintf("Slow response time (%dms avg)", avg)
	case avg <= 5000:
		result.Score = 2
		result.Severity = SeverityFail
		result.Message = fmt.Sprintf("Very slow response time (%dms avg)", avg)
	default:
		result.Score = 1
		result.Severity = SeverityFail
		result.Message = fmt.Sprintf("Extremely slow response time (%dms avg)", avg)
	}
	result.Suggestion = "Consider caching, CDN, or backend query optimization"
	return result
}

// ── x402 Agent Payments ─────────────────────────────────────────────────────

func checkX402(cfg ScanConfig) CheckResult {
	result := CheckResult{
		ID:       "x402",
		Name:     "x402 Agent Payments",
		MaxScore: 10,
		Severity: SeverityFail,
	}

	x402Headers := []string{
		"x-payment-address", "x-payment-network", "x-payment-amount",
		"x-payment-currency", "x-payment-required",
	}

	score := 0

	// Check /.well-known/x402.
	resp, err := safeFetch(resolveURL(cfg.URL, "/.well-known/x402"), cfg)
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			score += 4
		}
	}

	// Check main URL for x402 headers.
	resp, err = safeFetch(cfg.URL, cfg)
	if err == nil {
		resp.Body.Close()
		headerCount := 0
		for _, h := range x402Headers {
			if resp.Header.Get(h) != "" {
				headerCount++
			}
		}
		if headerCount > 0 {
			score += 3
		}
	}

	// Probe for 402 response.
	resp, err = safeFetch(resolveURL(cfg.URL, "/api/__x402_probe__"), cfg)
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusPaymentRequired {
			score += 3
		}
	}

	if score > 10 {
		score = 10
	}

	result.Score = score
	switch {
	case score >= 8:
		result.Severity = SeverityPass
		result.Message = "x402 micropayment support detected"
	case score >= 4:
		result.Severity = SeverityWarn
		result.Message = "Partial x402 support detected"
	default:
		result.Message = "No x402 micropayment support"
	}
	result.Suggestion = "Add x402 micropayment support for AI agent billing"
	return result
}

// ── agents.txt ──────────────────────────────────────────────────────────────

func checkAgentsTxt(cfg ScanConfig) CheckResult {
	result := CheckResult{
		ID:       "agents-txt",
		Name:     "agents.txt Permissions",
		MaxScore: 10,
		Severity: SeverityFail,
	}

	resp, err := safeFetch(resolveURL(cfg.URL, "/agents.txt"), cfg)
	if err != nil {
		result.Score = 0
		result.Message = "Could not fetch /agents.txt"
		result.Suggestion = "Add /agents.txt with User-Agent, Allow/Disallow, and rate-limit directives"
		return result
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()

	if resp.StatusCode != 200 {
		result.Score = 0
		result.Message = "No /agents.txt found"
		result.Suggestion = "Add /agents.txt with User-Agent, Allow/Disallow, Auth, and Rate-Limit directives"
		return result
	}

	content := string(body)
	directives := []string{"User-Agent", "Allow", "Disallow", "Auth", "Rate-Limit"}
	found := 0
	for _, d := range directives {
		if strings.Contains(content, d) || strings.Contains(content, strings.ToLower(d)) {
			found++
		}
	}

	switch {
	case found >= 3:
		result.Score = 10
		result.Severity = SeverityPass
		result.Message = "agents.txt present with comprehensive directives"
	case found > 0:
		result.Score = 6
		result.Severity = SeverityWarn
		result.Message = "agents.txt present but has few directives"
		result.Suggestion = "Add Auth and Rate-Limit directives to agents.txt"
	default:
		result.Score = 3
		result.Severity = SeverityWarn
		result.Message = "agents.txt exists but has no recognized directives"
		result.Suggestion = "Add User-Agent, Allow/Disallow, Auth, and Rate-Limit directives"
	}
	return result
}

