import type {
  ApiError,
  Budget,
  Coordination,
  Offering,
  QuarantinedList,
  ReconciliationList,
  ReconcileResponse,
  ReplayResponse,
  Session,
  TicketType,
} from "./types";

const ORDER = import.meta.env.VITE_ORDER_API_BASE ?? "http://127.0.0.1:8084";
const ADMISSION = import.meta.env.VITE_ADMISSION_API_BASE ?? "http://127.0.0.1:8082";
const SALE = import.meta.env.VITE_SALE_API_BASE ?? "http://127.0.0.1:8081";

const TOKEN_KEY = "gateflow_ops_jwt";

/** Session-scoped operator JWT. Not for production secret storage. */
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

async function request<T>(
  base: string,
  path: string,
  init?: RequestInit & { token?: string },
): Promise<T> {
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
    const message =
      errObj?.message ||
      (typeof body === "string" && body) ||
      res.statusText ||
      `HTTP ${res.status}`;
    throw new HttpError({
      status: res.status,
      message: String(message),
      code: errObj?.code,
    });
  }
  return (body ?? {}) as T;
}

export type OperatorLoginResponse = {
  access_token: string;
  token_type: string;
  expires_in: number;
  user: { id: string; username: string; display_name?: string; scopes: string[] };
};

export const api = {
  login: (username: string, password: string) =>
    request<OperatorLoginResponse>(SALE, "/v1/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
      token: "",
    }),
  createUser: (body: { username: string; password: string; display_name?: string; scopes?: string[] }, token?: string) =>
    request<{ id: string; username: string }>(SALE, "/v1/admin/users", {
      method: "POST",
      body: JSON.stringify(body),
      token,
    }),
  listUsers: (token?: string) =>
    request<{ items: Array<{ id: string; username: string; display_name?: string; active: boolean }> }>(
      SALE,
      "/v1/admin/users",
      { token },
    ),
  getReconciliation: (token?: string) =>
    request<ReconciliationList>(ORDER, "/v1/admin/reconciliation", { token }),
  reconcileOrder: (orderId: string, token?: string) =>
    request<ReconcileResponse>(ORDER, `/v1/orders/${encodeURIComponent(orderId)}/reconcile`, {
      method: "POST",
      body: "{}",
      token,
    }),
  listQuarantined: (token?: string) =>
    request<QuarantinedList>(ORDER, "/v1/admin/outbox/quarantined", { token }),
  replayEvent: (eventId: string, token?: string) =>
    request<ReplayResponse>(
      ORDER,
      `/v1/admin/outbox/events/${encodeURIComponent(eventId)}/replay`,
      { method: "POST", body: "{}", token },
    ),
  getCoordination: (token?: string) =>
    request<Coordination>(ADMISSION, "/v1/admin/coordination", { token }),
  fence: (reason: string, token?: string) =>
    request<Coordination>(ADMISSION, "/v1/admin/coordination/fence", {
      method: "POST",
      body: JSON.stringify({ reason }),
      token,
    }),
  unfence: (token?: string) =>
    request<Coordination>(ADMISSION, "/v1/admin/coordination/unfence", {
      method: "POST",
      body: "{}",
      token,
    }),

  // Sale (ops:sale:write / ops:sale:publish for mutations; list/get public)
  listOfferings: (token?: string) =>
    request<{ items: Offering[] }>(SALE, "/v1/offerings", { token }),
  getOffering: (id: string, token?: string) =>
    request<Offering>(SALE, `/v1/offerings/${encodeURIComponent(id)}`, { token }),
  createOffering: (
    body: { name: string; description?: string; category?: string },
    token?: string,
  ) =>
    request<Offering>(SALE, "/v1/offerings", {
      method: "POST",
      body: JSON.stringify(body),
      token,
    }),
  listSessions: (offeringId: string, token?: string) =>
    request<{ items: Session[] }>(
      SALE,
      `/v1/offerings/${encodeURIComponent(offeringId)}/sessions`,
      { token },
    ),
  createSession: (
    offeringId: string,
    body: {
      starts_at: string;
      ends_at: string;
      sale_opens_at: string;
      sale_closes_at: string;
      max_per_buyer?: number;
      admission_strategy?: string;
    },
    token?: string,
  ) =>
    request<{ session: Session; purchase_policy: { max_per_buyer: number } }>(
      SALE,
      `/v1/offerings/${encodeURIComponent(offeringId)}/sessions`,
      { method: "POST", body: JSON.stringify(body), token },
    ),
  listTicketTypes: (sessionId: string, token?: string) =>
    request<{ items: TicketType[] }>(
      SALE,
      `/v1/sessions/${encodeURIComponent(sessionId)}/ticket-types`,
      { token },
    ),
  createTicketType: (
    sessionId: string,
    body: {
      name: string;
      capacity: number;
      price_cents: number;
      section?: string;
      currency?: string;
    },
    token?: string,
  ) =>
    request<TicketType>(SALE, `/v1/sessions/${encodeURIComponent(sessionId)}/ticket-types`, {
      method: "POST",
      body: JSON.stringify(body),
      token,
    }),
  publishOffering: (offeringId: string, token?: string) =>
    request<{
      offering: Offering;
      sessions: Session[];
      ticket_types: TicketType[];
    }>(SALE, `/v1/offerings/${encodeURIComponent(offeringId)}/publish`, {
      method: "POST",
      body: "{}",
      token,
    }),

  // Budgets (ops:admission:budget write; read also allows read/fence)
  getBudget: (scope: string, token?: string) =>
    request<Budget>(ADMISSION, `/v1/admin/budgets/${encodeURIComponent(scope)}`, { token }),
  putBudget: (
    scope: string,
    body: { max_grants: number; max_active_checkouts: number },
    token?: string,
  ) =>
    request<Budget>(ADMISSION, `/v1/admin/budgets/${encodeURIComponent(scope)}`, {
      method: "PUT",
      body: JSON.stringify(body),
      token,
    }),
};
