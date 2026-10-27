import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { Link, useParams } from "react-router-dom";
import { api, getToken, HttpError } from "../api/client";
import { explainApiError, isTerminalOrderStatus, orderStatusCopy } from "../lib/statusCopy";

export function OrderDetailPage() {
  const { orderId = "" } = useParams();
  const signedIn = Boolean(getToken());
  const qc = useQueryClient();

  const q = useQuery({
    queryKey: ["order", orderId, signedIn],
    queryFn: () => api.getOrder(orderId),
    enabled: signedIn && Boolean(orderId),
    refetchInterval: (query) => {
      const st = query.state.data?.status;
      if (!st || isTerminalOrderStatus(st)) return false;
      return 8_000;
    },
  });

  useEffect(() => {
    // Drop cache when identity changes (signedIn flips / orderId changes handled by key)
    return () => {
      void qc.removeQueries({ queryKey: ["order", orderId] });
    };
  }, [orderId, qc]);

  const recon = useMutation({
    mutationFn: () => api.reconcileOrder(orderId),
    onSuccess: (r) => {
      void qc.setQueryData(["order", orderId, signedIn], r.order);
    },
  });

  if (!signedIn) {
    return (
      <p className="muted" role="status">
        Sign in to view order detail.
      </p>
    );
  }
  if (q.isLoading) return <p className="muted" role="status">Loading order…</p>;
  if (q.isError) {
    const e = explainApiError(q.error);
    return (
      <div role="alert" className="error">
        <strong>{e.title}</strong>
        <div>{e.action}</div>
        {q.error instanceof HttpError && q.error.status === 403 && (
          <p>This order is not available to the current buyer token.</p>
        )}
        <p>
          <Link to="/orders">← Orders</Link>
        </p>
      </div>
    );
  }

  const o = q.data!;
  const copy = orderStatusCopy(o.status);

  return (
    <div>
      <p>
        <Link to="/orders">← Orders</Link>
      </p>
      <h2>Order {o.id}</h2>
      <dl className="kv">
        <dt>Status</dt>
        <dd className={copy.tone === "ok" ? "ok" : copy.tone === "error" ? "error" : "warn"}>
          {copy.label}
        </dd>
        <dt>Session</dt>
        <dd className="break">{o.session_id}</dd>
        <dt>Qty / amount</dt>
        <dd>
          {o.quantity} · {(o.amount_cents / 100).toFixed(2)}
        </dd>
        <dt>Payment op</dt>
        <dd className="break">{o.payment_operation_id ?? "—"}</dd>
        <dt>Updated</dt>
        <dd>{o.updated_at ?? "—"}</dd>
      </dl>
      <p className="muted" role="status" aria-live="polite">
        {copy.next}
      </p>
      <div className="row">
        <button type="button" className="secondary" onClick={() => void q.refetch()} disabled={q.isFetching}>
          Refresh
        </button>
        {!isTerminalOrderStatus(o.status) && (
          <button
            type="button"
            disabled={recon.isPending}
            onClick={() => recon.mutate()}
            aria-describedby="recon-help"
          >
            {recon.isPending ? "Reconciling…" : "Reconcile"}
          </button>
        )}
      </div>
      <p id="recon-help" className="muted small">
        Reconcile asks the server to advance recovery. It does not guarantee confirmation.
      </p>
      {recon.isError && (
        <p className="error" role="alert">
          {explainApiError(recon.error).title}: {explainApiError(recon.error).action}
        </p>
      )}
      {recon.isSuccess && (
        <p className="info" role="status">
          Reconcile result: {recon.data.result} · {recon.data.order.status}
        </p>
      )}
    </div>
  );
}
