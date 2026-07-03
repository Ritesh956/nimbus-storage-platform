// Tokens are kept in localStorage for simplicity — a known XSS-exposure
// tradeoff vs. httpOnly cookies. A cookie-based flow would need this
// Next.js app to proxy auth through its own route handlers (BFF pattern),
// which is real added complexity; documented here as a deliberate
// simplification for a demo-scale app, not an oversight.
const ACCESS_KEY = "nimbus_access_token";
const REFRESH_KEY = "nimbus_refresh_token";

export function getAccessToken(): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem(ACCESS_KEY);
}

export function getRefreshToken(): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem(REFRESH_KEY);
}

export function setTokens(access: string, refresh: string) {
  window.localStorage.setItem(ACCESS_KEY, access);
  window.localStorage.setItem(REFRESH_KEY, refresh);
}

export function clearTokens() {
  window.localStorage.removeItem(ACCESS_KEY);
  window.localStorage.removeItem(REFRESH_KEY);
}
