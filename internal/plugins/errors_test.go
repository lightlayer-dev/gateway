package plugins_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lightlayer-dev/gateway/internal/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteError_Format(t *testing.T) {
	rec := httptest.NewRecorder()
	plugins.WriteError(rec, http.StatusBadRequest, "invalid_param", "The 'name' field is required.")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var env plugins.AgentErrorEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.Equal(t, "invalid_request_error", env.Type)
	assert.Equal(t, "invalid_param", env.Code)
	assert.Equal(t, "The 'name' field is required.", env.Message)
	assert.Equal(t, 400, env.Status)
	assert.False(t, env.IsRetriable)
	assert.Nil(t, env.RetryAfter)
	assert.Empty(t, env.Param)
	assert.Empty(t, env.DocsURL)
}

func TestWriteError_TypeMapping(t *testing.T) {
	tests := []struct {
		status   int
		wantType string
		retriable bool
	}{
		{400, "invalid_request_error", false},
		{401, "authentication_error", false},
		{403, "permission_error", false},
		{404, "not_found_error", false},
		{429, "rate_limit_error", true},
		{500, "api_error", false},
		{502, "api_error", true},
		{503, "api_error", true},
		{504, "api_error", true},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			rec := httptest.NewRecorder()
			plugins.WriteError(rec, tt.status, "test_code", "test message")

			var env plugins.AgentErrorEnvelope
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
			assert.Equal(t, tt.wantType, env.Type)
			assert.Equal(t, tt.status, env.Status)
			assert.Equal(t, tt.retriable, env.IsRetriable)
		})
	}
}

func TestWriteErrorFull_WithRetryAfter(t *testing.T) {
	rec := httptest.NewRecorder()
	retryAfter := 30
	plugins.WriteErrorFull(rec, plugins.AgentErrorEnvelope{
		Type:        "rate_limit_error",
		Code:        "rate_limited",
		Message:     "Too many requests.",
		Status:      429,
		IsRetriable: true,
		RetryAfter:  &retryAfter,
		Param:       "",
		DocsURL:     "https://docs.example.com/rate-limits",
	})

	assert.Equal(t, 429, rec.Code)
	assert.Equal(t, "30", rec.Header().Get("Retry-After"))

	var env plugins.AgentErrorEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.Equal(t, "rate_limit_error", env.Type)
	assert.NotNil(t, env.RetryAfter)
	assert.Equal(t, 30, *env.RetryAfter)
	assert.Equal(t, "https://docs.example.com/rate-limits", env.DocsURL)
}

func TestWriteErrorFull_WithParam(t *testing.T) {
	rec := httptest.NewRecorder()
	plugins.WriteErrorFull(rec, plugins.AgentErrorEnvelope{
		Type:        "invalid_request_error",
		Code:        "invalid_param",
		Message:     "The 'email' field must be a valid email.",
		Status:      400,
		IsRetriable: false,
		Param:       "email",
	})

	var env plugins.AgentErrorEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.Equal(t, "email", env.Param)
}
