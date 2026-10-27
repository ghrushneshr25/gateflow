import { useState } from "react";
import { Link, Route, Routes } from "react-router-dom";
import { AuthBar } from "./auth/AuthBar";
import { BudgetsPage } from "./pages/BudgetsPage";
import { CoordinationPage } from "./pages/CoordinationPage";
import { OutboxPage } from "./pages/OutboxPage";
import { ReconciliationPage } from "./pages/ReconciliationPage";
import { CreateEventPage } from "./pages/CreateEventPage";
import { SalesPage } from "./pages/SalesPage";
import { UsersPage } from "./pages/UsersPage";

export function App() {
  const [, setAuthTick] = useState(0);
  return (
    <div className="shell">
      <header>
        <span className="badge">Admin · Operator Console</span>
        <h1>Gateflow Admin</h1>
        <p className="muted">
          Create events, publish inventory, fence admission, recover orders. Separate from Buyer Web.
        </p>
        <nav aria-label="Primary">
          <Link to="/">Home</Link>
          <Link to="/events/new">Create event</Link>
          <Link to="/sales">Sales</Link>
          <Link to="/users">Users</Link>
          <Link to="/budgets">Budgets</Link>
          <Link to="/reconciliation">Reconciliation</Link>
          <Link to="/outbox">Outbox</Link>
          <Link to="/coordination">Coordination</Link>
        </nav>
      </header>
      <AuthBar onChange={() => setAuthTick((n) => n + 1)} />
      <main className="card">
        <Routes>
          <Route
            path="/"
            element={
              <div>
                <h2>Admin control plane</h2>
                <ul className="muted">
                  <li>
                    <Link to="/events/new">Create event</Link> — name, capacity, price → publish
                    (buyers can book)
                  </li>
                  <li>
                    <Link to="/sales">Sales</Link> — step-by-step draft / session / ticket / publish
                  </li>
                  <li>
                    <Link to="/budgets">Budgets</Link> — admission caps
                  </li>
                  <li>
                    <Link to="/coordination">Coordination</Link> — fence / unfence
                  </li>
                  <li>
                    <Link to="/reconciliation">Reconciliation</Link> · <Link to="/outbox">Outbox</Link>
                  </li>
                </ul>
                <p className="muted small">
                  Buyer app is a separate deployment (port 8088). Admin is this app (port 8089).
                </p>
              </div>
            }
          />
          <Route path="/events/new" element={<CreateEventPage />} />
          <Route path="/sales" element={<SalesPage />} />
          <Route path="/users" element={<UsersPage />} />
          <Route path="/budgets" element={<BudgetsPage />} />
          <Route path="/reconciliation" element={<ReconciliationPage />} />
          <Route path="/outbox" element={<OutboxPage />} />
          <Route path="/coordination" element={<CoordinationPage />} />
        </Routes>
      </main>
    </div>
  );
}
