import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { Link } from "react-router-dom";
import { api, getToken, HttpError, tokenScope } from "../api/client";
import { explainApiError, orderStatusCopy } from "../lib/statusCopy";

export function OrdersPage() {
  const signedIn = Boolean(getToken());
  const scope = signedIn ? tokenScope() : "anon";
  const qc = useQueryClient();

  const q = useQuery({
    queryKey: ["orders", scope],
    queryFn: () => api.listOrders(),
    enabled: signedIn,
    refetchInterval: (query) => {
      const items = query.state.data?.items ?? [];
      const anyOpen = items.some(
        (o) => o.status !== "CONFIRMED" && o.status !== "FAILED",
      );
      return anyOpen ? 10_000 : false;
    },
  });

  useEffect(() => {
    return () => {
      void qc.removeQueries({ queryKey: ["orders"] });
    };
  }, [scope, qc]);

  if (!signedIn) {
    return (
      <div>
        <h2>Orders</h2>
        <p className="muted" role="status">
          Sign in to list <em>your</em> orders only.
        </p>
      </div>
    );
  }
  if (q.isLoading) return <p className="muted" role="status">Loading orders…</p>;
  if (q.isError) {
    const e = explainApiError(q.error);
    return (
      <div role="alert" className="error">
        <h2>Orders</h2>
        <strong>{e.title}</strong>
        <div>{e.action}</div>
        {q.error instanceof HttpError && q.error.status === 0 && (
          <button type="button" className="secondary" onClick={() => void q.refetch()}>
            Retry
          </button>
        )}
      </div>
    );
  }
  const items = q.data?.items ?? [];
  return (
    <div>
      <div className="row" style={{ justifyContent: "space-between" }}>
        <h2>Your orders</h2>
        <button type="button" className="secondary" onClick={() => void q.refetch()} disabled={q.isFetching}>
          Refresh
        </button>
      </div>
      {items.length === 0 ? (
        <p className="muted">No orders yet.</p>
      ) : (
        <ul className="list">
          {items.map((o) => {
            const copy = orderStatusCopy(o.status);
            return (
              <li key={o.id}>
                <Link to={`/orders/${o.id}`}>
                  <strong>{o.id}</strong>
                </Link>{" "}
                ·{" "}
                <span className={copy.tone === "ok" ? "ok" : copy.tone === "error" ? "error" : "warn"}>
                  {copy.label}
                </span>{" "}
                · qty {o.quantity} · {(o.amount_cents / 100).toFixed(2)}
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
