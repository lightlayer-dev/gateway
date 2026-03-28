package detection_test

import (
	"testing"

	"github.com/lightlayer-dev/gateway/internal/detection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectAgent_AllKnownPatterns(t *testing.T) {
	tests := []struct {
		userAgent string
		wantName  string
		wantProv  string
	}{
		// OpenAI
		{"Mozilla/5.0 ChatGPT-User/1.0", "ChatGPT", "OpenAI"},
		{"Mozilla/5.0 (compatible; GPTBot/1.1)", "GPTBot", "OpenAI"},
		// Google
		{"Mozilla/5.0 (compatible; Google-Extended)", "Google-Extended", "Google"},
		{"Mozilla/5.0 (compatible; Googlebot/2.1)", "Googlebot", "Google"},
		// Microsoft
		{"Mozilla/5.0 (compatible; Bingbot/2.0)", "Bingbot", "Microsoft"},
		// Anthropic
		{"ClaudeBot/1.0", "ClaudeBot", "Anthropic"},
		{"Claude-Web/1.0", "Claude-Web", "Anthropic"},
		{"Anthropic-Crawler/1.0", "Anthropic", "Anthropic"},
		// Perplexity
		{"Mozilla/5.0 (compatible; PerplexityBot/1.0)", "PerplexityBot", "Perplexity"},
		// Cohere
		{"Cohere-AI/1.0", "Cohere", "Cohere"},
		// You.com
		{"YouBot/1.0", "YouBot", "You.com"},
		// Common Crawl
		{"CCBot/2.0 (https://commoncrawl.org/faq/)", "CCBot", "Common Crawl"},
		// ByteDance
		{"Bytespider/1.0", "Bytespider", "ByteDance"},
		// Apple
		{"Mozilla/5.0 (compatible; Applebot/0.1)", "Applebot", "Apple"},
		// Meta
		{"Meta-ExternalAgent/1.0", "Meta-ExternalAgent", "Meta"},
		// Allen AI
		{"AI2Bot/1.0", "AI2Bot", "Allen AI"},
		// Diffbot
		{"Diffbot/1.0", "Diffbot", "Diffbot"},
		// Amazon
		{"Mozilla/5.0 (compatible; Amazonbot/0.1)", "Amazonbot", "Amazon"},
	}

	for _, tt := range tests {
		t.Run(tt.wantName, func(t *testing.T) {
			info := detection.DetectAgent(tt.userAgent)
			require.NotNil(t, info, "expected agent to be detected for %q", tt.userAgent)
			assert.True(t, info.Detected)
			assert.Equal(t, tt.wantName, info.Name)
			assert.Equal(t, tt.wantProv, info.Provider)
		})
	}
}

func TestDetectAgent_CaseInsensitive(t *testing.T) {
	tests := []string{
		"chatgpt-user/1.0",
		"CHATGPT-USER/1.0",
		"ChatGPT-user/1.0",
		"claudebot/1.0",
		"CLAUDEBOT/1.0",
		"perplexitybot",
		"PERPLEXITYBOT",
	}

	for _, ua := range tests {
		t.Run(ua, func(t *testing.T) {
			info := detection.DetectAgent(ua)
			require.NotNil(t, info, "expected agent to be detected for %q", ua)
			assert.True(t, info.Detected)
		})
	}
}

func TestDetectAgent_NoMatch(t *testing.T) {
	tests := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		"curl/7.68.0",
		"PostmanRuntime/7.28.4",
		"",
		"   ",
	}

	for _, ua := range tests {
		t.Run(ua, func(t *testing.T) {
			info := detection.DetectAgent(ua)
			assert.Nil(t, info)
		})
	}
}

func TestDetectAgent_EmbeddedInLongerUA(t *testing.T) {
	// Agent pattern embedded in a longer User-Agent string.
	ua := "Mozilla/5.0 (compatible; ClaudeBot/1.0; +https://anthropic.com)"
	info := detection.DetectAgent(ua)
	require.NotNil(t, info)
	assert.Equal(t, "ClaudeBot", info.Name)
	assert.Equal(t, "Anthropic", info.Provider)
}

func TestDetectAgent_FirstMatchWins(t *testing.T) {
	// When multiple patterns could match, the first one wins.
	// "Anthropic" appears after "ClaudeBot" in patterns, so ClaudeBot should win.
	ua := "ClaudeBot Anthropic/1.0"
	info := detection.DetectAgent(ua)
	require.NotNil(t, info)
	assert.Equal(t, "ClaudeBot", info.Name)
}

func TestKnownAgentCount(t *testing.T) {
	assert.GreaterOrEqual(t, detection.KnownAgentCount(), 18,
		"should have at least 18 known agent patterns")
}
