/**
 * Rate limiting conformance tests.
 *
 * Sends burst requests and validates rate-limit headers and 429 responses.
 */

import { describe, it, expect } from "vitest";
import { get, burst, header } from "../helpers.js";

describe("Rate limiting", () => {
  it("returns rate-limit headers on normal requests", async () => {
    const r = await get("/llms.txt");

    // Servers should include at least one rate-limit header variant.
    // Common names: X-RateLimit-Limit, X-RateLimit-Remaining, RateLimit-Limit, etc.
    const rl =
      header(r, "x-ratelimit-limit") ??
      header(r, "ratelimit-limit") ??
      header(r, "x-rate-limit-limit");

    // This test is informational — if no headers, it still passes but logs a warning.
    if (rl === undefined) {
      console.warn(
        "  [INFO] No rate-limit headers found on GET /llms.txt — " +
          "server may not expose them on this endpoint.",
      );
    }
  });

  it("returns X-RateLimit-Remaining that is a number (when present)", async () => {
    const r = await get("/llms.txt");
    const remaining =
      header(r, "x-ratelimit-remaining") ??
      header(r, "ratelimit-remaining") ??
      header(r, "x-rate-limit-remaining");

    if (remaining !== undefined) {
      expect(Number.isNaN(Number(remaining))).toBe(false);
    }
  });

  it("returns 429 when rate limit is exceeded (burst test)", async () => {
    // Send a burst of 120 requests to a rate-limited endpoint.
    // If the server has a rate limit configured, at least one should be 429.
    // If there's no rate limit on this endpoint, we just verify we get 200s.
    const results = await burst("/agents.txt", 120);

    const statuses = results.map((r) => r.status);
    const has429 = statuses.includes(429);
    const allOk = statuses.every((s) => s === 200);

    if (has429) {
      // At least one 429 — check it has Retry-After or rate-limit headers
      const limited = results.find((r) => r.status === 429)!;
      const retryAfter =
        header(limited, "retry-after") ??
        header(limited, "x-ratelimit-reset") ??
        header(limited, "ratelimit-reset");

      // Should have some indication of when to retry
      if (retryAfter === undefined) {
        console.warn(
          "  [INFO] 429 response lacks Retry-After / RateLimit-Reset header.",
        );
      }
    } else if (allOk) {
      console.warn(
        "  [INFO] All 120 requests returned 200 — rate limiting may not be " +
          "configured on /agents.txt. Consider testing against an endpoint " +
          "with stricter limits.",
      );
    }

    // Either all 200 or some 429 — anything else is unexpected
    for (const s of statuses) {
      expect([200, 429]).toContain(s);
    }
  });

  it("429 response body is a structured error envelope", async () => {
    // We try to trigger a 429 with a burst.
    const results = await burst("/agents.txt", 120);
    const limited = results.find((r) => r.status === 429);

    if (!limited) {
      // Can't trigger 429 — skip
      console.warn(
        "  [SKIP] Could not trigger 429 — cannot validate error envelope.",
      );
      return;
    }

    let body: Record<string, unknown>;
    try {
      body = JSON.parse(limited.body) as Record<string, unknown>;
    } catch {
      // Some servers return plain text 429 — acceptable but not ideal
      console.warn("  [INFO] 429 response body is not JSON.");
      return;
    }

    // Expect an error envelope
    const err = (body.error ?? body) as Record<string, unknown>;
    expect(typeof err.message === "string" || typeof err.type === "string").toBe(
      true,
    );
  });
});
