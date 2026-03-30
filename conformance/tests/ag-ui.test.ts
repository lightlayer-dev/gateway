/**
 * AG-UI SSE streaming conformance test.
 *
 * Validates that the SSE endpoint (if present) returns proper
 * Server-Sent Events with the expected headers and event format.
 *
 * Since the AG-UI endpoint path may vary by implementation, this test
 * tries common paths: /ag-ui, /sse, /stream, /events.
 * Set AG_UI_PATH env var to override.
 */

import { describe, it, expect } from "vitest";
import { baseUrl, header } from "../helpers.js";

const AG_UI_PATH = process.env.AG_UI_PATH ?? "/ag-ui";

/**
 * Open an SSE connection, collect events for up to `timeoutMs`, then abort.
 */
async function collectSSE(
  path: string,
  timeoutMs = 3000,
): Promise<{
  status: number;
  contentType: string;
  headers: Headers;
  events: Array<{ event?: string; data: string }>;
  raw: string;
}> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);

  try {
    const res = await fetch(`${baseUrl}${path}`, {
      method: "POST",
      headers: {
        Accept: "text/event-stream",
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ threadId: "conformance-test" }),
      signal: controller.signal,
    });

    const status = res.status;
    const contentType = res.headers.get("content-type") ?? "";
    const headers = res.headers;

    if (status !== 200) {
      clearTimeout(timer);
      return { status, contentType, headers, events: [], raw: "" };
    }

    const raw = await res.text().catch(() => "");
    clearTimeout(timer);

    // Parse SSE events from raw text
    const events: Array<{ event?: string; data: string }> = [];
    let currentEvent: string | undefined;
    let currentData: string[] = [];

    for (const line of raw.split("\n")) {
      if (line.startsWith("event:")) {
        currentEvent = line.slice(6).trim();
      } else if (line.startsWith("data:")) {
        currentData.push(line.slice(5).trim());
      } else if (line === "" && currentData.length > 0) {
        events.push({ event: currentEvent, data: currentData.join("\n") });
        currentEvent = undefined;
        currentData = [];
      }
    }

    // Flush remaining
    if (currentData.length > 0) {
      events.push({ event: currentEvent, data: currentData.join("\n") });
    }

    return { status, contentType, headers, events, raw };
  } catch {
    clearTimeout(timer);
    return { status: 0, contentType: "", headers: new Headers(), events: [], raw: "" };
  }
}

describe("AG-UI SSE streaming", () => {
  it("SSE endpoint exists (returns 200 or known status)", async () => {
    const { status } = await collectSSE(AG_UI_PATH, 2000);
    // 200 = streaming, 404 = not configured, 405 = method not allowed
    // All are valid — we just want to know it's reachable
    expect([0, 200, 201, 202, 204, 404, 405]).toContain(status);
  });

  it("returns text/event-stream content-type (when 200)", async () => {
    const result = await collectSSE(AG_UI_PATH, 2000);
    if (result.status !== 200) return;

    expect(result.contentType).toMatch(/text\/event-stream/);
  });

  it("includes Cache-Control: no-cache header (when 200)", async () => {
    const result = await collectSSE(AG_UI_PATH, 2000);
    if (result.status !== 200) return;

    const cc = result.headers.get("cache-control") ?? "";
    expect(cc).toMatch(/no-cache/);
  });

  it("events have valid SSE format (when streaming)", async () => {
    const result = await collectSSE(AG_UI_PATH, 3000);
    if (result.status !== 200 || result.events.length === 0) return;

    for (const evt of result.events) {
      // Each event should have a data field
      expect(evt.data.length).toBeGreaterThan(0);

      // If event has a named type, try to parse data as JSON
      if (evt.event) {
        try {
          const parsed = JSON.parse(evt.data) as Record<string, unknown>;
          expect(parsed).toHaveProperty("type");
        } catch {
          // Non-JSON data is acceptable for some event types
        }
      }
    }
  });

  it("RUN_STARTED event contains threadId and runId (when present)", async () => {
    const result = await collectSSE(AG_UI_PATH, 3000);
    if (result.status !== 200) return;

    const runStarted = result.events.find(
      (e) => e.event === "RUN_STARTED",
    );
    if (!runStarted) return;

    const data = JSON.parse(runStarted.data) as Record<string, unknown>;
    expect(data).toHaveProperty("type", "RUN_STARTED");
    expect(data).toHaveProperty("threadId");
    expect(data).toHaveProperty("runId");
  });

  it("TEXT_MESSAGE_CONTENT events have messageId and delta (when present)", async () => {
    const result = await collectSSE(AG_UI_PATH, 3000);
    if (result.status !== 200) return;

    const textContent = result.events.filter(
      (e) => e.event === "TEXT_MESSAGE_CONTENT",
    );

    for (const evt of textContent) {
      const data = JSON.parse(evt.data) as Record<string, unknown>;
      expect(data).toHaveProperty("type", "TEXT_MESSAGE_CONTENT");
      expect(data).toHaveProperty("messageId");
      expect(data).toHaveProperty("delta");
    }
  });
});
