const SALE = import.meta.env.VITE_SALE_API_BASE ?? "http://127.0.0.1:8081";
const ADMISSION = import.meta.env.VITE_ADMISSION_API_BASE ?? "http://127.0.0.1:8082";
const ORDER = import.meta.env.VITE_ORDER_API_BASE ?? "http://127.0.0.1:8084";

async function json<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers ?? {}),
    },
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || res.statusText);
  }
  return res.json() as Promise<T>;
}

export type Offering = {
  id: string;
  name: string;
  description: string;
  category: string;
  status: string;
};

export type Order = {
  id: string;
  status: string;
  quantity: number;
  amount_cents: number;
  session_id: string;
};

export const api = {
  listOfferings: () =>
    json<{ items: Offering[] }>(`${SALE}/v1/offerings?buyer=1`),
  listOrders: (buyerId: string) =>
    json<{ items: Order[] }>(`${ORDER}/v1/orders`, {
      headers: { "X-Buyer-ID": buyerId },
    }),
  joinAndGrant: async (sessionId: string, buyerId: string) => {
    await json(`${ADMISSION}/v1/sessions/${sessionId}/queue/joins`, {
      method: "POST",
      headers: { "X-Buyer-ID": buyerId },
      body: "{}",
    });
    return json<{ entitlement: { id: string } }>(
      `${ADMISSION}/v1/sessions/${sessionId}/grants`,
      {
        method: "POST",
        body: JSON.stringify({ buyer_id: buyerId }),
      },
    );
  },
};
