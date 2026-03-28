package onboarding

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ── Webhook Tests ───────────────────────────────────────────────────────

func TestSign(t *testing.T) {
	body := []byte(`{"agent_id":"test"}`)
	secret := "mysecret"

	sig := Sign(body, secret)
	if sig == "" {
		t.Fatal("expected non-empty signature")
	}

	// Same input → same output.
	sig2 := Sign(body, secret)
	if sig != sig2 {
		t.Fatalf("signatures should be deterministic: %s != %s", sig, sig2)
	}

	// Different secret → different output.
	sig3 := Sign(body, "other")
	if sig == sig3 {
		t.Fatal("different secrets should produce different signatures")
	}
}

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"agent_id":"test"}`)
	secret := "mysecret"
	sig := "sha256=" + Sign(body, secret)

	if !VerifySignature(body, secret, sig) {
		t.Fatal("expected valid signature to verify")
	}

	if VerifySignature(body, "wrong", sig) {
		t.Fatal("expected wrong secret to fail verification")
	}

	if VerifySignature([]byte("tampered"), secret, sig) {
		t.Fatal("expected tampered body to fail verification")
	}
}

func TestWebhookClient_Call(t *testing.T) {
	wantResp := &WebhookResponse{
		Status: "provisioned",
		Credentials: &Credential{
			Type:   "api_key",
			Token:  "sk_live_abc123",
			Header: "X-API-Key",
		},
	}

	// Mock webhook server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content type")
		}

		var req WebhookRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.AgentID != "bot-1" {
			t.Errorf("expected agent_id=bot-1, got %s", req.AgentID)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(wantResp)
	}))
	defer srv.Close()

	client := NewWebhookClient(srv.URL, "", 5*time.Second)
	resp, err := client.Call(&WebhookRequest{
		AgentID:       "bot-1",
		AgentName:     "TestBot",
		AgentProvider: "TestCo",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != "provisioned" {
		t.Errorf("expected provisioned, got %s", resp.Status)
	}
	if resp.Credentials.Token != "sk_live_abc123" {
		t.Errorf("expected token sk_live_abc123, got %s", resp.Credentials.Token)
	}
}

func TestWebhookClient_HMAC(t *testing.T) {
	secret := "test-secret"
	var gotSig string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Webhook-Signature")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(&WebhookResponse{Status: "provisioned"})
	}))
	defer srv.Close()

	client := NewWebhookClient(srv.URL, secret, 5*time.Second)
	_, err := client.Call(&WebhookRequest{AgentID: "bot", AgentName: "B", AgentProvider: "P", Timestamp: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotSig == "" {
		t.Fatal("expected X-Webhook-Signature header to be set")
	}
	if len(gotSig) < 10 || gotSig[:7] != "sha256=" {
		t.Fatalf("expected sha256= prefix, got %s", gotSig)
	}
}

func TestWebhookClient_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	client := NewWebhookClient(srv.URL, "", 5*time.Second)
	_, err := client.Call(&WebhookRequest{AgentID: "bot", AgentName: "B", AgentProvider: "P", Timestamp: "t"})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// ── Plugin Registration Endpoint Tests ──────────────────────────────────

func newTestPlugin(t *testing.T, webhookURL string) *Plugin {
	t.Helper()
	p := New()
	cfg := map[string]interface{}{
		"provisioning_webhook": webhookURL,
	}
	if err := p.Init(cfg); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	return p
}

func TestPlugin_RegisterSuccess(t *testing.T) {
	// Mock webhook server.
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(&WebhookResponse{
			Status: "provisioned",
			Credentials: &Credential{
				Type:   "api_key",
				Token:  "sk_test_123",
				Header: "X-API-Key",
			},
		})
	}))
	defer webhookSrv.Close()

	p := newTestPlugin(t, webhookSrv.URL)
	handler := p.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called for /agent/register")
	}))

	body, _ := json.Marshal(RegistrationRequest{
		AgentID:       "claude-bot",
		AgentName:     "ClaudeBot",
		AgentProvider: "Anthropic",
	})

	req := httptest.NewRequest(http.MethodPost, "/agent/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp RegistrationResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "provisioned" {
		t.Errorf("expected provisioned, got %s", resp.Status)
	}
	if resp.Credentials.Token != "sk_test_123" {
		t.Errorf("expected token sk_test_123, got %s", resp.Credentials.Token)
	}
}

func TestPlugin_RegisterRejected(t *testing.T) {
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(&WebhookResponse{
			Status: "rejected",
			Reason: "Agent provider not allowed",
		})
	}))
	defer webhookSrv.Close()

	p := newTestPlugin(t, webhookSrv.URL)
	handler := p.Middleware()(http.NotFoundHandler())

	body, _ := json.Marshal(RegistrationRequest{
		AgentID:       "bad-bot",
		AgentName:     "BadBot",
		AgentProvider: "Unknown",
	})

	req := httptest.NewRequest(http.MethodPost, "/agent/register", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}

	var resp RegistrationResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "rejected" {
		t.Errorf("expected rejected, got %s", resp.Status)
	}
}

func TestPlugin_RegisterMissingFields(t *testing.T) {
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("webhook should not be called for invalid requests")
	}))
	defer webhookSrv.Close()

	p := newTestPlugin(t, webhookSrv.URL)
	handler := p.Middleware()(http.NotFoundHandler())

	tests := []struct {
		name string
		body RegistrationRequest
		want string
	}{
		{"missing agent_id", RegistrationRequest{AgentName: "Bot", AgentProvider: "P"}, "agent_id"},
		{"missing agent_name", RegistrationRequest{AgentID: "bot", AgentProvider: "P"}, "agent_name"},
		{"missing agent_provider", RegistrationRequest{AgentID: "bot", AgentName: "Bot"}, "agent_provider"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/agent/register", bytes.NewReader(body))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", w.Code)
			}
		})
	}
}

func TestPlugin_RegisterRequireIdentity(t *testing.T) {
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("webhook should not be called without identity token")
	}))
	defer webhookSrv.Close()

	p := New()
	cfg := map[string]interface{}{
		"provisioning_webhook": webhookSrv.URL,
		"require_identity":     true,
	}
	if err := p.Init(cfg); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	handler := p.Middleware()(http.NotFoundHandler())
	body, _ := json.Marshal(RegistrationRequest{
		AgentID:       "bot",
		AgentName:     "Bot",
		AgentProvider: "P",
	})

	req := httptest.NewRequest(http.MethodPost, "/agent/register", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPlugin_RegisterAllowedProviders(t *testing.T) {
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("webhook should not be called for disallowed provider")
	}))
	defer webhookSrv.Close()

	p := New()
	cfg := map[string]interface{}{
		"provisioning_webhook": webhookSrv.URL,
		"allowed_providers":    []interface{}{"Anthropic", "OpenAI"},
	}
	if err := p.Init(cfg); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	handler := p.Middleware()(http.NotFoundHandler())
	body, _ := json.Marshal(RegistrationRequest{
		AgentID:       "bot",
		AgentName:     "Bot",
		AgentProvider: "Unknown",
	})

	req := httptest.NewRequest(http.MethodPost, "/agent/register", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

// ── 401 Auth Required Tests ─────────────────────────────────────────────

func TestPlugin_401ForUnauthenticated(t *testing.T) {
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer webhookSrv.Close()

	p := newTestPlugin(t, webhookSrv.URL)
	nextCalled := false
	handler := p.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if nextCalled {
		t.Error("next handler should not be called for unauthenticated requests")
	}

	var resp AuthRequiredResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.RegisterURL != "/agent/register" {
		t.Errorf("expected register_url=/agent/register, got %s", resp.RegisterURL)
	}
	if resp.Error != "auth_required" {
		t.Errorf("expected error=auth_required, got %s", resp.Error)
	}
}

func TestPlugin_PassthroughWithAuth(t *testing.T) {
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer webhookSrv.Close()

	p := newTestPlugin(t, webhookSrv.URL)

	tests := []struct {
		name   string
		header string
		value  string
	}{
		{"Authorization header", "Authorization", "Bearer token123"},
		{"X-API-Key header", "X-API-Key", "key123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled := false
			handler := p.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
			req.Header.Set(tt.header, tt.value)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if !nextCalled {
				t.Error("next handler should be called for authenticated requests")
			}
			if w.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", w.Code)
			}
		})
	}
}

func TestPlugin_PassthroughQueryParam(t *testing.T) {
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer webhookSrv.Close()

	p := newTestPlugin(t, webhookSrv.URL)
	nextCalled := false
	handler := p.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/data?api_key=key123", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !nextCalled {
		t.Error("next handler should be called when api_key query param present")
	}
}

func TestPlugin_DiscoveryPathsNotBlocked(t *testing.T) {
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer webhookSrv.Close()

	p := newTestPlugin(t, webhookSrv.URL)

	paths := []string{
		"/.well-known/ai",
		"/.well-known/agent.json",
		"/llms.txt",
		"/llms-full.txt",
		"/agents.txt",
		"/robots.txt",
		"/agent/register",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			handler := p.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			}))

			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			// /agent/register with GET won't match the POST handler, so it falls through
			// to shouldReturn401 check, which exempts it.
			if w.Code == http.StatusUnauthorized {
				t.Errorf("path %s should not return 401", path)
			}
		})
	}
}

// ── Rate Limiting Tests ─────────────────────────────────────────────────

func TestPlugin_RateLimit(t *testing.T) {
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(&WebhookResponse{Status: "provisioned", Credentials: &Credential{Type: "api_key", Token: "t"}})
	}))
	defer webhookSrv.Close()

	p := New()
	cfg := map[string]interface{}{
		"provisioning_webhook": webhookSrv.URL,
		"rate_limit": map[string]interface{}{
			"max_registrations": float64(2),
			"window":            "1h",
		},
	}
	if err := p.Init(cfg); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	handler := p.Middleware()(http.NotFoundHandler())

	makeReq := func() int {
		body, _ := json.Marshal(RegistrationRequest{
			AgentID: "bot", AgentName: "Bot", AgentProvider: "P",
		})
		req := httptest.NewRequest(http.MethodPost, "/agent/register", bytes.NewReader(body))
		req.RemoteAddr = "1.2.3.4:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Code
	}

	// First 2 should succeed.
	if code := makeReq(); code != http.StatusOK {
		t.Errorf("request 1: expected 200, got %d", code)
	}
	if code := makeReq(); code != http.StatusOK {
		t.Errorf("request 2: expected 200, got %d", code)
	}
	// Third should be rate limited.
	if code := makeReq(); code != http.StatusTooManyRequests {
		t.Errorf("request 3: expected 429, got %d", code)
	}
}

func TestPlugin_RateLimitWindowReset(t *testing.T) {
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(&WebhookResponse{Status: "provisioned", Credentials: &Credential{Type: "api_key", Token: "t"}})
	}))
	defer webhookSrv.Close()

	// Override time.
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	origNow := NowFunc
	NowFunc = func() time.Time { return now }
	defer func() { NowFunc = origNow }()

	p := New()
	cfg := map[string]interface{}{
		"provisioning_webhook": webhookSrv.URL,
		"rate_limit": map[string]interface{}{
			"max_registrations": float64(1),
			"window":            "1h",
		},
	}
	if err := p.Init(cfg); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	handler := p.Middleware()(http.NotFoundHandler())

	makeReq := func() int {
		body, _ := json.Marshal(RegistrationRequest{
			AgentID: "bot", AgentName: "Bot", AgentProvider: "P",
		})
		req := httptest.NewRequest(http.MethodPost, "/agent/register", bytes.NewReader(body))
		req.RemoteAddr = "1.2.3.4:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Code
	}

	if code := makeReq(); code != http.StatusOK {
		t.Errorf("request 1: expected 200, got %d", code)
	}
	if code := makeReq(); code != http.StatusTooManyRequests {
		t.Errorf("request 2: expected 429, got %d", code)
	}

	// Advance time past window.
	now = now.Add(2 * time.Hour)
	if code := makeReq(); code != http.StatusOK {
		t.Errorf("request 3 after reset: expected 200, got %d", code)
	}
}

// ── Init Tests ──────────────────────────────────────────────────────────

func TestPlugin_InitMissingWebhook(t *testing.T) {
	p := New()
	err := p.Init(map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for missing provisioning_webhook")
	}
}

func TestPlugin_InitDefaults(t *testing.T) {
	p := New()
	err := p.Init(map[string]interface{}{
		"provisioning_webhook": "http://example.com/provision",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.cfg.WebhookTimeout != 10*time.Second {
		t.Errorf("expected default timeout 10s, got %v", p.cfg.WebhookTimeout)
	}
	if p.cfg.RequireIdentity {
		t.Error("expected require_identity default false")
	}
}

// ── Client IP Tests ─────────────────────────────────────────────────────

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		xff        string
		xri        string
		remoteAddr string
		want       string
	}{
		{"X-Forwarded-For", "1.2.3.4, 5.6.7.8", "", "9.9.9.9:1234", "1.2.3.4"},
		{"X-Real-IP", "", "1.2.3.4", "9.9.9.9:1234", "1.2.3.4"},
		{"RemoteAddr", "", "", "9.9.9.9:1234", "9.9.9.9"},
		{"RemoteAddr no port", "", "", "9.9.9.9", "9.9.9.9"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.xri != "" {
				req.Header.Set("X-Real-IP", tt.xri)
			}
			got := clientIP(req)
			if got != tt.want {
				t.Errorf("expected %s, got %s", tt.want, got)
			}
		})
	}
}

// ── Webhook Failure Tests ───────────────────────────────────────────────

func TestPlugin_WebhookFailure(t *testing.T) {
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom"))
	}))
	defer webhookSrv.Close()

	p := newTestPlugin(t, webhookSrv.URL)
	handler := p.Middleware()(http.NotFoundHandler())

	body, _ := json.Marshal(RegistrationRequest{
		AgentID: "bot", AgentName: "Bot", AgentProvider: "P",
	})
	req := httptest.NewRequest(http.MethodPost, "/agent/register", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}
