import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api, clearToken, HttpError, setToken } from "./client";

describe("api client", () => {
  beforeEach(() => {
    clearToken();
    vi.stubGlobal("fetch", vi.fn());
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    clearToken();
  });

  it("sends Authorization bearer when token set", async () => {
    setToken("tok-abc");
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValue(
      new Response(JSON.stringify({ reconciliation_required: [], payment_pending: [] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    await api.getReconciliation();
    expect(fetchMock).toHaveBeenCalled();
    const call = fetchMock.mock.calls[0];
    expect(call).toBeDefined();
    const init = call![1] as RequestInit;
    const headers = init.headers as Record<string, string>;
    expect(headers.Authorization).toBe("Bearer tok-abc");
  });

  it("maps 401 and 403 to HttpError", async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "ORD_UNAUTHORIZED", message: "nope" } }), {
        status: 401,
      }),
    );
    await expect(api.getReconciliation()).rejects.toMatchObject({
      status: 401,
      message: "nope",
      code: "ORD_UNAUTHORIZED",
    } satisfies Partial<HttpError>);

    fetchMock.mockResolvedValue(
      new Response(JSON.stringify({ error: { message: "forbidden" } }), { status: 403 }),
    );
    await expect(api.listQuarantined()).rejects.toMatchObject({ status: 403 });
  });

  it("treats network failure as status 0", async () => {
    vi.mocked(fetch).mockRejectedValue(new Error("failed to fetch"));
    await expect(api.getCoordination()).rejects.toMatchObject({ status: 0 });
  });
});
