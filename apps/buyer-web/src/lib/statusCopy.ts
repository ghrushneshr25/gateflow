import { HttpError } from "../api/client";

/** Human-readable order status — never invents success. */
export function orderStatusCopy(status: string): { label: string; tone: "ok" | "warn" | "error" | "muted"; next: string } {
  switch (status) {
    case "CONFIRMED":
      return {
        label: "Confirmed",
        tone: "ok",
        next: "Your booking is confirmed by the server. No further action needed.",
      };
    case "PAYMENT_PENDING":
      return {
        label: "Payment pending",
        tone: "warn",
        next: "Payment outcome is not final. Refresh or reconcile; do not start a new checkout for the same attempt.",
      };
    case "RECONCILIATION_REQUIRED":
      return {
        label: "Needs reconciliation",
        tone: "warn",
        next: "The server needs recovery. Use Reconcile (same order). This is not a confirmed booking.",
      };
    case "FAILED":
      return {
        label: "Failed",
        tone: "error",
        next: "This order failed. Start a new booking if you still want tickets.",
      };
    case "PAYMENT_SUCCEEDED":
      return {
        label: "Payment recorded — not confirmed",
        tone: "warn",
        next: "Payment success alone is not confirmation. Refresh or reconcile until CONFIRMED or FAILED.",
      };
    default:
      return {
        label: status || "Unknown",
        tone: "muted",
        next: "Refresh for authoritative status. Do not assume success or failure.",
      };
  }
}

export function explainApiError(err: unknown): { title: string; action: string; retryable: boolean } {
  if (!(err instanceof HttpError)) {
    return { title: "Unexpected error", action: "Refresh and try again if the problem continues.", retryable: true };
  }
  const code = err.code ?? "";
  if (err.status === 0) {
    return {
      title: "Network timeout or offline",
      action:
        "Outcome may already be on the server. Refresh status before retrying. Checkout keeps the same Idempotency-Key.",
      retryable: true,
    };
  }
  if (err.status === 401) {
    return {
      title: "Not signed in",
      action: "Sign in with your buyer name above, then continue.",
      retryable: false,
    };
  }
  if (err.status === 403) {
    return {
      title: "Not allowed",
      action: "This buyer cannot perform that action. Check you are using your own token.",
      retryable: false,
    };
  }
  if (code === "ADM_COORDINATION_FENCED" || err.status === 503 && /fenc/i.test(err.message)) {
    return {
      title: "Admission temporarily fenced",
      action: "New grants are blocked by operators. Wait and refresh; do not invent a grant locally.",
      retryable: true,
    };
  }
  if (code === "ADM_BUDGET_EXHAUSTED" || err.status === 429) {
    return {
      title: "Admission budget exhausted",
      action: "No more grants available for this session right now. Try another session or try later.",
      retryable: false,
    };
  }
  if (code === "INV_CAPACITY_EXCEEDED" || /capacity|sold.?out/i.test(err.message)) {
    return {
      title: "Sold out or no capacity",
      action: "Inventory rejected the reservation. Choose another ticket type or offering.",
      retryable: false,
    };
  }
  if (code === "ADM_ENTITLEMENT_EXPIRED" || code === "INV_RESERVATION_EXPIRED" || /expir/i.test(code + err.message)) {
    return {
      title: "Grant or reservation expired",
      action: "Request a new grant if still eligible, then checkout again (new logical attempt only after expiry is confirmed).",
      retryable: true,
    };
  }
  if (code === "ORD_IDEMPOTENCY_CONFLICT") {
    return {
      title: "Checkout conflict",
      action: "Same key was used with different details. Refresh orders; do not force a new key unless starting a new booking.",
      retryable: false,
    };
  }
  if (err.status === 404) {
    return {
      title: "Not found",
      action: "Resource missing or not yours. Go back and refresh listings.",
      retryable: false,
    };
  }
  if (err.status === 409) {
    return {
      title: "Conflict",
      action: err.message || "Server rejected a conflicting state. Refresh and follow the status shown.",
      retryable: true,
    };
  }
  return {
    title: `Error ${err.status}`,
    action: err.message || "Refresh authoritative status before retrying.",
    retryable: err.status >= 500 || err.status === 0,
  };
}

export function isTerminalOrderStatus(status: string): boolean {
  return status === "CONFIRMED" || status === "FAILED";
}
