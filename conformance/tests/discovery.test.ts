/**
 * Discovery endpoint conformance tests.
 *
 * Validates: GET /agents.txt, GET /llms.txt, GET /llms-full.txt
 */

import { describe, it, expect } from "vitest";
import { get, header } from "../helpers.js";

// ---------------------------------------------------------------------------
// GET /agents.txt
// ---------------------------------------------------------------------------

describe("GET /agents.txt", () => {
  it("returns 200", async () => {
    const r = await get("/agents.txt");
    expect(r.status).toBe(200);
  });

  it("has content-type text/plain", async () => {
    const r = await get("/agents.txt");
    const ct = header(r, "content-type") ?? "";
    expect(ct).toMatch(/text\/plain/);
  });

  it("body is non-empty", async () => {
    const r = await get("/agents.txt");
    expect(r.body.length).toBeGreaterThan(0);
  });

  it("contains a User-agent directive", async () => {
    const r = await get("/agents.txt");
    // agents.txt must contain at least one User-agent line
    expect(r.body).toMatch(/User-agent:/i);
  });

  it("contains Allow or Deny directives", async () => {
    const r = await get("/agents.txt");
    expect(r.body).toMatch(/\b(Allow|Deny):/i);
  });
});

// ---------------------------------------------------------------------------
// GET /llms.txt
// ---------------------------------------------------------------------------

describe("GET /llms.txt", () => {
  it("returns 200", async () => {
    const r = await get("/llms.txt");
    expect(r.status).toBe(200);
  });

  it("has content-type text/plain", async () => {
    const r = await get("/llms.txt");
    const ct = header(r, "content-type") ?? "";
    expect(ct).toMatch(/text\/plain/);
  });

  it("body is non-empty", async () => {
    const r = await get("/llms.txt");
    expect(r.body.length).toBeGreaterThan(0);
  });

  it("starts with a markdown heading (# Title)", async () => {
    const r = await get("/llms.txt");
    expect(r.body.trimStart()).toMatch(/^#\s+/);
  });

  it("contains a description block (> quote)", async () => {
    const r = await get("/llms.txt");
    expect(r.body).toMatch(/^>\s+/m);
  });
});

// ---------------------------------------------------------------------------
// GET /llms-full.txt
// ---------------------------------------------------------------------------

describe("GET /llms-full.txt", () => {
  it("returns 200", async () => {
    const r = await get("/llms-full.txt");
    expect(r.status).toBe(200);
  });

  it("has content-type text/plain", async () => {
    const r = await get("/llms-full.txt");
    const ct = header(r, "content-type") ?? "";
    expect(ct).toMatch(/text\/plain/);
  });

  it("body is non-empty and at least as long as llms.txt", async () => {
    const [full, short] = await Promise.all([
      get("/llms-full.txt"),
      get("/llms.txt"),
    ]);
    expect(full.body.length).toBeGreaterThanOrEqual(short.body.length);
  });

  it("starts with a markdown heading", async () => {
    const r = await get("/llms-full.txt");
    expect(r.body.trimStart()).toMatch(/^#\s+/);
  });
});
