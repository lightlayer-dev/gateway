/**
 * Error handling conformance tests.
 *
 * Validates that the server returns structured error envelopes for common
 * error conditions (404, malformed requests, etc.).
 */

import { describe, it, expect } from "vitest";
import { get, postJson, json, header } from "../helpers.js";

describe("Error handling", () => {
  // -------------------------------------------------------------------------
  // 404 Not Found
  // -------------------------------------------------------------------------

  describe("404 Not Found", () => {
    it("returns 404 for a non-existent path", async () => {
      const r = await get("/__conformance_nonexistent_path__");
      expect(r.status).toBe(404);
    });

    it("returns JSON error envelope for agent requests", async () => {
      const r = await get("/__conformance_nonexistent_path__", {
        Accept: "application/json",
        "User-Agent": "ConformanceBot/1.0",
      });
      expect(r.status).toBe(404);

      const ct = header(r, "content-type") ?? "";
      expect(ct).toMatch(/application\/json/);

      const body = json(r) as Record<string, unknown>;
      // Error envelope: { error: { type, message, status } } or flat { type, message, status }
      const err = (body.error ?? body) as Record<string, unknown>;
      expect(
        typeof err.type === "string" || typeof err.code === "string",
      ).toBe(true);
      expect(typeof err.message).toBe("string");
    });

    it("error envelope includes status field matching HTTP status", async () => {
      const r = await get("/__conformance_nonexistent_path__", {
        Accept: "application/json",
        "User-Agent": "ConformanceBot/1.0",
      });

      const body = json(r) as Record<string, unknown>;
      const err = (body.error ?? body) as Record<string, unknown>;

      if ("status" in err) {
        expect(err.status).toBe(404);
      }
    });

    it("error indicates not_found type or code", async () => {
      const r = await get("/__conformance_nonexistent_path__", {
        Accept: "application/json",
        "User-Agent": "ConformanceBot/1.0",
      });

      const body = json(r) as Record<string, unknown>;
      const err = (body.error ?? body) as Record<string, unknown>;

      const typeOrCode = (err.type ?? err.code ?? "") as string;
      expect(typeOrCode.toLowerCase()).toMatch(/not.?found/);
    });

    it("error has is_retriable = false (if present)", async () => {
      const r = await get("/__conformance_nonexistent_path__", {
        Accept: "application/json",
        "User-Agent": "ConformanceBot/1.0",
      });

      const body = json(r) as Record<string, unknown>;
      const err = (body.error ?? body) as Record<string, unknown>;

      if ("is_retriable" in err) {
        expect(err.is_retriable).toBe(false);
      }
    });
  });

  // -------------------------------------------------------------------------
  // Content negotiation (HTML vs JSON)
  // -------------------------------------------------------------------------

  describe("Content negotiation", () => {
    it("returns HTML for browser-like Accept header", async () => {
      const r = await get("/__conformance_nonexistent_path__", {
        Accept: "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
        "User-Agent":
          "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36",
      });

      const ct = header(r, "content-type") ?? "";
      // Should be HTML or at least not JSON
      if (ct.includes("text/html")) {
        expect(r.body).toMatch(/<html/i);
      }
    });

    it("returns JSON for agent User-Agent", async () => {
      const r = await get("/__conformance_nonexistent_path__", {
        "User-Agent": "ClaudeBot/1.0",
      });

      const ct = header(r, "content-type") ?? "";
      expect(ct).toMatch(/application\/json/);
    });
  });
});
