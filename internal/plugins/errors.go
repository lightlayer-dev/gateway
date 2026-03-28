package plugins

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// AgentErrorEnvelope is the structured JSON error format for all gateway errors.
// Ported from agent-layer-ts types.ts AgentErrorEnvelope.
type AgentErrorEnvelope struct {
	Type        string `json:"type"`
	Code        string `json:"code"`
	Message     string `json:"message"`
	Status      int    `json:"status"`
	IsRetriable bool   `json:"is_retriable"`
	RetryAfter  *int   `json:"retry_after,omitempty"`
	Param       string `json:"param,omitempty"`
	DocsURL     string `json:"docs_url,omitempty"`
}

// WriteError writes a structured JSON error response using AgentErrorEnvelope.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteErrorFull(w, AgentErrorEnvelope{
		Type:        errorType(status),
		Code:        code,
		Message:     message,
		Status:      status,
		IsRetriable: isRetriable(status),
	})
}

// WriteErrorFull writes a full AgentErrorEnvelope as the response.
func WriteErrorFull(w http.ResponseWriter, envelope AgentErrorEnvelope) {
	w.Header().Set("Content-Type", "application/json")
	if envelope.RetryAfter != nil {
		w.Header().Set("Retry-After", retryAfterStr(*envelope.RetryAfter))
	}
	w.WriteHeader(envelope.Status)
	json.NewEncoder(w).Encode(envelope)
}

// errorType maps HTTP status codes to error type strings.
func errorType(status int) string {
	switch {
	case status == http.StatusBadRequest:
		return "invalid_request_error"
	case status == http.StatusUnauthorized:
		return "authentication_error"
	case status == http.StatusForbidden:
		return "permission_error"
	case status == http.StatusNotFound:
		return "not_found_error"
	case status == http.StatusTooManyRequests:
		return "rate_limit_error"
	case status >= 500:
		return "api_error"
	default:
		return "api_error"
	}
}

// isRetriable returns true for status codes that are generally retriable.
func isRetriable(status int) bool {
	switch status {
	case http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func retryAfterStr(seconds int) string {
	return strconv.Itoa(seconds)
}
