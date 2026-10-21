import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, HttpError } from "../api/client";
import type { Order } from "../api/types";

function statusClass(status: string) {
  if (status === "CONFIRMED") return "ok";
  if (status.includes("RECON") || status.includes("PENDING")) return "warn";
  return "";
}

export function ReconciliationPage() {
  const qc = useQueryClient();
  const [selected, setSelected] = useState<Order | null>(null);
  const [actionMsg, setActionMsg] = useState<string>("");

  const q = useQuery({
    queryKey: ["reconciliation"],
    queryFn: () => api.getReconciliation(),
    refetchInterval: 10_000,
  });

  const mutate = useMutation({
    mutationFn: (orderId: string) => api.reconcileOrder(orderId),
    onMutate: () => setActionMsg("Submitting reconcile…"),
    onSuccess: (data) => {
      setActionMsg(`Server result: ${data.result} · status ${data.order.status}`);
      setSelected(data.order);
      void qc.invalidateQueries({ queryKey: ["reconciliation"] });
    },
    onError: (err: unknown) => {
      if (err instanceof HttpError) {
        if (err.status === 0) {
          setActionMsg(
            `Network/timeout: ${err.message}. Outcome uncertain — refresh before retrying.`,
          );
        } else if (err.status === 401) {
          setActionMsg("401 Unauthorized — operator JWT missing or invalid.");
        } else if (err.status === 403) {
          setActionMsg("403 Forbidden — need ops:order:recover scope.");
        } else {
          setActionMsg(`Error ${err.status}: ${err.message}`);
        }
      } else {
        setActionMsg("Unexpected error");
      }
    },
  });

  const recon = q.data?.reconciliation_required ?? [];
  const pending = q.data?.payment_pending ?? [];

  return (
    <div>
      <div className="page-head">
        <h2>Reconciliation</h2>
        <button type="button" className="secondary" onClick={() => void q.refetch()} disabled={q.isFetching}>
          Refresh
        </button>
      </div>
      <p className="muted">
        Lists orders needing recovery. Reconcile requests the backend Advance path; confirmation is
        only from the server response.
      </p>

      {q.isLoading && <p role="status">Loading…</p>}
      {q.isError && (
        <p className="error" role="alert">
          {q.error instanceof HttpError
            ? `${q.error.status || "net"}: ${q.error.message}`
            : "Failed to load"}
        </p>
      )}

      {actionMsg && (
        <p className={mutate.isError ? "error" : "info"} role="status">
          {actionMsg}
        </p>
      )}

      {!q.isLoading && !q.isError && recon.length === 0 && pending.length === 0 && (
        <p className="muted">No reconciliation-required or payment-pending orders.</p>
      )}

      {recon.length > 0 && (
        <section aria-labelledby="recon-heading">
          <h3 id="recon-heading">Reconciliation required ({recon.length})</h3>
          <OrderTable
            orders={recon}
            selectedId={selected?.id}
            onSelect={setSelected}
            onReconcile={(id) => mutate.mutate(id)}
            busy={mutate.isPending}
          />
        </section>
      )}

      {pending.length > 0 && (
        <section aria-labelledby="pending-heading">
          <h3 id="pending-heading">Payment pending ({pending.length})</h3>
          <OrderTable
            orders={pending}
            selectedId={selected?.id}
            onSelect={setSelected}
            onReconcile={(id) => mutate.mutate(id)}
            busy={mutate.isPending}
          />
        </section>
      )}

      {selected && (
        <aside className="detail card" aria-label="Order detail">
          <h3>Detail · {selected.id}</h3>
          <dl className="kv">
            <dt>Status</dt>
            <dd className={statusClass(selected.status)}>{selected.status}</dd>
            <dt>Buyer</dt>
            <dd>{selected.buyer_id}</dd>
            <dt>Session</dt>
            <dd>{selected.session_id}</dd>
            <dt>Payment op</dt>
            <dd>{selected.payment_operation_id ?? "—"}</dd>
            <dt>Compensation op</dt>
            <dd>{selected.compensation_operation_id ?? "—"}</dd>
            <dt>Updated</dt>
            <dd>{selected.updated_at ?? "—"}</dd>
          </dl>
          <button
            type="button"
            disabled={mutate.isPending}
            onClick={() => mutate.mutate(selected.id)}
          >
            {mutate.isPending ? "Reconciling…" : "Request reconcile"}
          </button>
        </aside>
      )}
    </div>
  );
}

function OrderTable({
  orders,
  selectedId,
  onSelect,
  onReconcile,
  busy,
}: {
  orders: Order[];
  selectedId?: string;
  onSelect: (o: Order) => void;
  onReconcile: (id: string) => void;
  busy: boolean;
}) {
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th scope="col">Order</th>
            <th scope="col">Status</th>
            <th scope="col">Buyer</th>
            <th scope="col">Updated</th>
            <th scope="col">Actions</th>
          </tr>
        </thead>
        <tbody>
          {orders.map((o) => (
            <tr key={o.id} className={selectedId === o.id ? "selected" : undefined}>
              <td>
                <button type="button" className="linkish" onClick={() => onSelect(o)}>
                  {o.id}
                </button>
              </td>
              <td className={statusClass(o.status)}>{o.status}</td>
              <td>{o.buyer_id}</td>
              <td>{o.updated_at ?? "—"}</td>
              <td>
                <button type="button" disabled={busy} onClick={() => onReconcile(o.id)}>
                  Reconcile
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
