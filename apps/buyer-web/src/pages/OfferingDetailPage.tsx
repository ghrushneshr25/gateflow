import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api, HttpError } from "../api/client";

export function OfferingDetailPage() {
  const { offeringId = "" } = useParams();
  const nav = useNavigate();

  const offQ = useQuery({
    queryKey: ["offering", offeringId],
    queryFn: () => api.getOffering(offeringId),
    enabled: Boolean(offeringId),
  });
  const sesQ = useQuery({
    queryKey: ["sessions", offeringId],
    queryFn: () => api.listSessions(offeringId),
    enabled: Boolean(offeringId),
  });

  const session = sesQ.data?.items[0];
  const ttQ = useQuery({
    queryKey: ["tickets", session?.id],
    queryFn: () => api.listTicketTypes(session!.id),
    enabled: Boolean(session?.id),
  });

  if (offQ.isLoading || sesQ.isLoading) return <p className="muted">Loading…</p>;
  if (offQ.isError) {
    return (
      <p className="error" role="alert">
        {offQ.error instanceof HttpError ? offQ.error.message : "Failed to load"}
      </p>
    );
  }

  const o = offQ.data!;
  const tickets = ttQ.data?.items ?? [];

  return (
    <div>
      <p>
        <Link to="/offerings">← Offerings</Link>
      </p>
      <h2>{o.name}</h2>
      <p className="muted">
        {o.category} · {o.status}
      </p>
      {o.status !== "PUBLISHED" && (
        <p className="warn">This offering is not PUBLISHED; checkout may fail.</p>
      )}
      {!session && <p className="muted">No sessions configured.</p>}
      <div className="row">
        <button
          type="button"
          className="secondary"
          onClick={() => {
            void offQ.refetch();
            void sesQ.refetch();
            void ttQ.refetch();
          }}
        >
          Refresh details
        </button>
      </div>
      {session && (
        <section className="panel" aria-labelledby="session-heading">
          <h3 id="session-heading">Session</h3>
          <p className="muted small break">
            {session.id} · sale {session.sale_opens_at} → {session.sale_closes_at}
          </p>
          {ttQ.isLoading && <p className="muted" role="status">Loading ticket types…</p>}
          {tickets.length === 0 && !ttQ.isLoading && (
            <p className="muted" role="status">
              No ticket types (or missing inventory pool after publish).
            </p>
          )}
          <ul className="list">
            {tickets.map((tt) => {
              const poolOk = Boolean(tt.inventory_pool_id);
              const canContinue = poolOk && o.status === "PUBLISHED";
              return (
                <li key={tt.id}>
                  <strong>{tt.name}</strong> · {(tt.price_cents / 100).toFixed(2)} {tt.currency} ·
                  listed cap {tt.capacity}
                  {!poolOk && <span className="warn"> · no pool (not bookable)</span>}
                  <div className="row">
                    <button
                      type="button"
                      disabled={!canContinue}
                      aria-disabled={!canContinue}
                      onClick={() =>
                        nav(
                          `/book/${offeringId}?session=${encodeURIComponent(session.id)}&tt=${encodeURIComponent(tt.id)}&pool=${encodeURIComponent(tt.inventory_pool_id!)}&price=${tt.price_cents}`,
                        )
                      }
                    >
                      Continue to queue
                    </button>
                  </div>
                  {!canContinue && (
                    <p className="muted small">Refresh after operator publish if this stays disabled.</p>
                  )}
                </li>
              );
            })}
          </ul>
        </section>
      )}
    </div>
  );
}
