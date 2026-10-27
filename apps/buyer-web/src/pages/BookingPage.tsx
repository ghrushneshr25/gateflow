import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import {
  api,
  clearIdempotencyKey,
  getOrCreateIdempotencyKey,
  getOrderHint,
  getToken,
  HttpError,
  setOrderHint,
} from "../api/client";
import type { Order } from "../api/types";
import { explainApiError, isTerminalOrderStatus, orderStatusCopy } from "../lib/statusCopy";

const POLL_MS = 2500;
const POLL_MAX = 40;

export function BookingPage() {
  const { offeringId = "" } = useParams();
  const [sp] = useSearchParams();
  const sessionId = sp.get("session") ?? "";
  const ticketTypeId = sp.get("tt") ?? "";
  const poolId = sp.get("pool") ?? "";
  const price = Number(sp.get("price") || "0");
  const nav = useNavigate();
  const qc = useQueryClient();

  const [step, setStep] = useState<"queue" | "grant" | "checkout" | "done">("queue");
  const [banner, setBanner] = useState<{ tone: "error" | "info" | "ok"; title: string; action: string } | null>(
    null,
  );
  const [entitlementId, setEntitlementId] = useState<string | null>(null);
  const [entExpired, setEntExpired] = useState(false);
  const [order, setOrder] = useState<Order | null>(null);
  const [checkoutBusy, setCheckoutBusy] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const pollCount = useRef(0);
  const abortPoll = useRef(false);

  const signedIn = Boolean(getToken());

  const meQ = useQuery({
    queryKey: ["queue-me", sessionId, signedIn],
    queryFn: () => api.queueMe(sessionId),
    enabled: Boolean(sessionId) && signedIn && step !== "done",
    refetchInterval: (q) => {
      if (abortPoll.current || step === "done" || step === "checkout") return false;
      if (!signedIn) return false;
      const st = q.state.data?.status;
      if (st === "GRANTED") return false;
      if (pollCount.current >= POLL_MAX) return false;
      return POLL_MS;
    },
  });

  useEffect(() => {
    abortPoll.current = false;
    pollCount.current = 0;
    // Recover uncertain prior checkout for this buyer+session+ticket
    const hint = getOrderHint(sessionId, ticketTypeId);
    if (hint && signedIn) {
      void api
        .getOrder(hint)
        .then((o) => {
          setOrder(o);
          setStep("done");
          setBanner({
            tone: "info",
            title: "Restored prior checkout attempt",
            action: `Order ${o.id} status is ${o.status}. Refresh or reconcile; same Idempotency-Key still applies until CONFIRMED.`,
          });
        })
        .catch(() => {
          /* hint stale */
        });
    }
    return () => {
      abortPoll.current = true;
    };
  }, [sessionId, ticketTypeId, signedIn]);

  useEffect(() => {
    if (meQ.dataUpdatedAt) pollCount.current += 1;
  }, [meQ.dataUpdatedAt]);

  useEffect(() => {
    const e = meQ.data;
    if (e?.status === "GRANTED" && e.entitlement_id) {
      setEntitlementId(e.entitlement_id);
      setEntExpired(false);
      setStep((s) => (s === "done" ? s : "checkout"));
    }
  }, [meQ.data]);

  const join = useMutation({
    mutationFn: () => api.joinQueue(sessionId),
    onSuccess: (entry) => {
      setBanner({
        tone: "info",
        title: "Joined queue",
        action: `Status ${entry.status}. Next: request admission grant when ready.`,
      });
      setStep("grant");
      void qc.invalidateQueries({ queryKey: ["queue-me", sessionId] });
    },
    onError: showErr,
  });

  const grant = useMutation({
    mutationFn: () => api.requestGrant(sessionId),
    onSuccess: (data) => {
      setEntitlementId(data.entitlement.id);
      setEntExpired(false);
      setBanner({
        tone: "ok",
        title: `Grant ${data.entitlement.status}`,
        action: `Entitlement ${data.entitlement.id}. Proceed to checkout before it expires.`,
      });
      setStep("checkout");
      void qc.invalidateQueries({ queryKey: ["queue-me", sessionId] });
    },
    onError: showErr,
  });

  function showErr(err: unknown) {
    const e = explainApiError(err);
    setBanner({ tone: "error", title: e.title, action: e.action });
    if (err instanceof HttpError && (err.code?.includes("EXPIR") || /expir/i.test(err.message))) {
      setEntExpired(true);
    }
  }

  async function runCheckout() {
    if (!entitlementId || !poolId || checkoutBusy) return;
    if (!getToken()) {
      setBanner({
        tone: "error",
        title: "Not signed in",
        action: "Sign in with your buyer name, then checkout.",
      });
      return;
    }
    if (entExpired) {
      setBanner({
        tone: "error",
        title: "Grant may be expired",
        action: "Request a new grant before checkout.",
      });
      return;
    }
    setCheckoutBusy(true);
    setUncertain(false);
    setBanner({ tone: "info", title: "Submitting checkout…", action: "Waiting for authoritative order status." });
    const idem = getOrCreateIdempotencyKey(sessionId, ticketTypeId);
    try {
      const o = await api.checkout({
        session_id: sessionId,
        ticket_type_id: ticketTypeId,
        pool_id: poolId,
        entitlement_id: entitlementId,
        quantity: 1,
        amount_cents: price || 0,
        idempotency_key: idem,
      });
      setOrder(o);
      setOrderHint(sessionId, ticketTypeId, o.id);
      setStep("done");
      const copy = orderStatusCopy(o.status);
      if (o.status === "CONFIRMED") {
        clearIdempotencyKey(sessionId, ticketTypeId);
        setBanner({ tone: "ok", title: copy.label, action: copy.next });
      } else {
        setBanner({
          tone: "info",
          title: copy.label,
          action: `${copy.next} Idempotency-Key retained for this attempt.`,
        });
      }
    } catch (err) {
      if (err instanceof HttpError && err.status === 0) {
        setUncertain(true);
        setStep("done");
        setBanner({
          tone: "error",
          title: "Checkout outcome uncertain",
          action: `Request may have been accepted. Same key kept (${idem.slice(0, 8)}…). Use “Find prior order” / Orders list before starting a new attempt.`,
        });
      } else {
        showErr(err);
      }
    } finally {
      setCheckoutBusy(false);
    }
  }

  async function refreshOrder() {
    if (!order?.id) return;
    try {
      const o = await api.getOrder(order.id);
      setOrder(o);
      const copy = orderStatusCopy(o.status);
      setBanner({ tone: copy.tone === "error" ? "error" : copy.tone === "ok" ? "ok" : "info", title: copy.label, action: copy.next });
      if (o.status === "CONFIRMED") clearIdempotencyKey(sessionId, ticketTypeId);
      setUncertain(false);
    } catch (e) {
      showErr(e);
    }
  }

  async function reconcile() {
    if (!order?.id || checkoutBusy) return;
    setCheckoutBusy(true);
    try {
      const r = await api.reconcileOrder(order.id);
      setOrder(r.order);
      const copy = orderStatusCopy(r.order.status);
      setBanner({
        tone: copy.tone === "ok" ? "ok" : "info",
        title: `Reconcile: ${r.result}`,
        action: copy.next,
      });
      if (r.order.status === "CONFIRMED") clearIdempotencyKey(sessionId, ticketTypeId);
      setUncertain(false);
    } catch (e) {
      showErr(e);
    } finally {
      setCheckoutBusy(false);
    }
  }

  async function findPriorOrder() {
    const hint = getOrderHint(sessionId, ticketTypeId);
    if (hint) {
      try {
        const o = await api.getOrder(hint);
        setOrder(o);
        setStep("done");
        setUncertain(false);
        const copy = orderStatusCopy(o.status);
        setBanner({ tone: "info", title: "Found prior order", action: copy.next });
        return;
      } catch {
        /* fall through */
      }
    }
    try {
      const list = await api.listOrders();
      const match = (list.items ?? []).find(
        (o) => o.session_id === sessionId && o.ticket_type_id === ticketTypeId,
      );
      if (match) {
        setOrder(match);
        setOrderHint(sessionId, ticketTypeId, match.id);
        setStep("done");
        setBanner({
          tone: "info",
          title: "Matched order from history",
          action: orderStatusCopy(match.status).next,
        });
      } else {
        setBanner({
          tone: "info",
          title: "No matching order found",
          action: "If checkout never reached the server, you may checkout again with the same Idempotency-Key.",
        });
      }
    } catch (e) {
      showErr(e);
    }
  }

  if (!sessionId || !ticketTypeId || !poolId) {
    return (
      <p className="error" role="alert">
        Missing booking params. <Link to={`/offerings/${offeringId}`}>Back to offering</Link>
      </p>
    );
  }

  const status = order ? orderStatusCopy(order.status) : null;
  const pollExhausted = pollCount.current >= POLL_MAX && meQ.data?.status !== "GRANTED";

  return (
    <div>
      <p>
        <Link to={`/offerings/${offeringId}`}>← Offering</Link>
      </p>
      <h2>Book</h2>
      <p className="muted small">
        session {sessionId} · ticket {ticketTypeId} · pool {poolId}
      </p>

      {banner && (
        <div
          className={`banner ${banner.tone === "error" ? "error" : banner.tone === "ok" ? "ok" : "info"}`}
          role="status"
          aria-live="polite"
        >
          <strong>{banner.title}</strong>
          <div>{banner.action}</div>
        </div>
      )}

      {!signedIn && (
        <p className="warn" role="status">
          Sign in above before queue, grant, or checkout.
        </p>
      )}

      <ol className="steps" aria-label="Booking steps">
        <li className={step === "queue" ? "active" : ""}>1. Join queue</li>
        <li className={step === "grant" ? "active" : ""}>2. Admission grant</li>
        <li className={step === "checkout" || step === "done" ? "active" : ""}>3. Checkout</li>
      </ol>

      <section className="panel" aria-labelledby="queue-heading">
        <h3 id="queue-heading">Queue</h3>
        <div className="row">
          <button
            type="button"
            disabled={join.isPending || !signedIn}
            onClick={() => join.mutate()}
            aria-describedby="queue-help"
          >
            {join.isPending ? "Joining…" : "Join queue"}
          </button>
          <button type="button" className="secondary" onClick={() => void meQ.refetch()} disabled={!signedIn}>
            Refresh status
          </button>
        </div>
        <p id="queue-help" className="muted small">
          Join is idempotent for your buyer. Status comes from the server only.
        </p>
        {meQ.data && (
          <p className="muted">
            Status: <strong>{meQ.data.status}</strong>
            {meQ.data.entitlement_id ? ` · entitlement ${meQ.data.entitlement_id}` : ""}
            {meQ.data.sequence_no != null ? ` · seq ${meQ.data.sequence_no}` : ""}
          </p>
        )}
        {meQ.isError && meQ.error instanceof HttpError && meQ.error.status === 404 && (
          <p className="muted">Not in queue yet.</p>
        )}
        {pollExhausted && (
          <p className="warn">Polling stopped after {POLL_MAX} checks. Use Refresh status.</p>
        )}
      </section>

      <section className="panel" aria-labelledby="grant-heading">
        <h3 id="grant-heading">Admission</h3>
        <button type="button" disabled={grant.isPending || !signedIn} onClick={() => grant.mutate()}>
          {grant.isPending ? "Requesting…" : entExpired ? "Request new grant" : "Request grant"}
        </button>
        {entitlementId && !entExpired && (
          <p className="ok">
            Entitlement: <code>{entitlementId}</code>
          </p>
        )}
        {entExpired && (
          <p className="warn" role="status">
            Previous grant may be expired. Request a new grant before checkout.
          </p>
        )}
      </section>

      <section className="panel" aria-labelledby="checkout-heading">
        <h3 id="checkout-heading">Checkout</h3>
        <p className="muted small" id="checkout-help">
          One Idempotency-Key per buyer+session+ticket until CONFIRMED. Timeouts keep the same key.
          Payment success alone is not confirmation.
        </p>
        <div className="row">
          <button
            type="button"
            disabled={checkoutBusy || !entitlementId || !signedIn || entExpired}
            onClick={() => void runCheckout()}
            aria-describedby="checkout-help"
          >
            {checkoutBusy ? "Working…" : uncertain ? "Retry checkout (same key)" : "Checkout"}
          </button>
          {(uncertain || step === "done") && (
            <button type="button" className="secondary" onClick={() => void findPriorOrder()}>
              Find prior order
            </button>
          )}
        </div>
      </section>

      {order && (
        <section className="panel" aria-live="polite" aria-labelledby="order-heading">
          <h3 id="order-heading">Order status (authoritative)</h3>
          <p>
            <Link to={`/orders/${order.id}`}>{order.id}</Link> ·{" "}
            <strong className={status?.tone === "ok" ? "ok" : status?.tone === "error" ? "error" : "warn"}>
              {status?.label ?? order.status}
            </strong>
          </p>
          <p className="muted small">{status?.next}</p>
          <div className="row">
            <button type="button" className="secondary" disabled={checkoutBusy} onClick={() => void refreshOrder()}>
              Refresh order
            </button>
            {!isTerminalOrderStatus(order.status) && (
              <button type="button" disabled={checkoutBusy} onClick={() => void reconcile()}>
                Reconcile
              </button>
            )}
            {order.status === "CONFIRMED" && (
              <button type="button" className="secondary" onClick={() => nav(`/orders/${order.id}`)}>
                View order
              </button>
            )}
          </div>
        </section>
      )}
    </div>
  );
}
