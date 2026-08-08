import { cookies } from "next/headers";
import { API_INTERNAL_URL, REFRESH_COOKIE } from "@/lib/server/auth-proxy";

// Revokes the refresh family (and, if the client sent its still-live access
// token, blacklists its jti — auth.Service.Logout) and always clears the
// cookie regardless of how the backend call goes, so the browser side of a
// session can't outlive a failed revoke.
export async function POST(request: Request) {
  const cookieStore = await cookies();
  const refreshToken = cookieStore.get(REFRESH_COOKIE)?.value ?? "";
  const authHeader = request.headers.get("authorization") ?? undefined;

  try {
    await fetch(`${API_INTERNAL_URL}/v1/auth/logout`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        ...(authHeader ? { Authorization: authHeader } : {}),
      },
      body: JSON.stringify({ refresh_token: refreshToken }),
    });
  } catch {
    // Best-effort — the cookie still gets cleared below either way.
  }

  cookieStore.delete(REFRESH_COOKIE);
  return new Response(null, { status: 204 });
}
