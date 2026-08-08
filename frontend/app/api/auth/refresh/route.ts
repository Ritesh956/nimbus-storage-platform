import { cookies } from "next/headers";
import { proxyJSON, REFRESH_COOKIE, REFRESH_COOKIE_MAX_AGE, refreshCookieOptions } from "@/lib/server/auth-proxy";

// Reads the httpOnly refresh cookie server-side, rotates it against the Go
// API, and hands the client back a fresh access token — this is also how
// AuthProvider (lib/auth-context.tsx) rehydrates "was I logged in?" on a
// cold page load, since the refresh token itself is never JS-readable.
export async function POST() {
  const cookieStore = await cookies();
  const refreshToken = cookieStore.get(REFRESH_COOKIE)?.value;
  if (!refreshToken) {
    return Response.json({ error: { code: "unauthorized", message: "not logged in" } }, { status: 401 });
  }

  const { status, data } = await proxyJSON("/v1/auth/refresh", { refresh_token: refreshToken });
  if (status >= 400) {
    cookieStore.delete(REFRESH_COOKIE);
    return Response.json(data, { status });
  }

  cookieStore.set(REFRESH_COOKIE, data.refresh_token as string, {
    ...refreshCookieOptions,
    maxAge: REFRESH_COOKIE_MAX_AGE,
  });
  return Response.json({ access_token: data.access_token, expires_in: data.expires_in });
}
