import { cookies } from "next/headers";
import { proxyJSON, REFRESH_COOKIE, REFRESH_COOKIE_MAX_AGE, refreshCookieOptions } from "@/lib/server/auth-proxy";

// Step two of a TOTP-gated login (see app/api/auth/login) — same
// cookie-the-refresh-token/return-only-the-access-token shape.
export async function POST(request: Request) {
  const { status, data } = await proxyJSON("/v1/auth/login/totp", await request.json());
  if (status >= 400) return Response.json(data, { status });

  const cookieStore = await cookies();
  cookieStore.set(REFRESH_COOKIE, data.refresh_token as string, {
    ...refreshCookieOptions,
    maxAge: REFRESH_COOKIE_MAX_AGE,
  });
  return Response.json({ access_token: data.access_token, expires_in: data.expires_in });
}
