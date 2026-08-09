import { describe, it, expect, vi, beforeEach } from "vitest";

// Audit §10's own named highest-value gap: lib/api.ts's request/
// refresh-race/error-mapping logic had no direct tests — everything about
// it was inferred from lib/upload.ts's tests exercising it indirectly.
// fetch is stubbed globally; api.ts's other collaborator (lib/tokens.ts,
// an in-memory module variable) is used for real rather than mocked, since
// it's simple enough that mocking it would just be re-describing it.

const fetchMock = vi.fn();
vi.stubGlobal("fetch", fetchMock);

import { api, ApiError } from "./api";
import { getAccessToken, setAccessToken } from "./tokens";

function jsonResponse(status: number, body: unknown): Response {
  return {
    status,
    ok: status >= 200 && status < 300,
    text: vi.fn(async () => JSON.stringify(body)),
  } as unknown as Response;
}

beforeEach(() => {
  fetchMock.mockReset();
  setAccessToken(null);
});

describe("request()", () => {
  it("attaches the Authorization header and returns the parsed JSON body", async () => {
    setAccessToken("tok-123");
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { user_id: "u1", email: "a@b.com", is_platform_admin: false }));

    const result = await api.auth.me();

    expect(result).toEqual({ user_id: "u1", email: "a@b.com", is_platform_admin: false });
    const [url, opts] = fetchMock.mock.calls[0];
    expect(url).toBe("http://localhost:8080/v1/auth/me");
    expect((opts.headers as Headers).get("Authorization")).toBe("Bearer tok-123");
  });

  it("omits the Authorization header when there's no access token", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(200, []));
    await api.orgs.listMine();
    const [, opts] = fetchMock.mock.calls[0];
    expect((opts.headers as Headers).has("Authorization")).toBe(false);
  });

  it("throws ApiError with the server's error code/message on failure", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(404, { error: { code: "not_found", message: "org not found" } }));

    await expect(api.orgs.listMine()).rejects.toMatchObject({
      status: 404,
      code: "not_found",
      message: "org not found",
    });
  });

  it("falls back to default error code/message when the body carries no error field", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(500, {}));

    await expect(api.orgs.listMine()).rejects.toMatchObject({
      status: 500,
      code: "internal",
      message: "request failed",
    });
  });

  it("returns undefined for a 204 response without attempting to parse a body", async () => {
    const res = jsonResponse(204, {});
    fetchMock.mockResolvedValueOnce(res);

    const result = await api.files.trash("f1");

    expect(result).toBeUndefined();
    expect(res.text).not.toHaveBeenCalled();
  });

  it("dedupes concurrent 401s into a single refresh call, not one per in-flight request", async () => {
    // Regression test for the race lib/api.ts's own comment calls out:
    // two requests hitting 401 at once must share one refresh(), not each
    // trigger their own rotation of the same refresh token (which would
    // revoke the whole family server-side on the loser's reuse).
    setAccessToken("expired");
    let refreshCalls = 0;
    fetchMock.mockImplementation(async (url: string) => {
      if (url === "/api/auth/refresh") {
        refreshCalls++;
        return jsonResponse(200, { access_token: "fresh-token" });
      }
      if (url.endsWith("/v1/orgs")) {
        return getAccessToken() === "expired"
          ? jsonResponse(401, {})
          : jsonResponse(200, [{ id: "o1", name: "Org", owner_user_id: "u1" }]);
      }
      throw new Error(`unexpected fetch to ${url}`);
    });

    const [a, b] = await Promise.all([api.orgs.listMine(), api.orgs.listMine()]);

    expect(a).toEqual([{ id: "o1", name: "Org", owner_user_id: "u1" }]);
    expect(b).toEqual(a);
    expect(refreshCalls).toBe(1);
    expect(getAccessToken()).toBe("fresh-token");
  });

  it("clears the access token and throws a clean 401 when refresh itself fails", async () => {
    setAccessToken("expired");
    fetchMock.mockImplementation(async (url: string) => {
      if (url === "/api/auth/refresh") {
        return jsonResponse(401, { error: { code: "unauthorized", message: "refresh token reused" } });
      }
      if (url.endsWith("/v1/orgs")) return jsonResponse(401, {});
      throw new Error(`unexpected fetch to ${url}`);
    });

    // request() replaces whatever error refresh() surfaced with a fixed,
    // generic 401 — the caller never sees "refresh token reused", just
    // "not logged in", regardless of why the refresh failed.
    await expect(api.orgs.listMine()).rejects.toMatchObject({
      status: 401,
      code: "unauthorized",
      message: "not logged in",
    });
    expect(getAccessToken()).toBeNull();
  });

  it("retries the original request exactly once after a successful refresh, not in a loop", async () => {
    setAccessToken("expired");
    let orgsCalls = 0;
    fetchMock.mockImplementation(async (url: string) => {
      if (url === "/api/auth/refresh") return jsonResponse(200, { access_token: "fresh-token" });
      if (url.endsWith("/v1/orgs")) {
        orgsCalls++;
        // Every call still carries the stale token in this test (the
        // fixture never rejects it a second time) — the assertion below is
        // what proves request() doesn't loop: exactly one retry happens
        // regardless of whether the retried call would itself 401 again.
        return jsonResponse(401, {});
      }
      throw new Error(`unexpected fetch to ${url}`);
    });

    // The retried call 401s too (fixture always does), so this ultimately
    // rejects — that's fine, the point is counting how many times /v1/orgs
    // was actually hit before request() gives up.
    await expect(api.orgs.listMine()).rejects.toBeInstanceOf(ApiError);

    // Once with the stale token, once retried after refresh (retry=false
    // on the second call is what stops it here) — never a third attempt.
    expect(orgsCalls).toBe(2);
  });
});

describe("ApiError", () => {
  it("carries status/code/message as real properties, not just the Error message", () => {
    const err = new ApiError(403, "forbidden", "not a member");
    expect(err).toBeInstanceOf(Error);
    expect(err.status).toBe(403);
    expect(err.code).toBe("forbidden");
    expect(err.message).toBe("not a member");
  });
});
