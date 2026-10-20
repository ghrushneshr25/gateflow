import { useQuery } from "@tanstack/react-query";
import { Link, Route, Routes } from "react-router-dom";
import { api } from "./api/client";

const BUYER_ID = "buyer_web_demo";

export function App() {
  return (
    <div className="shell">
      <header>
        <span className="badge">Buyer Web · Phase 2</span>
        <h1>Gateflow</h1>
        <p className="muted">
          Discover offerings, join admission, and complete recoverable checkout.
        </p>
        <nav>
          <Link to="/">Home</Link>
          <Link to="/offerings">Offerings</Link>
          <Link to="/orders">Orders</Link>
        </nav>
      </header>
      <main className="card">
        <Routes>
          <Route
            path="/"
            element={
              <div>
                <h2>Welcome</h2>
                <p className="muted">
                  Browse published offerings. Backend VS-1 path is live via local services.
                </p>
              </div>
            }
          />
          <Route path="/offerings" element={<OfferingsPage />} />
          <Route path="/orders" element={<OrdersPage />} />
        </Routes>
      </main>
    </div>
  );
}

function OfferingsPage() {
  const q = useQuery({
    queryKey: ["offerings"],
    queryFn: api.listOfferings,
    refetchInterval: 5000,
  });
  if (q.isLoading) return <p className="muted">Loading offerings…</p>;
  if (q.isError) {
    return (
      <div>
        <h2>Offerings</h2>
        <p className="muted">Could not reach Sale API. Is it running on :8081?</p>
        <pre style={{ whiteSpace: "pre-wrap" }}>{String(q.error)}</pre>
      </div>
    );
  }
  const items = q.data?.items ?? [];
  return (
    <div>
      <h2>Offerings</h2>
      {items.length === 0 ? (
        <p className="muted">No published offerings yet.</p>
      ) : (
        <ul>
          {items.map((o) => (
            <li key={o.id}>
              <strong>{o.name}</strong>{" "}
              <span className="muted">
                ({o.category}) · {o.status}
              </span>
              <div className="muted">{o.description}</div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function OrdersPage() {
  const q = useQuery({
    queryKey: ["orders", BUYER_ID],
    queryFn: () => api.listOrders(BUYER_ID),
    refetchInterval: 5000,
  });
  if (q.isLoading) return <p className="muted">Loading orders…</p>;
  if (q.isError) {
    return (
      <div>
        <h2>Orders</h2>
        <p className="muted">Could not reach Order API (:8084).</p>
      </div>
    );
  }
  const items = q.data?.items ?? [];
  return (
    <div>
      <h2>Orders</h2>
      {items.length === 0 ? (
        <p className="muted">No orders for {BUYER_ID}.</p>
      ) : (
        <ul>
          {items.map((o) => (
            <li key={o.id}>
              <strong>{o.id}</strong> · {o.status} · qty {o.quantity} ·{" "}
              {(o.amount_cents / 100).toFixed(2)}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
