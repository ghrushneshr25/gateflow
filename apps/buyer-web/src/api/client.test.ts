import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  api,
  clearBuyerSession,
  clearIdempotencyKey,
  clearToken,
  getOrCreateIdempotencyKey,
  getOrderHint,
  getToken,
  HttpError,
  setOrderHint,
  setToken,
  tokenScope,
} from "./client";

/** Minimal fake JWT payload with sub (header.payload.sig). */
function fakeJwt(sub: string): string {
  const payload = btoa(JSON.stringify({ sub, principal_type: "buyer" }))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
  return `hdr.${payload}.sig`;
}

describe("buyer api client", () => {
  beforeEach(() => {
    clearToken();
    sessionStorage.clear();
    vi.stubGlobal("fetch", vi.fn());
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    clearToken();
    sessionStorage.clear();
  });

  it("sends Bearer and never X-Buyer-ID", async () => {
    setToken("buyer-tok");
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ items: [] }), { status: 200 }));
    await api.listOrders();
    const init = fetchMock.mock.calls[0]![1] as RequestInit;
    const h = init.headers as Record<string, string>;
    expect(h.Authorization).toBe("Bearer buyer-tok");
    expect(h["X-Buyer-ID"]).toBeUndefined();
  });

  it("scopes idempotency keys by buyer identity", () => {
    setToken(fakeJwt("buyer_a"));
    const a1 = getOrCreateIdempotencyKey("ses_1", "tt_1");
    const a2 = getOrCreateIdempotencyKey("ses_1", "tt_1");
    expect(a1).toBe(a2);
    expect(tokenScope()).toContain("buyer_a");

    clearBuyerSession();
    setToken(fakeJwt("buyer_b"));
    const b1 = getOrCreateIdempotencyKey("ses_1", "tt_1");
    expect(b1).not.toBe(a1);
  });

  it("clears keys on clearBuyerSession", () => {
    setToken(fakeJwt("buyer_a"));
    getOrCreateIdempotencyKey("ses_1", "tt_1");
    setOrderHint("ses_1", "tt_1", "ord_1");
    clearBuyerSession();
    expect(getToken()).toBe("");
    setToken(fakeJwt("buyer_a"));
    // new key after clear
    const k = getOrCreateIdempotencyKey("ses_1", "tt_1");
    expect(k).toBeTruthy();
    expect(getOrderHint("ses_1", "tt_1")).toBeNull();
    clearIdempotencyKey("ses_1", "tt_1");
  });

  it("maps 401 to HttpError", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: { message: "nope", code: "ORD_UNAUTHORIZED" } }), {
        status: 401,
      }),
    );
    await expect(api.listOrders()).rejects.toMatchObject({
      status: 401,
      code: "ORD_UNAUTHORIZED",
    } satisfies Partial<HttpError>);
  });
});
