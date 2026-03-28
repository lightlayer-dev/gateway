package detection

import (
	"regexp"
	"strings"
)

// AgentInfo describes a detected AI agent.
type AgentInfo struct {
	Detected bool   `json:"detected"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Version  string `json:"version,omitempty"`
	Verified bool   `json:"verified"`
}

// agentPattern maps a compiled regex to agent metadata.
type agentPattern struct {
	pattern  *regexp.Regexp
	name     string
	provider string
}

// agentPatterns is the list of known AI agent User-Agent patterns.
// Ported from agent-layer-ts analytics.ts AGENT_PATTERNS.
var agentPatterns = []agentPattern{
	{regexp.MustCompile(`(?i)ChatGPT-User`), "ChatGPT", "OpenAI"},
	{regexp.MustCompile(`(?i)GPTBot`), "GPTBot", "OpenAI"},
	{regexp.MustCompile(`(?i)Google-Extended`), "Google-Extended", "Google"},
	{regexp.MustCompile(`(?i)Googlebot`), "Googlebot", "Google"},
	{regexp.MustCompile(`(?i)Bingbot`), "Bingbot", "Microsoft"},
	{regexp.MustCompile(`(?i)ClaudeBot`), "ClaudeBot", "Anthropic"},
	{regexp.MustCompile(`(?i)Claude-Web`), "Claude-Web", "Anthropic"},
	{regexp.MustCompile(`(?i)Anthropic`), "Anthropic", "Anthropic"},
	{regexp.MustCompile(`(?i)PerplexityBot`), "PerplexityBot", "Perplexity"},
	{regexp.MustCompile(`(?i)Cohere-AI`), "Cohere", "Cohere"},
	{regexp.MustCompile(`(?i)YouBot`), "YouBot", "You.com"},
	{regexp.MustCompile(`(?i)CCBot`), "CCBot", "Common Crawl"},
	{regexp.MustCompile(`(?i)Bytespider`), "Bytespider", "ByteDance"},
	{regexp.MustCompile(`(?i)Applebot`), "Applebot", "Apple"},
	{regexp.MustCompile(`(?i)Meta-ExternalAgent`), "Meta-ExternalAgent", "Meta"},
	{regexp.MustCompile(`(?i)AI2Bot`), "AI2Bot", "Allen AI"},
	{regexp.MustCompile(`(?i)Diffbot`), "Diffbot", "Diffbot"},
	{regexp.MustCompile(`(?i)Amazonbot`), "Amazonbot", "Amazon"},
}

// DetectAgent inspects a User-Agent string and returns agent info.
// Returns nil if no known agent is detected.
func DetectAgent(userAgent string) *AgentInfo {
	if strings.TrimSpace(userAgent) == "" {
		return nil
	}

	for _, ap := range agentPatterns {
		if ap.pattern.MatchString(userAgent) {
			return &AgentInfo{
				Detected: true,
				Name:     ap.name,
				Provider: ap.provider,
			}
		}
	}

	return nil
}

// KnownAgentCount returns the number of known agent patterns.
func KnownAgentCount() int {
	return len(agentPatterns)
}
