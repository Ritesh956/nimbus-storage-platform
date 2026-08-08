// The access token lives in memory only — not localStorage — so it can't be
// read back out of storage after the fact by an injected script; it's lost
// on reload and rehydrated via POST /api/auth/refresh (lib/api.ts), which
// reads the httpOnly refresh cookie server-side. The refresh token itself
// never reaches this module at all: app/api/auth/*'s route handlers set and
// read it as an httpOnly, SameSite=Strict cookie the Next.js server proxies
// through to the Go API — a browser-side XSS payload can no longer walk off
// with a long-lived credential the way a localStorage-held refresh token
// could (roadmap #1 / audit §04's single highest-leverage security fix).
let accessToken: string | null = null;

export function getAccessToken(): string | null {
  return accessToken;
}

export function setAccessToken(token: string | null) {
  accessToken = token;
}
