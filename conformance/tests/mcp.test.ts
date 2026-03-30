/**
 * MCP (Model Context Protocol) JSON-RPC conformance tests.
 *
 * Validates: POST /.well-known/mcp with initialize, tools/list, tools/call
 */

import { describe, it, expect } from "vitest";
import { postJson, json } from "../helpers.js";

const MCP_PATH = "/.well-known/mcp";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function rpc(method: string, params: unknown = {}, id: string | number = 1) {
  return { jsonrpc: "2.0", id, method, params };
}

// ---------------------------------------------------------------------------
// initialize
// ---------------------------------------------------------------------------

describe("MCP initialize", () => {
  it("returns a valid JSON-RPC response", async () => {
    const r = await postJson(MCP_PATH, rpc("initialize"));
    expect(r.status).toBe(200);

    const body = json(r) as Record<string, unknown>;
    expect(body.jsonrpc).toBe("2.0");
    expect(body.id).toBeDefined();
  });

  it("result contains protocolVersion", async () => {
    const r = await postJson(MCP_PATH, rpc("initialize"));
    const body = json(r) as Record<string, unknown>;
    const result = body.result as Record<string, unknown>;
    expect(result).toBeDefined();
    expect(typeof result.protocolVersion).toBe("string");
  });

  it("result contains serverInfo with name", async () => {
    const r = await postJson(MCP_PATH, rpc("initialize"));
    const body = json(r) as Record<string, unknown>;
    const result = body.result as Record<string, unknown>;
    const serverInfo = result.serverInfo as Record<string, unknown>;
    expect(serverInfo).toBeDefined();
    expect(typeof serverInfo.name).toBe("string");
  });

  it("result contains capabilities object", async () => {
    const r = await postJson(MCP_PATH, rpc("initialize"));
    const body = json(r) as Record<string, unknown>;
    const result = body.result as Record<string, unknown>;
    expect(result).toHaveProperty("capabilities");
    expect(typeof result.capabilities).toBe("object");
  });
});

// ---------------------------------------------------------------------------
// tools/list
// ---------------------------------------------------------------------------

describe("MCP tools/list", () => {
  it("returns tools array", async () => {
    const r = await postJson(MCP_PATH, rpc("tools/list", {}, 2));
    expect(r.status).toBe(200);

    const body = json(r) as Record<string, unknown>;
    expect(body.jsonrpc).toBe("2.0");
    const result = body.result as Record<string, unknown>;
    expect(result).toBeDefined();
    expect(Array.isArray(result.tools)).toBe(true);
  });

  it("each tool has name, description, inputSchema", async () => {
    const r = await postJson(MCP_PATH, rpc("tools/list", {}, 2));
    const body = json(r) as Record<string, unknown>;
    const result = body.result as Record<string, unknown>;
    const tools = result.tools as Record<string, unknown>[];

    for (const tool of tools) {
      expect(typeof tool.name).toBe("string");
      expect(typeof tool.description).toBe("string");
      expect(tool.inputSchema).toBeDefined();
      expect(typeof tool.inputSchema).toBe("object");
    }
  });

  it("tool inputSchema has type 'object'", async () => {
    const r = await postJson(MCP_PATH, rpc("tools/list", {}, 2));
    const body = json(r) as Record<string, unknown>;
    const result = body.result as Record<string, unknown>;
    const tools = result.tools as Record<string, unknown>[];

    for (const tool of tools) {
      const schema = tool.inputSchema as Record<string, unknown>;
      expect(schema.type).toBe("object");
    }
  });
});

// ---------------------------------------------------------------------------
// tools/call
// ---------------------------------------------------------------------------

describe("MCP tools/call", () => {
  it("returns error for unknown tool", async () => {
    const r = await postJson(
      MCP_PATH,
      rpc("tools/call", { name: "__nonexistent_tool__", arguments: {} }, 3),
    );
    expect(r.status).toBe(200);

    const body = json(r) as Record<string, unknown>;
    expect(body.jsonrpc).toBe("2.0");
    // Should be a JSON-RPC error (code -32602 or similar)
    expect(body.error).toBeDefined();
    const err = body.error as Record<string, unknown>;
    expect(typeof err.code).toBe("number");
    expect(typeof err.message).toBe("string");
  });

  it("returns content array for a valid tool", async () => {
    // First, get the tool list so we know a valid tool name
    const listRes = await postJson(MCP_PATH, rpc("tools/list", {}, 10));
    const listBody = json(listRes) as Record<string, unknown>;
    const result = listBody.result as Record<string, unknown>;
    const tools = result.tools as Record<string, unknown>[];

    if (tools.length === 0) {
      // No tools registered — skip this test
      return;
    }

    const toolName = tools[0].name as string;
    const callRes = await postJson(
      MCP_PATH,
      rpc("tools/call", { name: toolName, arguments: {} }, 11),
    );

    const callBody = json(callRes) as Record<string, unknown>;
    expect(callBody.jsonrpc).toBe("2.0");

    // Should have either result.content or an error (if origin is unreachable)
    if (callBody.result) {
      const callResult = callBody.result as Record<string, unknown>;
      expect(Array.isArray(callResult.content)).toBe(true);
      const content = callResult.content as Record<string, unknown>[];
      for (const item of content) {
        expect(typeof item.type).toBe("string");
      }
    }
    // If it's an error (e.g. origin down), that's still a valid JSON-RPC response
  });
});

// ---------------------------------------------------------------------------
// notifications/initialized (no response expected)
// ---------------------------------------------------------------------------

describe("MCP notifications/initialized", () => {
  it("accepts notification without id (no response body required)", async () => {
    const r = await postJson(MCP_PATH, {
      jsonrpc: "2.0",
      method: "notifications/initialized",
    });
    // Server may return 200 with empty body or 204
    expect([200, 202, 204]).toContain(r.status);
  });
});

// ---------------------------------------------------------------------------
// Invalid JSON-RPC
// ---------------------------------------------------------------------------

describe("MCP invalid requests", () => {
  it("returns JSON-RPC error for unknown method", async () => {
    const r = await postJson(MCP_PATH, rpc("nonexistent/method", {}, 99));
    const body = json(r) as Record<string, unknown>;
    expect(body.jsonrpc).toBe("2.0");
    expect(body.error).toBeDefined();
    const err = body.error as Record<string, unknown>;
    expect(typeof err.code).toBe("number");
  });
});
