package apikeys

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lightlayer-dev/gateway/internal/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestPlugin(t *testing.T) *Plugin {
	t.Helper()
	p := New()
	err := p.Init(map[string]interface{}{
		"prefix":     "test_",
		"admin_path": "/api/keys",
	})
	require.NoError(t, err)
	return p
}

// wrapWithContext wraps the handler so RequestContext is injected.
func wrapWithContext(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := &plugins.RequestContext{
			Metadata: make(map[string]interface{}),
		}
		ctx := plugins.WithRequestContext(r.Context(), rc)
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

func TestCreateAndListKeys(t *testing.T) {
	p := newTestPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	// Create a key.
	body, _ := json.Marshal(CreateKeyRequest{
		CompanyID: "acme",
		UserID:    "user1",
		Scopes:    []string{"read", "write"},
		Metadata:  map[string]interface{}{"env": "test"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)

	var createResp CreateKeyResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &createResp))
	assert.True(t, len(createResp.RawKey) > 0)
	assert.Contains(t, createResp.RawKey, "test_")
	assert.Equal(t, "acme", createResp.Key.CompanyID)
	assert.Equal(t, []string{"read", "write"}, createResp.Key.Scopes)

	// List keys.
	req = httptest.NewRequest(http.MethodGet, "/api/keys", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var listResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &listResp)
	keys := listResp["keys"].([]interface{})
	assert.Len(t, keys, 1)
}

func TestDeleteKey(t *testing.T) {
	p := newTestPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	// Create a key.
	body, _ := json.Marshal(CreateKeyRequest{
		CompanyID: "acme",
		UserID:    "user1",
		Scopes:    []string{"read"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var createResp CreateKeyResponse
	json.Unmarshal(w.Body.Bytes(), &createResp)

	// Delete it.
	req = httptest.NewRequest(http.MethodDelete, "/api/keys/"+createResp.Key.KeyID, nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var delResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &delResp)
	assert.Equal(t, true, delResp["deleted"])

	// List should be empty.
	req = httptest.NewRequest(http.MethodGet, "/api/keys", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	json.Unmarshal(w.Body.Bytes(), &delResp)
	keys := delResp["keys"].([]interface{})
	assert.Len(t, keys, 0)
}

func TestDeleteNonexistentKey(t *testing.T) {
	p := newTestPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodDelete, "/api/keys/nonexistent", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestKeyValidation(t *testing.T) {
	p := newTestPlugin(t)
	handler := wrapWithContext(p.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	// Create a key.
	store := p.GetStore().(*MemoryApiKeyStore)
	key := &ScopedApiKey{
		KeyID:     "test-key-id",
		CompanyID: "acme",
		UserID:    "user1",
		Scopes:    []string{"read"},
		CreatedAt: time.Now(),
	}
	store.Store("test_validkey123", key)

	// Request with valid key.
	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("Authorization", "Bearer test_validkey123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Request with invalid key.
	req = httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("Authorization", "Bearer test_invalidkey")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestExpiredKeyValidation(t *testing.T) {
	p := newTestPlugin(t)
	handler := wrapWithContext(p.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	store := p.GetStore().(*MemoryApiKeyStore)
	expired := time.Now().Add(-1 * time.Hour)
	key := &ScopedApiKey{
		KeyID:     "expired-key-id",
		CompanyID: "acme",
		UserID:    "user1",
		Scopes:    []string{"read"},
		ExpiresAt: &expired,
		CreatedAt: time.Now().Add(-2 * time.Hour),
	}
	store.Store("test_expiredkey", key)

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("Authorization", "Bearer test_expiredkey")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestExtractAPIKey(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(r *http.Request)
		expected string
	}{
		{
			name: "bearer token",
			setup: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer mykey123")
			},
			expected: "mykey123",
		},
		{
			name: "x-api-key header",
			setup: func(r *http.Request) {
				r.Header.Set("X-API-Key", "mykey456")
			},
			expected: "mykey456",
		},
		{
			name:     "query param",
			setup:    func(r *http.Request) {},
			expected: "mykey789",
		},
		{
			name:     "no key",
			setup:    func(r *http.Request) {},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := "/api/data"
			if tt.name == "query param" {
				path = "/api/data?api_key=mykey789"
			}
			req := httptest.NewRequest(http.MethodGet, path, nil)
			tt.setup(req)
			assert.Equal(t, tt.expected, ExtractAPIKey(req))
		})
	}
}

func TestHasScope(t *testing.T) {
	key := &ScopedApiKey{Scopes: []string{"read", "write"}}

	assert.True(t, HasScope(key, []string{"read"}))
	assert.True(t, HasScope(key, []string{"read", "write"}))
	assert.False(t, HasScope(key, []string{"admin"}))
	assert.False(t, HasScope(key, []string{"read", "admin"}))

	// Wildcard scope.
	wildcard := &ScopedApiKey{Scopes: []string{"*"}}
	assert.True(t, HasScope(wildcard, []string{"anything"}))
	assert.True(t, HasScope(wildcard, []string{"read", "write", "admin"}))
}

func TestNoKeyPassthrough(t *testing.T) {
	p := newTestPlugin(t)
	called := false
	handler := wrapWithContext(p.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCreateKeyValidation(t *testing.T) {
	p := newTestPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	// Missing scopes.
	body, _ := json.Marshal(CreateKeyRequest{
		CompanyID: "acme",
		UserID:    "user1",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateKeyWithExpiration(t *testing.T) {
	p := newTestPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	body, _ := json.Marshal(CreateKeyRequest{
		CompanyID: "acme",
		UserID:    "user1",
		Scopes:    []string{"read"},
		ExpiresAt: "2030-01-01T00:00:00Z",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var resp CreateKeyResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NotNil(t, resp.Key.ExpiresAt)
}

func TestXAPIKeyHeader(t *testing.T) {
	p := newTestPlugin(t)
	store := p.GetStore().(*MemoryApiKeyStore)
	store.Store("test_xapikey", &ScopedApiKey{
		KeyID:     "xapi-key-id",
		Scopes:    []string{"read"},
		CreatedAt: time.Now(),
	})

	handler := wrapWithContext(p.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("X-API-Key", "test_xapikey")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
