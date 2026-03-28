// Package onboarding implements agent self-registration via webhook-based provisioning.
//
// Agents discover the registration endpoint through /.well-known/agent.json and /llms.txt,
// POST to /agent/register with their identity, and receive credentials back. The gateway
// forwards the request to the API owner's provisioning webhook and returns the result.
// The gateway never stores credentials — it's a stateless facilitator.
package onboarding

import "time"

// ── Registration Request (Agent → Gateway) ─────────────────────────────

// RegistrationRequest is the body an agent sends to POST /agent/register.
type RegistrationRequest struct {
	IdentityToken string                 `json:"identity_token,omitempty"` // Optional signed JWT proving agent identity
	AgentID       string                 `json:"agent_id"`                // Agent's self-identified ID
	AgentName     string                 `json:"agent_name"`              // Human-readable name
	AgentProvider string                 `json:"agent_provider"`          // Who operates this agent
	Metadata      map[string]interface{} `json:"metadata,omitempty"`      // Optional extra info
}

// ── Registration Response (Gateway → Agent) ─────────────────────────────

// RegistrationResponse is the standardized response to a registration request.
type RegistrationResponse struct {
	Status      string      `json:"status"`                // "provisioned" or "rejected"
	Credentials *Credential `json:"credentials,omitempty"` // Populated when status=provisioned
	Reason      string      `json:"reason,omitempty"`      // Populated when status=rejected
}

// Credential represents provisioned credentials in one of three formats.
type Credential struct {
	Type string `json:"type"` // "api_key", "oauth2_client_credentials", "bearer"

	// api_key fields
	Token  string `json:"token,omitempty"`
	Header string `json:"header,omitempty"`

	// oauth2_client_credentials fields
	ClientID     string   `json:"client_id,omitempty"`
	ClientSecret string   `json:"client_secret,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`

	// bearer fields
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`

	// Shared fields
	TokenEndpoint string  `json:"token_endpoint,omitempty"`
	ExpiresAt     *string `json:"expires_at,omitempty"` // RFC 3339 or null
}

// ── Auth Required Response ──────────────────────────────────────────────

// AuthRequiredResponse is returned as 401 when an unauthenticated agent hits the API.
type AuthRequiredResponse struct {
	Error                    string   `json:"error"`
	Message                  string   `json:"message"`
	RegisterURL              string   `json:"register_url"`
	AuthDocs                 string   `json:"auth_docs,omitempty"`
	SupportedCredentialTypes []string `json:"supported_credential_types"`
}

// ── Webhook Request (Gateway → API Owner) ───────────────────────────────

// WebhookRequest is sent to the API owner's provisioning webhook.
type WebhookRequest struct {
	AgentID          string `json:"agent_id"`
	AgentName        string `json:"agent_name"`
	AgentProvider    string `json:"agent_provider"`
	IdentityVerified bool   `json:"identity_verified"`
	RequestIP        string `json:"request_ip"`
	Timestamp        string `json:"timestamp"` // RFC 3339
}

// ── Webhook Response (API Owner → Gateway) ──────────────────────────────

// WebhookResponse is what the API owner returns from their provisioning webhook.
type WebhookResponse = RegistrationResponse

// ── Plugin Config ───────────────────────────────────────────────────────

// Config holds agent onboarding plugin settings.
type Config struct {
	ProvisioningWebhook string        // URL to POST webhook requests to
	WebhookSecret       string        // HMAC-SHA256 secret for signing webhook calls
	WebhookTimeout      time.Duration // Timeout for webhook HTTP calls
	RequireIdentity     bool          // If true, agent must present a signed JWT
	AllowedProviders    []string      // Empty = allow all. Restrict by provider name.
	AuthDocs            string        // URL to auth documentation
	RateLimit           *RateLimitConfig
}

// RateLimitConfig controls registration rate limiting.
type RateLimitConfig struct {
	MaxRegistrations int           // Per IP per window
	Window           time.Duration // Rate limit window
}

// SupportedCredentialTypes lists the credential types agents can receive.
var SupportedCredentialTypes = []string{"api_key", "oauth2_client_credentials", "bearer"}
