import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, HttpError } from "../api/client";

export function OfferingsPage() {
  const q = useQuery({
    queryKey: ["offerings-published"],
    queryFn: () => api.listOfferings(),
  });

  if (q.isLoading) return <p className="muted" role="status">Loading offerings…</p>;
  if (q.isError) {
    return (
      <div>
        <h2>Offerings</h2>
        <p className="error" role="alert">
          {q.error instanceof HttpError
            ? `${q.error.status || "net"}: ${q.error.message}`
            : String(q.error)}
        </p>
      </div>
    );
  }

  const items = (q.data?.items ?? []).filter((o) => o.status === "PUBLISHED");
  return (
    <div>
      <div className="row" style={{ justifyContent: "space-between" }}>
        <h2>Published offerings</h2>
        <button type="button" className="secondary" onClick={() => void q.refetch()} disabled={q.isFetching}>
          Refresh
        </button>
      </div>
      <p className="muted small">
        Listings can go stale. Availability is confirmed only when queue/grant/checkout succeed on the
        server.
      </p>
      {items.length === 0 ? (
        <p className="muted" role="status">
          No published offerings. An operator must publish a sale first.
        </p>
      ) : (
        <ul className="list">
          {items.map((o) => (
            <li key={o.id}>
              <Link to={`/offerings/${o.id}`}>
                <strong>{o.name}</strong>
              </Link>{" "}
              <span className="muted">
                ({o.category}) · {o.status}
              </span>
              {o.description ? <div className="muted">{o.description}</div> : null}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
