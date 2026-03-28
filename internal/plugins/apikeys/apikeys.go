// Package apikeys implements scoped API key authentication and management.
// Ported from agent-layer-ts api-keys.ts.
package apikeys

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lightlayer-dev/gateway/internal/plugins"
)

func init() {
	plugins.Register("api_keys", func() plugins.Plugin { return New() })
}

// ── Types ────────────────────────────────────────────────────────────────

// ScopedApiKey represents an API key with scoped access.
type ScopedApiKey struct {
	KeyID     string                 `json:"key_id"`
	CompanyID string                 `json:"company_id"`
	UserID    string                 `json:"user_id"`
	Scopes    []string               `json:"scopes"`
	ExpiresAt *time.Time             `json:"expires_at,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
}

// ApiKeyStore is the interface for resolving raw keys to scoped keys.
type ApiKeyStore interface {
	Resolve(rawKey string) (*ScopedApiKey, error)
	Store(rawKey string, key *ScopedApiKey) error
	Delete(keyID string) error
	List() ([]*ScopedApiKey, error)
}

// CreateKeyRequest is the JSON body for POST /api/keys.
type CreateKeyRequest struct {
	CompanyID string                 `json:"company_id"`
	UserID    string                 `json:"user_id"`
	Scopes    []string               `json:"scopes"`
	ExpiresAt string                 `json:"expires_at,omitempty"` // RFC 3339
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// CreateKeyResponse is the JSON response for key creation.
type CreateKeyResponse struct {
	RawKey string       `json:"raw_key"`
	Key    ScopedApiKey `json:"key"`
}

// ── MemoryApiKeyStore ────────────────────────────────────────────────────

// MemoryApiKeyStore is an in-memory API key store for development.
type MemoryApiKeyStore struct {
	mu   sync.RWMutex
	keys map[string]*ScopedApiKey // rawKey → key
	byID map[string]string        // keyID → rawKey
}

// NewMemoryStore creates a new in-memory key store.
func NewMemoryStore() *MemoryApiKeyStore {
	return &MemoryApiKeyStore{
		keys: make(map[string]*ScopedApiKey),
		byID: make(map[string]string),
	}
}

func (s *MemoryApiKeyStore) Resolve(rawKey string) (*ScopedApiKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key, ok := s.keys[rawKey]
	if !ok {
		return nil, nil
	}
	return key, nil
}

func (s *MemoryApiKeyStore) Store(rawKey string, key *ScopedApiKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[rawKey] = key
	s.byID[key.KeyID] = rawKey
	return nil
}

func (s *MemoryApiKeyStore) Delete(keyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rawKey, ok := s.byID[keyID]
	if !ok {
		return fmt.Errorf("key not found: %s", keyID)
	}
	delete(s.keys, rawKey)
	delete(s.byID, keyID)
	return nil
}

func (s *MemoryApiKeyStore) List() ([]*ScopedApiKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*ScopedApiKey, 0, len(s.keys))
	for _, key := range s.keys {
		result = append(result, key)
	}
	return result, nil
}

// ── Key Generation ───────────────────────────────────────────────────────

// GenerateAPIKey creates a new prefixed API key.
func GenerateAPIKey(prefix string) string {
	b := make([]byte, 16)
	rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// GenerateKeyID creates a random key identifier.
func GenerateKeyID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ── Validation ───────────────────────────────────────────────────────────

// ValidateKey checks if a raw API key is valid (exists, not expired).
func ValidateKey(store ApiKeyStore, rawKey string, now time.Time) (*ScopedApiKey, string) {
	key, err := store.Resolve(rawKey)
	if err != nil || key == nil {
		return nil, "invalid_api_key"
	}
	if key.ExpiresAt != nil && now.After(*key.ExpiresAt) {
		return nil, "api_key_expired"
	}
	return key, ""
}

// HasScope checks if a key has all required scopes. Wildcard "*" grants all.
func HasScope(key *ScopedApiKey, required []string) bool {
	for _, s := range key.Scopes {
		if s == "*" {
			return true
		}
	}
	scopeSet := make(map[string]bool, len(key.Scopes))
	for _, s := range key.Scopes {
		scopeSet[s] = true
	}
	for _, r := range required {
		if !scopeSet[r] {
			return false
		}
	}
	return true
}

// ExtractAPIKey extracts an API key from the request.
// Checks: Authorization: Bearer <key>, X-API-Key header, ?api_key query param.
func ExtractAPIKey(r *http.Request) string {
	// Authorization: Bearer <key>
	if auth := r.Header.Get("Authorization"); auth != "" {
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			return strings.TrimSpace(parts[1])
		}
	}
	// X-API-Key header.
	if key := r.Header.Get("X-API-Key"); key != "" {
		return key
	}
	// Query param.
	if key := r.URL.Query().Get("api_key"); key != "" {
		return key
	}
	return ""
}

// ── Plugin ───────────────────────────────────────────────────────────────

// Config holds API keys plugin configuration.
type Config struct {
	Prefix    string
	AdminPath string
	Store     ApiKeyStore
}

// Plugin implements the API keys authentication plugin.
type Plugin struct {
	cfg Config

	// NowFunc allows overriding time.Now for testing.
	NowFunc func() time.Time
}

// New creates a new API keys plugin.
func New() *Plugin {
	return &Plugin{NowFunc: time.Now}
}

func (p *Plugin) Name() string { return "api_keys" }

func (p *Plugin) Init(cfg map[string]interface{}) error {
	p.cfg.Prefix = "llgw_"
	if v, ok := cfg["prefix"].(string); ok && v != "" {
		p.cfg.Prefix = v
	}

	p.cfg.AdminPath = "/api/keys"
	if v, ok := cfg["admin_path"].(string); ok && v != "" {
		p.cfg.AdminPath = v
	}

	// Use memory store by default.
	store := NewMemoryStore()
	p.cfg.Store = store

	// Pre-load configured keys.
	if keys, ok := cfg["keys"].([]interface{}); ok {
		for _, k := range keys {
			km, ok := k.(map[string]interface{})
			if !ok {
				continue
			}
			rawKey := GenerateAPIKey(p.cfg.Prefix)
			key := &ScopedApiKey{
				KeyID:     fmt.Sprintf("%v", km["id"]),
				CompanyID: fmt.Sprintf("%v", km["company_id"]),
				UserID:    fmt.Sprintf("%v", km["user_id"]),
				CreatedAt: p.NowFunc(),
			}
			if scopes, ok := km["scopes"].([]interface{}); ok {
				for _, s := range scopes {
					key.Scopes = append(key.Scopes, fmt.Sprintf("%v", s))
				}
			}
			if scopes, ok := km["scopes"].([]string); ok {
				key.Scopes = scopes
			}
			if exp, ok := km["expires_at"].(string); ok && exp != "" {
				if t, err := time.Parse(time.RFC3339, exp); err == nil {
					key.ExpiresAt = &t
				}
			}
			if meta, ok := km["metadata"].(map[string]interface{}); ok {
				key.Metadata = meta
			}
			store.Store(rawKey, key)
		}
	}

	slog.Info("api_keys: initialized",
		"prefix", p.cfg.Prefix,
		"admin_path", p.cfg.AdminPath,
	)
	return nil
}

func (p *Plugin) Close() error { return nil }

// GetStore returns the underlying store (for testing/admin use).
func (p *Plugin) GetStore() ApiKeyStore { return p.cfg.Store }

func (p *Plugin) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Handle admin API endpoints.
			if strings.HasPrefix(r.URL.Path, p.cfg.AdminPath) {
				p.handleAdmin(w, r)
				return
			}

			// Extract and validate API key.
			rawKey := ExtractAPIKey(r)
			if rawKey == "" {
				// No API key present — let request continue (other auth may apply).
				next.ServeHTTP(w, r)
				return
			}

			key, errMsg := ValidateKey(p.cfg.Store, rawKey, p.NowFunc())
			if key == nil {
				plugins.WriteError(w, http.StatusUnauthorized, errMsg,
					"Invalid or expired API key")
				return
			}

			// Store key info in request context metadata.
			if rc := plugins.GetRequestContext(r.Context()); rc != nil {
				rc.Metadata["api_key"] = key
				rc.Metadata["api_key_id"] = key.KeyID
				rc.Metadata["api_key_scopes"] = key.Scopes
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ── Admin API ────────────────────────────────────────────────────────────

func (p *Plugin) handleAdmin(w http.ResponseWriter, r *http.Request) {
	// POST /api/keys → create key
	// GET /api/keys → list keys
	// DELETE /api/keys/:id → revoke key
	switch r.Method {
	case http.MethodPost:
		if r.URL.Path == p.cfg.AdminPath {
			p.handleCreateKey(w, r)
			return
		}
	case http.MethodGet:
		if r.URL.Path == p.cfg.AdminPath {
			p.handleListKeys(w, r)
			return
		}
	case http.MethodDelete:
		// Extract key ID from path: /api/keys/{id}
		suffix := strings.TrimPrefix(r.URL.Path, p.cfg.AdminPath+"/")
		if suffix != "" && suffix != r.URL.Path {
			p.handleDeleteKey(w, r, suffix)
			return
		}
	}

	plugins.WriteError(w, http.StatusNotFound, "not_found", "API key endpoint not found")
}

func (p *Plugin) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	var req CreateKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		plugins.WriteError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
		return
	}

	if len(req.Scopes) == 0 {
		plugins.WriteError(w, http.StatusBadRequest, "invalid_request", "scopes is required")
		return
	}

	rawKey := GenerateAPIKey(p.cfg.Prefix)
	key := &ScopedApiKey{
		KeyID:     GenerateKeyID(),
		CompanyID: req.CompanyID,
		UserID:    req.UserID,
		Scopes:    req.Scopes,
		Metadata:  req.Metadata,
		CreatedAt: p.NowFunc(),
	}

	if req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			plugins.WriteError(w, http.StatusBadRequest, "invalid_request",
				"Invalid expires_at format, use RFC 3339")
			return
		}
		key.ExpiresAt = &t
	}

	if err := p.cfg.Store.Store(rawKey, key); err != nil {
		plugins.WriteError(w, http.StatusInternalServerError, "internal_error",
			"Failed to store API key")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(CreateKeyResponse{RawKey: rawKey, Key: *key})
}

func (p *Plugin) handleListKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := p.cfg.Store.List()
	if err != nil {
		plugins.WriteError(w, http.StatusInternalServerError, "internal_error",
			"Failed to list API keys")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"keys": keys,
	})
}

func (p *Plugin) handleDeleteKey(w http.ResponseWriter, r *http.Request, keyID string) {
	if err := p.cfg.Store.Delete(keyID); err != nil {
		plugins.WriteError(w, http.StatusNotFound, "not_found",
			fmt.Sprintf("API key not found: %s", keyID))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"deleted": true,
		"key_id":  keyID,
	})
}
