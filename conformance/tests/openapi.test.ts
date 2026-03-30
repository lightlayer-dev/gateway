/**
 * OpenAPI endpoint conformance test.
 *
 * Validates: GET /openapi.json
 */

import { describe, it, expect } from "vitest";
import { get, json, header } from "../helpers.js";

describe("GET /openapi.json", () => {
  it("returns 200", async () => {
    const r = await get("/openapi.json");
    expect(r.status).toBe(200);
  });

  it("has content-type application/json", async () => {
    const r = await get("/openapi.json");
    const ct = header(r, "content-type") ?? "";
    expect(ct).toMatch(/application\/json/);
  });

  it("is valid JSON", async () => {
    const r = await get("/openapi.json");
    const body = json(r);
    expect(typeof body).toBe("object");
  });

  it("contains openapi version field", async () => {
    const r = await get("/openapi.json");
    const body = json(r) as Record<string, unknown>;
    // OpenAPI 3.x uses "openapi", Swagger 2.x uses "swagger"
    const hasVersion =
      typeof body.openapi === "string" || typeof body.swagger === "string";
    expect(hasVersion).toBe(true);
  });

  it("contains info object with title", async () => {
    const r = await get("/openapi.json");
    const body = json(r) as Record<string, unknown>;
    expect(body).toHaveProperty("info");
    const info = body.info as Record<string, unknown>;
    expect(info).toHaveProperty("title");
    expect(typeof info.title).toBe("string");
  });

  it("contains paths object", async () => {
    const r = await get("/openapi.json");
    const body = json(r) as Record<string, unknown>;
    expect(body).toHaveProperty("paths");
    expect(typeof body.paths).toBe("object");
  });
});
