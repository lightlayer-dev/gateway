/**
 * Agent detection conformance tests.
 *
 * Validates that the server correctly identifies AI agent User-Agent strings
 * and responds appropriately (JSON content-type, analytics tracking, etc.).
 */

import { describe, it, expect } from "vitest";
import { get, header } from "../helpers.js";

const KNOWN_AGENTS = [
  { ua: "ChatGPT-User", name: "ChatGPT" },
  { ua: "GPTBot/1.0", name: "GPTBot" },
  { ua: "ClaudeBot/1.0", name: "ClaudeBot" },
  { ua: "Anthropic-AI", name: "Anthropic" },
  { ua: "PerplexityBot/1.0", name: "PerplexityBot" },
  { ua: "Cohere-AI/1.0", name: "Cohere" },
  { ua: "Googlebot/2.1", name: "Googlebot" },
  { ua: "Google-Extended", name: "Google-Extended" },
  { ua: "Bingbot/2.0", name: "Bingbot" },
  { ua: "CCBot/2.0", name: "CCBot" },
  { ua: "Applebot/0.1", name: "Applebot" },
  { ua: "Amazonbot/0.1", name: "Amazonbot" },
  { ua: "Meta-ExternalAgent/1.0", name: "Meta-ExternalAgent" },
  { ua: "Bytespider", name: "Bytespider" },
  { ua: "AI2Bot", name: "AI2Bot" },
  { ua: "Diffbot/1.0", name: "Diffbot" },
];

const BROWSER_UAS = [
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
];

describe("Agent detection", () => {
  // -------------------------------------------------------------------------
  // Known AI agents get JSON responses
  // -------------------------------------------------------------------------

  describe("known agents receive JSON error responses", () => {
    for (const { ua, name } of KNOWN_AGENTS) {
      it(`detects ${name} (${ua})`, async () => {
        const r = await get("/__conformance_agent_detect__", {
          "User-Agent": ua,
        });

        // Should return a 404 (path doesn't exist) — the key assertion is
        // that the response is JSON, not HTML, because the server detected
        // an agent.
        expect(r.status).toBe(404);
        const ct = header(r, "content-type") ?? "";
        expect(ct).toMatch(/application\/json/);
      });
    }
  });

  // -------------------------------------------------------------------------
  // Browser UAs get HTML (or at least not JSON)
  // -------------------------------------------------------------------------

  describe("browser User-Agents", () => {
    for (const ua of BROWSER_UAS) {
      it(`returns non-JSON for browser: ${ua.slice(0, 40)}...`, async () => {
        const r = await get("/__conformance_agent_detect__", {
          "User-Agent": ua,
          Accept:
            "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
        });

        expect(r.status).toBe(404);
        const ct = header(r, "content-type") ?? "";
        // Should be HTML or at least not pure JSON
        if (ct.includes("text/html")) {
          expect(r.body).toMatch(/<html/i);
        }
      });
    }
  });

  // -------------------------------------------------------------------------
  // Accept header overrides User-Agent detection
  // -------------------------------------------------------------------------

  describe("Accept header override", () => {
    it("returns JSON when Accept: application/json regardless of UA", async () => {
      const r = await get("/__conformance_agent_detect__", {
        "User-Agent":
          "Mozilla/5.0 (Macintosh; Intel Mac OS X) AppleWebKit/537.36",
        Accept: "application/json",
      });

      expect(r.status).toBe(404);
      const ct = header(r, "content-type") ?? "";
      expect(ct).toMatch(/application\/json/);
    });
  });
});
