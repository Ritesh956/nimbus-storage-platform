import { cookies } from "next/headers";
import { proxyJSON, REFRESH_COOKIE, REFRESH_COOKIE_MAX_AGE, refreshCookieOptions } from "@/lib/server/auth-proxy";

// Proxies POST /v1/auth/login: the refresh token never reaches the browser
// as JS-readable JSON — it's set here as an httpOnly cookie, and only the
// short-lived access token goes back in the response body for the client to
// hold in memory (lib/tokens.ts).
export async function POST(request: Request) {
  const { status, data } = await proxyJSON("/v1/auth/login", await request.json());
  if (status >= 400) return Response.json(data, { status });

  if (data.totp_required) {
    // No tokens minted yet — nothing to cookie until totpLogin finishes it.
    return Response.json(data);
  }

  const cookieStore = await cookies();
  cookieStore.set(REFRESH_COOKIE, data.refresh_token as string, {
    ...refreshCookieOptions,
    maxAge: REFRESH_COOKIE_MAX_AGE,
  });
  return Response.json({ access_token: data.access_token, expires_in: data.expires_in });
}
