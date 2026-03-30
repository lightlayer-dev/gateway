/**
 * Shared helpers for the LightLayer agent-layer conformance test suite.
 *
 * Every test file imports `baseUrl` and the tiny fetch wrappers from here.
 * Set the BASE_URL env var to point at the server under test.
 */

// ---------------------------------------------------------------------------
// Base URL
// ---------------------------------------------------------------------------

export const baseUrl = (
  process.env.BASE_URL ?? "http://localhost:8080"
).replace(/\/+$/, "");

// ---------------------------------------------------------------------------
// Fetch helpers
// ---------------------------------------------------------------------------

export interface FetchResult {
  status: number;
  headers: Headers;
  body: string;
}

/** Plain GET with optional extra headers. */
export async function get(
  path: string,
  headers: Record<string, string> = {},
): Promise<FetchResult> {
  const res = await fetch(`${baseUrl}${path}`, { headers });
  return {
    status: res.status,
    headers: res.headers,
    body: await res.text(),
  };
}

/** POST with a JSON body. */
export async function postJson(
  path: string,
  body: unknown,
  headers: Record<string, string> = {},
): Promise<FetchResult> {
  const res = await fetch(`${baseUrl}${path}`, {
    method: "POST",
    headers: { "Content-Type": "application/json", ...headers },
    body: JSON.stringify(body),
  });
  return {
    status: res.status,
    headers: res.headers,
    body: await res.text(),
  };
}

/** Send N concurrent GETs and return all results. */
export async function burst(
  path: string,
  count: number,
  headers: Record<string, string> = {},
): Promise<FetchResult[]> {
  return Promise.all(
    Array.from({ length: count }, () => get(path, headers)),
  );
}

/** Parse body as JSON – throws a helpful message on failure. */
export function json(r: FetchResult): unknown {
  try {
    return JSON.parse(r.body);
  } catch {
    throw new Error(
      `Expected JSON but got (status ${r.status}):\n${r.body.slice(0, 500)}`,
    );
  }
}

/** Header value (case-insensitive) or undefined. */
export function header(r: FetchResult, name: string): string | undefined {
  return r.headers.get(name) ?? undefined;
}
