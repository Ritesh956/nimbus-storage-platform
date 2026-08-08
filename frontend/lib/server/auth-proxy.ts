// Server-only helpers for app/api/auth/*'s route handlers — the thin
// Next.js BFF proxy that keeps the refresh token out of JS-reachable
// storage (roadmap #1 / audit §04's single highest-leverage security fix).
// Never import this from client ("use client") code.

// NEXT_PUBLIC_API_URL is baked into the client bundle at build time and
// points at whatever host the *browser* can reach (docker-compose/k8s both
// publish nimbus-api on localhost:8080 for exactly that reason — see
// deploy/Dockerfile.web). The Next.js server process runs somewhere else
// entirely when containerized (the nimbus-web container can't resolve
// "localhost" to nimbus-api the way the browser's host machine can — same
// class of footgun as the presigned-MinIO-URL one in CLAUDE.md), so route
// handlers need their own, container-network-aware value. Compose/Helm set
// NIMBUS_API_INTERNAL_URL to "http://nimbus-api:8080"; falling back to
// NEXT_PUBLIC_API_URL covers plain `npm run dev` on the host, where both
// values are the same.
export const API_INTERNAL_URL =
  process.env.NIMBUS_API_INTERNAL_URL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export const REFRESH_COOKIE = "nimbus_refresh_token";

// Matches the backend's default NIMBUS_REFRESH_TOKEN_TTL (config.go) — a
// static duplicate rather than something fetched at runtime, since a
// mismatch fails safe either direction: too short just means an early
// re-login, too long just means the cookie outlives a token the backend has
// already stopped honoring (RotateRefreshToken re-checks expiry itself).
export const REFRESH_COOKIE_MAX_AGE = 60 * 60 * 24 * 7;

export const refreshCookieOptions = {
  httpOnly: true,
  sameSite: "strict" as const,
  secure: process.env.NODE_ENV === "production",
  path: "/",
};

// The Go API's auth responses are heterogeneous by design (token pair vs.
// TOTP challenge vs. {error}) — callers narrow this themselves per field,
// same as request()'s own body: unknown in lib/api.ts.
type ProxyResponse = Record<string, unknown>;

export async function proxyJSON(path: string, body: unknown): Promise<{ status: number; data: ProxyResponse }> {
  const res = await fetch(`${API_INTERNAL_URL}${path}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const data = await res.json().catch(() => ({}));
  return { status: res.status, data };
}
