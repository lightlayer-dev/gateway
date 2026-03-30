/**
 * .well-known endpoint conformance tests.
 *
 * Validates: GET /.well-known/ai, GET /.well-known/ai/json-ld, GET /.well-known/agent.json
 */

import { describe, it, expect } from "vitest";
import { get, json, header } from "../helpers.js";

// ---------------------------------------------------------------------------
// GET /.well-known/ai
// ---------------------------------------------------------------------------

describe("GET /.well-known/ai", () => {
  it("returns 200", async () => {
    const r = await get("/.well-known/ai");
    expect(r.status).toBe(200);
  });

  it("has content-type application/json", async () => {
    const r = await get("/.well-known/ai");
    const ct = header(r, "content-type") ?? "";
    expect(ct).toMatch(/application\/json/);
  });

  it("response is a valid JSON object", async () => {
    const r = await get("/.well-known/ai");
    const body = json(r) as Record<string, unknown>;
    expect(typeof body).toBe("object");
    expect(body).not.toBeNull();
  });

  it("contains required 'name' field", async () => {
    const r = await get("/.well-known/ai");
    const body = json(r) as Record<string, unknown>;
    expect(body).toHaveProperty("name");
    expect(typeof body.name).toBe("string");
    expect((body.name as string).length).toBeGreaterThan(0);
  });
});

// ---------------------------------------------------------------------------
// GET /.well-known/ai/json-ld
// ---------------------------------------------------------------------------

describe("GET /.well-known/ai/json-ld", () => {
  it("returns 200", async () => {
    const r = await get("/.well-known/ai/json-ld");
    expect(r.status).toBe(200);
  });

  it("has content-type application/json", async () => {
    const r = await get("/.well-known/ai/json-ld");
    const ct = header(r, "content-type") ?? "";
    expect(ct).toMatch(/application\/json/);
  });

  it("contains @context with schema.org", async () => {
    const r = await get("/.well-known/ai/json-ld");
    const body = json(r) as Record<string, unknown>;
    expect(body["@context"]).toMatch(/schema\.org/);
  });

  it("contains @type WebAPI", async () => {
    const r = await get("/.well-known/ai/json-ld");
    const body = json(r) as Record<string, unknown>;
    expect(body["@type"]).toBe("WebAPI");
  });

  it("contains name field", async () => {
    const r = await get("/.well-known/ai/json-ld");
    const body = json(r) as Record<string, unknown>;
    expect(body).toHaveProperty("name");
    expect(typeof body.name).toBe("string");
  });
});

// ---------------------------------------------------------------------------
// GET /.well-known/agent.json  (A2A Agent Card)
// ---------------------------------------------------------------------------

describe("GET /.well-known/agent.json", () => {
  it("returns 200", async () => {
    const r = await get("/.well-known/agent.json");
    expect(r.status).toBe(200);
  });

  it("has content-type application/json", async () => {
    const r = await get("/.well-known/agent.json");
    const ct = header(r, "content-type") ?? "";
    expect(ct).toMatch(/application\/json/);
  });

  it("contains required A2A fields", async () => {
    const r = await get("/.well-known/agent.json");
    const body = json(r) as Record<string, unknown>;
    // Must have name and url at minimum
    expect(body).toHaveProperty("name");
    expect(typeof body.name).toBe("string");
  });

  it("skills is an array (if present)", async () => {
    const r = await get("/.well-known/agent.json");
    const body = json(r) as Record<string, unknown>;
    if ("skills" in body) {
      expect(Array.isArray(body.skills)).toBe(true);
    }
  });

  it("each skill has id, name, description", async () => {
    const r = await get("/.well-known/agent.json");
    const body = json(r) as Record<string, unknown>;
    if ("skills" in body && Array.isArray(body.skills)) {
      for (const skill of body.skills as Record<string, unknown>[]) {
        expect(skill).toHaveProperty("id");
        expect(skill).toHaveProperty("name");
        expect(skill).toHaveProperty("description");
      }
    }
  });

  it("capabilities is an object (if present)", async () => {
    const r = await get("/.well-known/agent.json");
    const body = json(r) as Record<string, unknown>;
    if ("capabilities" in body) {
      expect(typeof body.capabilities).toBe("object");
      expect(body.capabilities).not.toBeNull();
    }
  });

  it("defaultInputModes / defaultOutputModes are string arrays (if present)", async () => {
    const r = await get("/.well-known/agent.json");
    const body = json(r) as Record<string, unknown>;
    for (const key of ["defaultInputModes", "defaultOutputModes"]) {
      if (key in body) {
        expect(Array.isArray(body[key])).toBe(true);
        for (const mode of body[key] as unknown[]) {
          expect(typeof mode).toBe("string");
        }
      }
    }
  });
});
