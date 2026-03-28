// Package mcp implements a JSON-RPC 2.0 MCP server that auto-generates
// tool definitions from gateway discovery capabilities config.
// Ported from agent-layer-ts mcp.ts.
package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/lightlayer-dev/gateway/internal/plugins"
)

func init() {
	plugins.Register("mcp", func() plugins.Plugin { return New() })
}

// ── Types ────────────────────────────────────────────────────────────────

// ToolDefinition is a single MCP tool.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// ServerInfo is returned during MCP initialize.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// JsonRpcRequest is a JSON-RPC 2.0 request.
type JsonRpcRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      interface{}            `json:"id,omitempty"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params,omitempty"`
}

// JsonRpcResponse is a JSON-RPC 2.0 response.
type JsonRpcResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id,omitempty"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JsonRpcError `json:"error,omitempty"`
}

// JsonRpcError is the error object in a JSON-RPC response.
type JsonRpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Capability mirrors config.Capability for tool generation.
type Capability struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Methods     []string `json:"methods"`
	Paths       []string `json:"paths"`
}

// Config holds MCP plugin configuration.
type Config struct {
	Endpoint     string
	Name         string
	Version      string
	Instructions string
	Tools        []ToolDefinition
	Capabilities []Capability
	OriginURL    string
}

// ToolCallHandler is called when tools/call is invoked.
type ToolCallHandler func(toolName string, args map[string]interface{}) (interface{}, error)

// ── Plugin ───────────────────────────────────────────────────────────────

// Plugin implements the MCP JSON-RPC 2.0 server.
type Plugin struct {
	cfg             Config
	tools           []ToolDefinition
	serverInfo      ServerInfo
	toolCallHandler ToolCallHandler
}

// New creates a new MCP plugin.
func New() *Plugin {
	return &Plugin{}
}

// SetToolCallHandler sets a custom handler for tools/call requests.
func (p *Plugin) SetToolCallHandler(h ToolCallHandler) {
	p.toolCallHandler = h
}

func (p *Plugin) Name() string { return "mcp" }

func (p *Plugin) Init(cfg map[string]interface{}) error {
	p.cfg.Endpoint = stringFromCfg(cfg, "endpoint", "/mcp")
	p.cfg.Name = stringFromCfg(cfg, "name", "LightLayer Gateway")
	p.cfg.Version = stringFromCfg(cfg, "version", "1.0.0")
	p.cfg.Instructions = stringFromCfg(cfg, "instructions", "")
	p.cfg.OriginURL = stringFromCfg(cfg, "origin_url", "")

	// Parse manual tool definitions.
	if tools, ok := cfg["tools"].([]interface{}); ok {
		for _, t := range tools {
			if tm, ok := t.(map[string]interface{}); ok {
				td := ToolDefinition{
					Name:        fmt.Sprintf("%v", tm["name"]),
					Description: fmt.Sprintf("%v", tm["description"]),
				}
				if is, ok := tm["input_schema"].(map[string]interface{}); ok {
					td.InputSchema = is
				} else {
					td.InputSchema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
				}
				p.cfg.Tools = append(p.cfg.Tools, td)
			}
		}
	}

	// Parse capabilities for auto-generation.
	if caps, ok := cfg["capabilities"].([]interface{}); ok {
		for _, c := range caps {
			if cm, ok := c.(map[string]interface{}); ok {
				cap := Capability{
					Name:        fmt.Sprintf("%v", cm["name"]),
					Description: fmt.Sprintf("%v", cm["description"]),
				}
				if methods, ok := cm["methods"].([]interface{}); ok {
					for _, m := range methods {
						cap.Methods = append(cap.Methods, fmt.Sprintf("%v", m))
					}
				}
				if methods, ok := cm["methods"].([]string); ok {
					cap.Methods = methods
				}
				if paths, ok := cm["paths"].([]interface{}); ok {
					for _, pa := range paths {
						cap.Paths = append(cap.Paths, fmt.Sprintf("%v", pa))
					}
				}
				if paths, ok := cm["paths"].([]string); ok {
					cap.Paths = paths
				}
				p.cfg.Capabilities = append(p.cfg.Capabilities, cap)
			}
		}
	}

	// Generate tools from capabilities.
	autoTools := GenerateToolDefinitions(p.cfg.Capabilities)

	// Merge: manual tools take precedence, then auto-generated.
	nameSet := make(map[string]bool)
	for _, t := range p.cfg.Tools {
		nameSet[t.Name] = true
	}
	p.tools = append(p.tools, p.cfg.Tools...)
	for _, t := range autoTools {
		if !nameSet[t.Name] {
			p.tools = append(p.tools, t)
		}
	}

	p.serverInfo = ServerInfo{
		Name:    p.cfg.Name,
		Version: p.cfg.Version,
	}

	slog.Info("mcp: initialized",
		"endpoint", p.cfg.Endpoint,
		"tools", len(p.tools),
	)
	return nil
}

func (p *Plugin) Close() error { return nil }

func (p *Plugin) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == p.cfg.Endpoint && r.Method == http.MethodPost {
				p.handleJsonRpc(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ── JSON-RPC Handler ─────────────────────────────────────────────────────

func (p *Plugin) handleJsonRpc(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		writeJsonRpcError(w, nil, -32700, "Parse error: could not read body")
		return
	}

	var req JsonRpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJsonRpcError(w, nil, -32700, "Parse error: invalid JSON")
		return
	}

	if req.JSONRPC != "2.0" {
		writeJsonRpcError(w, req.ID, -32600, "Invalid Request: jsonrpc must be \"2.0\"")
		return
	}

	// Notifications (no id) — acknowledge silently.
	if req.ID == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	resp := p.handleMethod(req)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Plugin) handleMethod(req JsonRpcRequest) JsonRpcResponse {
	switch req.Method {
	case "initialize":
		result := map[string]interface{}{
			"protocolVersion": "2025-03-26",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    p.serverInfo.Name,
				"version": p.serverInfo.Version,
			},
		}
		if p.cfg.Instructions != "" {
			result["instructions"] = p.cfg.Instructions
		}
		return JsonRpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}

	case "ping":
		return JsonRpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{}}

	case "tools/list":
		toolList := make([]map[string]interface{}, len(p.tools))
		for i, t := range p.tools {
			toolList[i] = map[string]interface{}{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.InputSchema,
			}
		}
		return JsonRpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
			"tools": toolList,
		}}

	case "tools/call":
		return p.handleToolCall(req)

	default:
		return JsonRpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JsonRpcError{Code: -32601, Message: fmt.Sprintf("Method not found: %s", req.Method)},
		}
	}
}

func (p *Plugin) handleToolCall(req JsonRpcRequest) JsonRpcResponse {
	params := req.Params
	name, _ := params["name"].(string)
	if name == "" {
		return JsonRpcResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &JsonRpcError{Code: -32602, Message: "Invalid params: tool name is required"},
		}
	}

	// Check tool exists.
	found := false
	for _, t := range p.tools {
		if t.Name == name {
			found = true
			break
		}
	}
	if !found {
		return JsonRpcResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &JsonRpcError{Code: -32602, Message: fmt.Sprintf("Unknown tool: %s", name)},
		}
	}

	if p.toolCallHandler == nil {
		return JsonRpcResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &JsonRpcError{Code: -32603, Message: "Tool call handler not configured"},
		}
	}

	args, _ := params["arguments"].(map[string]interface{})
	if args == nil {
		args = map[string]interface{}{}
	}

	result, err := p.toolCallHandler(name, args)
	if err != nil {
		return JsonRpcResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &JsonRpcError{Code: -32603, Message: err.Error()},
		}
	}

	return JsonRpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
}

// ── Tool Generation ──────────────────────────────────────────────────────

// FormatToolName converts HTTP method + path into a snake_case tool name.
// GET /api/users → get_api_users
// GET /api/users/:id → get_api_users_by_id
func FormatToolName(method, path string) string {
	clean := strings.Trim(path, "/")
	clean = strings.NewReplacer(
		":", "by_",
		"{", "by_",
		"}", "",
	).Replace(clean)
	// Replace non-alphanumeric with underscore.
	var b strings.Builder
	for _, c := range clean {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			b.WriteRune(c)
		} else {
			b.WriteRune('_')
		}
	}
	name := strings.ToLower(method) + "_" + b.String()
	// Collapse multiple underscores.
	for strings.Contains(name, "__") {
		name = strings.ReplaceAll(name, "__", "_")
	}
	name = strings.Trim(name, "_")
	return strings.ToLower(name)
}

// ParseToolName reverses FormatToolName: get_api_users → {GET, /api/users}.
func ParseToolName(toolName string) (method, path string) {
	parts := strings.SplitN(toolName, "_", 2)
	if len(parts) < 2 {
		return strings.ToUpper(toolName), "/"
	}
	method = strings.ToUpper(parts[0])

	segments := strings.Split(parts[1], "_")
	var result []string
	for i := 0; i < len(segments); i++ {
		if segments[i] == "by" && i+1 < len(segments) {
			result = append(result, ":"+segments[i+1])
			i++
		} else {
			result = append(result, segments[i])
		}
	}
	path = "/" + strings.Join(result, "/")
	return
}

// GenerateToolDefinitions creates MCP tools from capabilities config.
func GenerateToolDefinitions(capabilities []Capability) []ToolDefinition {
	var tools []ToolDefinition
	for _, cap := range capabilities {
		for _, method := range cap.Methods {
			for _, path := range cap.Paths {
				// Skip wildcard paths for tool generation; use the base path.
				cleanPath := strings.TrimSuffix(path, "/*")
				cleanPath = strings.TrimSuffix(cleanPath, "/*")
				name := FormatToolName(method, cleanPath)
				desc := fmt.Sprintf("%s %s", strings.ToUpper(method), cleanPath)
				if cap.Description != "" {
					desc = fmt.Sprintf("%s — %s", desc, cap.Description)
				}
				tools = append(tools, ToolDefinition{
					Name:        name,
					Description: desc,
					InputSchema: map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{},
					},
				})
			}
		}
	}
	return tools
}

// ── Helpers ──────────────────────────────────────────────────────────────

func writeJsonRpcError(w http.ResponseWriter, id interface{}, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(JsonRpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &JsonRpcError{Code: code, Message: msg},
	})
}

func stringFromCfg(cfg map[string]interface{}, key, def string) string {
	if v, ok := cfg[key].(string); ok && v != "" {
		return v
	}
	return def
}
