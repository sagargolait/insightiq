import { afterEach, describe, expect, it, vi } from "vitest";
import { api, ApiError } from "./api";

function mockFetch(status: number, body?: unknown) {
  const fn = vi.fn(async () =>
    body === undefined
      ? new Response(null, { status })
      : new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
  );
  vi.stubGlobal("fetch", fn);
  return fn;
}

afterEach(() => vi.unstubAllGlobals());

describe("api client", () => {
  it("sends JSON to the same-origin /api/v1 prefix", async () => {
    const fetch = mockFetch(200, { id: "u1" });
    await api.auth.login({ email: "a@b.co", password: "pw" });

    const [url, init] = fetch.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("/api/v1/auth/login");
    expect(init.method).toBe("POST");
    expect(init.credentials).toBe("same-origin");
    expect(init.headers).toMatchObject({ "Content-Type": "application/json" });
    expect(JSON.parse(init.body as string)).toEqual({ email: "a@b.co", password: "pw" });
  });

  it("omits Content-Type on bodyless requests", async () => {
    const fetch = mockFetch(200, []);
    await api.orgs.list();
    const [, init] = fetch.mock.calls[0] as unknown as [string, RequestInit];
    expect(init.headers).not.toHaveProperty("Content-Type");
  });

  it("returns undefined for 204", async () => {
    mockFetch(204);
    await expect(api.orgs.removeMember("o1", "u1")).resolves.toBeUndefined();
  });

  it("maps the Go error envelope to ApiError", async () => {
    mockFetch(404, { error: { code: "ORGANIZATION_NOT_FOUND", message: "Organization not found" } });
    const err = await api.orgs.get("o1").catch((e) => e);

    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 404, code: "ORGANIZATION_NOT_FOUND", message: "Organization not found" });
  });

  it("falls back when the error body is not JSON", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("bad gateway", { status: 502 })));
    const err = await api.auth.me().catch((e) => e);
    expect(err).toMatchObject({ status: 502, code: "UNKNOWN_ERROR" });
  });

  it("exposes per-field errors", () => {
    const err = new ApiError(422, "VALIDATION_ERROR", "bad", [{ field: "email", message: "invalid" }]);
    expect(err.fieldError("email")).toBe("invalid");
    expect(err.fieldError("name")).toBeUndefined();
  });
});
