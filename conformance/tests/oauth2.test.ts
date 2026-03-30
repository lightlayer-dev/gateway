/**
 * OAuth2 discovery conformance test.
 *
 * Validates: GET /.well-known/oauth-authorization-server
 *
 * Note: This test only validates that the endpoint exists and returns the
 * expected structure. It does NOT attempt actual OAuth2 flows.
 */

import { describe, it, expect } from "vitest";
import { get, json, header } from "../helpers.js";

describe("GET /.well-known/oauth-authorization-server", () => {
  it("returns 200 (if OAuth2 is configured)", async () => {
    const r = await get("/.well-known/oauth-authorization-server");
    // 200 = OAuth2 configured, 404 = not configured (both are valid)
    expect([200, 404]).toContain(r.status);
  });

  it("returns JSON content-type (when 200)", async () => {
    const r = await get("/.well-known/oauth-authorization-server");
    if (r.status !== 200) return; // Skip if not configured

    const ct = header(r, "content-type") ?? "";
    expect(ct).toMatch(/application\/json/);
  });

  it("contains issuer field (when 200)", async () => {
    const r = await get("/.well-known/oauth-authorization-server");
    if (r.status !== 200) return;

    const body = json(r) as Record<string, unknown>;
    expect(body).toHaveProperty("issuer");
    expect(typeof body.issuer).toBe("string");
  });

  it("contains authorization_endpoint (when 200)", async () => {
    const r = await get("/.well-known/oauth-authorization-server");
    if (r.status !== 200) return;

    const body = json(r) as Record<string, unknown>;
    expect(body).toHaveProperty("authorization_endpoint");
    expect(typeof body.authorization_endpoint).toBe("string");
  });

  it("contains token_endpoint (when 200)", async () => {
    const r = await get("/.well-known/oauth-authorization-server");
    if (r.status !== 200) return;

    const body = json(r) as Record<string, unknown>;
    expect(body).toHaveProperty("token_endpoint");
    expect(typeof body.token_endpoint).toBe("string");
  });

  it("contains response_types_supported array (when 200)", async () => {
    const r = await get("/.well-known/oauth-authorization-server");
    if (r.status !== 200) return;

    const body = json(r) as Record<string, unknown>;
    if ("response_types_supported" in body) {
      expect(Array.isArray(body.response_types_supported)).toBe(true);
    }
  });

  it("contains grant_types_supported array (when 200)", async () => {
    const r = await get("/.well-known/oauth-authorization-server");
    if (r.status !== 200) return;

    const body = json(r) as Record<string, unknown>;
    if ("grant_types_supported" in body) {
      expect(Array.isArray(body.grant_types_supported)).toBe(true);
    }
  });
});
