package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestPlugin(t *testing.T) *Plugin {
	t.Helper()
	p := New()
	err := p.Init(map[string]interface{}{
		"name":         "Test API",
		"version":      "2.0.0",
		"endpoint":     "/mcp",
		"instructions": "Use these tools to interact with the Test API",
		"capabilities": []interface{}{
			map[string]interface{}{
				"name":        "users",
				"description": "User management",
				"methods":     []interface{}{"GET", "POST"},
				"paths":       []interface{}{"/api/users", "/api/users/:id"},
			},
		},
	})
	require.NoError(t, err)
	return p
}

func sendJsonRpc(handler http.Handler, method string, params map[string]interface{}, id interface{}) *httptest.ResponseRecorder {
	req := JsonRpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	body, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestInitialize(t *testing.T) {
	p := newTestPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	w := sendJsonRpc(handler, "initialize", nil, 1)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp JsonRpcResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, float64(1), resp.ID)
	assert.Nil(t, resp.Error)

	result, ok := resp.Result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "2025-03-26", result["protocolVersion"])
	assert.Equal(t, "Use these tools to interact with the Test API", result["instructions"])

	serverInfo := result["serverInfo"].(map[string]interface{})
	assert.Equal(t, "Test API", serverInfo["name"])
	assert.Equal(t, "2.0.0", serverInfo["version"])
}

func TestToolsList(t *testing.T) {
	p := newTestPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	w := sendJsonRpc(handler, "tools/list", nil, 2)

	var resp JsonRpcResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Nil(t, resp.Error)

	result := resp.Result.(map[string]interface{})
	tools := result["tools"].([]interface{})

	// 2 methods × 2 paths = 4 tools.
	assert.Len(t, tools, 4)

	// Check tool names exist.
	names := make(map[string]bool)
	for _, t := range tools {
		tm := t.(map[string]interface{})
		names[tm["name"].(string)] = true
	}
	assert.True(t, names["get_api_users"])
	assert.True(t, names["post_api_users"])
	assert.True(t, names["get_api_users_by_id"])
	assert.True(t, names["post_api_users_by_id"])
}

func TestToolsCallWithHandler(t *testing.T) {
	p := newTestPlugin(t)
	p.SetToolCallHandler(func(toolName string, args map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": "Hello from " + toolName},
			},
		}, nil
	})
	handler := p.Middleware()(http.NotFoundHandler())

	w := sendJsonRpc(handler, "tools/call", map[string]interface{}{
		"name":      "get_api_users",
		"arguments": map[string]interface{}{},
	}, 3)

	var resp JsonRpcResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Nil(t, resp.Error)

	result := resp.Result.(map[string]interface{})
	content := result["content"].([]interface{})
	assert.Len(t, content, 1)
	item := content[0].(map[string]interface{})
	assert.Equal(t, "Hello from get_api_users", item["text"])
}

func TestToolsCallUnknownTool(t *testing.T) {
	p := newTestPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	w := sendJsonRpc(handler, "tools/call", map[string]interface{}{
		"name": "nonexistent_tool",
	}, 4)

	var resp JsonRpcResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, -32602, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "Unknown tool")
}

func TestToolsCallNoHandler(t *testing.T) {
	p := newTestPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	w := sendJsonRpc(handler, "tools/call", map[string]interface{}{
		"name": "get_api_users",
	}, 5)

	var resp JsonRpcResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, -32603, resp.Error.Code)
}

func TestToolsCallMissingName(t *testing.T) {
	p := newTestPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	w := sendJsonRpc(handler, "tools/call", map[string]interface{}{}, 6)

	var resp JsonRpcResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, -32602, resp.Error.Code)
}

func TestMethodNotFound(t *testing.T) {
	p := newTestPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	w := sendJsonRpc(handler, "unknown/method", nil, 7)

	var resp JsonRpcResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, -32601, resp.Error.Code)
}

func TestPing(t *testing.T) {
	p := newTestPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	w := sendJsonRpc(handler, "ping", nil, 8)

	var resp JsonRpcResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)
}

func TestNotification(t *testing.T) {
	p := newTestPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	// Notification has no ID.
	w := sendJsonRpc(handler, "notifications/initialized", nil, nil)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestPassthroughNonMCPPaths(t *testing.T) {
	p := newTestPlugin(t)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := p.Middleware()(next)

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.True(t, called)
}

func TestFormatToolName(t *testing.T) {
	tests := []struct {
		method, path, expected string
	}{
		{"GET", "/api/users", "get_api_users"},
		{"POST", "/api/users/create", "post_api_users_create"},
		{"GET", "/api/users/:id", "get_api_users_by_id"},
		{"PUT", "/api/users/{id}", "put_api_users_by_id"},
		{"DELETE", "/api/users/:id/posts/:postId", "delete_api_users_by_id_posts_by_postid"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, FormatToolName(tt.method, tt.path))
		})
	}
}

func TestParseToolName(t *testing.T) {
	method, path := ParseToolName("get_api_users")
	assert.Equal(t, "GET", method)
	assert.Equal(t, "/api/users", path)

	method, path = ParseToolName("get_api_users_by_id")
	assert.Equal(t, "GET", method)
	assert.Equal(t, "/api/users/:id", path)
}

func TestAutoGeneratedToolsMatchCapabilities(t *testing.T) {
	caps := []Capability{
		{
			Name:        "widgets",
			Description: "Widget operations",
			Methods:     []string{"GET", "POST", "DELETE"},
			Paths:       []string{"/api/widgets", "/api/widgets/:id"},
		},
	}

	tools := GenerateToolDefinitions(caps)

	// 3 methods × 2 paths = 6 tools.
	assert.Len(t, tools, 6)

	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name] = true
		assert.NotEmpty(t, tool.Description)
		assert.NotNil(t, tool.InputSchema)
		assert.Contains(t, tool.Description, "Widget operations")
	}

	assert.True(t, names["get_api_widgets"])
	assert.True(t, names["post_api_widgets"])
	assert.True(t, names["delete_api_widgets"])
	assert.True(t, names["get_api_widgets_by_id"])
	assert.True(t, names["post_api_widgets_by_id"])
	assert.True(t, names["delete_api_widgets_by_id"])
}

func TestManualToolsMergedWithAutoGenerated(t *testing.T) {
	p := New()
	err := p.Init(map[string]interface{}{
		"name":    "Test",
		"version": "1.0.0",
		"tools": []interface{}{
			map[string]interface{}{
				"name":        "custom_tool",
				"description": "A custom tool",
			},
		},
		"capabilities": []interface{}{
			map[string]interface{}{
				"name":    "items",
				"methods": []interface{}{"GET"},
				"paths":   []interface{}{"/api/items"},
			},
		},
	})
	require.NoError(t, err)

	// Should have custom_tool + auto-generated get_api_items.
	assert.Len(t, p.tools, 2)
	names := make(map[string]bool)
	for _, tool := range p.tools {
		names[tool.Name] = true
	}
	assert.True(t, names["custom_tool"])
	assert.True(t, names["get_api_items"])
}

func TestInvalidJSON(t *testing.T) {
	p := newTestPlugin(t)
	handler := p.Middleware()(http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp JsonRpcResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, -32700, resp.Error.Code)
}
