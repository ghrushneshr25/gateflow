import type {
  ApiError,
  Entitlement,
  Offering,
  Order,
  QueueEntry,
  Session,
  TicketType,
} from "./types";

const SALE = import.meta.env.VITE_SALE_API_BASE ?? "http://127.0.0.1:8081";
const ADMISSION = import.meta.env.VITE_ADMISSION_API_BASE ?? "http://127.0.0.1:8082";
const ORDER = import.meta.env.VITE_ORDER_API_BASE ?? "http://127.0.0.1:8084";

const TOKEN_KEY = "gateflow_buyer_jwt";
const IDEM_PREFIX = "gateflow_checkout_idem:";
const ORDER_HINT_PREFIX = "gateflow_order_hint:";

/** Short fingerprint of token for storage scoping (not a security boundary). */
export function tokenScope(): string {
  const t = getToken();
  if (!t) return "anon";
  // Prefer JWT payload sub if parseable; else hash-ish prefix.
  try {
    const parts = t.split(".");
    const payload = parts[1];
    if (payload) {
      const json = JSON.parse(atob(payload.replace(/-/g, "+").replace(/_/g, "/")));
      if (json && typeof json.sub === "string" && json.sub) return `sub:${json.sub}`;
    }
  } catch {
    /* ignore */
  }
  return `tok:${t.slice(0, 16)}`;
}

export function getToken(): string {
  return sessionStorage.getItem(TOKEN_KEY) ?? "";
}

export function setToken(token: string): void {
  const t = token.trim();
  if (t) sessionStorage.setItem(TOKEN_KEY, t);
  else sessionStorage.removeItem(TOKEN_KEY);
}

export function clearToken(): void {
  sessionStorage.removeItem(TOKEN_KEY);
}

/** Clear token and all checkout idempotency/order hints (call on sign-out / identity switch). */
export function clearBuyerSession(): void {
  const drop: string[] = [];
  for (let i = 0; i < sessionStorage.length; i++) {
    const k = sessionStorage.key(i);
    if (!k) continue;
    if (k.startsWith(IDEM_PREFIX) || k.startsWith(ORDER_HINT_PREFIX)) drop.push(k);
  }
  for (const k of drop) sessionStorage.removeItem(k);
  clearToken();
}

function idemStorageKey(sessionId: string, ticketTypeId: string): string {
  return `${IDEM_PREFIX}${tokenScope()}:${sessionId}:${ticketTypeId}`;
}

function orderHintKey(sessionId: string, ticketTypeId: string): string {
  return `${ORDER_HINT_PREFIX}${tokenScope()}:${sessionId}:${ticketTypeId}`;
}

/**
 * Stable idempotency key for one logical checkout attempt (buyer+session+ticket).
 * Cleared only on CONFIRMED (or explicit clear). Never rotate on timeout.
 */
export function getOrCreateIdempotencyKey(sessionId: string, ticketTypeId: string): string {
  const k = idemStorageKey(sessionId, ticketTypeId);
  let v = sessionStorage.getItem(k);
  if (!v) {
    v =
      typeof crypto !== "undefined" && "randomUUID" in crypto
        ? crypto.randomUUID()
        : `idem-${Date.now()}-${Math.random().toString(36).slice(2)}`;
    sessionStorage.setItem(k, v);
  }
  return v;
}

export function clearIdempotencyKey(sessionId: string, ticketTypeId: string): void {
  sessionStorage.removeItem(idemStorageKey(sessionId, ticketTypeId));
  sessionStorage.removeItem(orderHintKey(sessionId, ticketTypeId));
}

/** Remember last known order id for uncertain checkout recovery (same buyer scope). */
export function setOrderHint(sessionId: string, ticketTypeId: string, orderId: string): void {
  sessionStorage.setItem(orderHintKey(sessionId, ticketTypeId), orderId);
}

export function getOrderHint(sessionId: string, ticketTypeId: string): string | null {
  return sessionStorage.getItem(orderHintKey(sessionId, ticketTypeId));
}

export class HttpError extends Error {
  status: number;
  code?: string;
  constructor(err: ApiError) {
    super(err.message);
    this.name = "HttpError";
    this.status = err.status;
    this.code = err.code;
  }
}

async function request<T>(base: string, path: string, init?: RequestInit & { token?: string }): Promise<T> {
  const token = init?.token ?? getToken();
  const headers: Record<string, string> = {
    Accept: "application/json",
    ...(init?.headers as Record<string, string> | undefined),
  };
  if (init?.body && !headers["Content-Type"]) {
    headers["Content-Type"] = "application/json";
  }
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  // Never send X-Buyer-ID — identity is JWT only (P5.1.1 / P5.3.0).
  let res: Response;
  try {
    res = await fetch(`${base}${path}`, { ...init, headers });
  } catch (e) {
    throw new HttpError({
      status: 0,
      message: e instanceof Error ? e.message : "network error",
    });
  }
  const text = await res.text();
  let body: unknown = null;
  if (text) {
    try {
      body = JSON.parse(text);
    } catch {
      body = text;
    }
  }
  if (!res.ok) {
    const errObj =
      body && typeof body === "object" && body !== null && "error" in body
        ? (body as { error?: { message?: string; code?: string } }).error
        : undefined;
    throw new HttpError({
      status: res.status,
      message: String(errObj?.message || (typeof body === "string" && body) || res.statusText || `HTTP ${res.status}`),
      code: errObj?.code,
    });
  }
  return (body ?? {}) as T;
}

export const api = {
  listOfferings: () =>
    request<{ items: Offering[] }>(SALE, "/v1/offerings?status=PUBLISHED"),
  getOffering: (id: string) => request<Offering>(SALE, `/v1/offerings/${encodeURIComponent(id)}`),
  listSessions: (offeringId: string) =>
    request<{ items: Session[] }>(SALE, `/v1/offerings/${encodeURIComponent(offeringId)}/sessions`),
  listTicketTypes: (sessionId: string) =>
    request<{ items: TicketType[] }>(
      SALE,
      `/v1/sessions/${encodeURIComponent(sessionId)}/ticket-types`,
    ),
  joinQueue: (sessionId: string) =>
    request<QueueEntry>(ADMISSION, `/v1/sessions/${encodeURIComponent(sessionId)}/queue/joins`, {
      method: "POST",
      body: "{}",
    }),
  queueMe: (sessionId: string) =>
    request<QueueEntry>(ADMISSION, `/v1/sessions/${encodeURIComponent(sessionId)}/queue/me`),
  /** Buyer self-grant (JWT sub) or service-scoped grant. */
  requestGrant: (sessionId: string) =>
    request<{ queue_entry: QueueEntry; entitlement: Entitlement }>(
      ADMISSION,
      `/v1/sessions/${encodeURIComponent(sessionId)}/grants`,
      { method: "POST", body: "{}" },
    ),
  getEntitlement: (id: string) =>
    request<Entitlement>(ADMISSION, `/v1/entitlements/${encodeURIComponent(id)}`),
  checkout: (body: {
    session_id: string;
    ticket_type_id: string;
    pool_id: string;
    entitlement_id: string;
    quantity: number;
    amount_cents: number;
    idempotency_key: string;
  }) =>
    request<Order>(ORDER, "/v1/checkouts", {
      method: "POST",
      body: JSON.stringify(body),
      headers: { "Idempotency-Key": body.idempotency_key },
    }),
  listOrders: () => request<{ items: Order[] }>(ORDER, "/v1/orders"),
  getOrder: (id: string) => request<Order>(ORDER, `/v1/orders/${encodeURIComponent(id)}`),
  reconcileOrder: (id: string) =>
    request<{ order: Order; result: string }>(ORDER, `/v1/orders/${encodeURIComponent(id)}/reconcile`, {
      method: "POST",
      body: "{}",
    }),
};
