import { useState } from "react";
import { Link, Route, Routes } from "react-router-dom";
import { AuthBar } from "./auth/AuthBar";
import { BudgetsPage } from "./pages/BudgetsPage";
import { CoordinationPage } from "./pages/CoordinationPage";
import { OutboxPage } from "./pages/OutboxPage";
import { ReconciliationPage } from "./pages/ReconciliationPage";
import { SalesPage } from "./pages/SalesPage";

export function App() {
  const [, setAuthTick] = useState(0);
  return (
    <div className="shell">
      <header>
        <span className="badge">Operator Console · P5.2.1</span>
        <h1>Gateflow Ops</h1>
        <p className="muted">
          Recovery spine + sale configuration + admission budgets. Backend is authoritative.
        </p>
        <nav aria-label="Primary">
          <Link to="/">Home</Link>
          <Link to="/sales">Sales</Link>
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
                <h2>Control plane</h2>
                <ul className="muted">
                  <li>
                    <Link to="/sales">Sales</Link> — draft offerings, sessions, ticket types, publish
                  </li>
                  <li>
                    <Link to="/budgets">Budgets</Link> — admission grant/active checkout caps
                  </li>
                  <li>
                    <Link to="/reconciliation">Reconciliation</Link> — ambiguous orders
                  </li>
                  <li>
                    <Link to="/outbox">Outbox</Link> — quarantine replay
                  </li>
                  <li>
                    <Link to="/coordination">Coordination</Link> — fence / unfence
                  </li>
                </ul>
              </div>
            }
          />
          <Route path="/sales" element={<SalesPage />} />
          <Route path="/budgets" element={<BudgetsPage />} />
          <Route path="/reconciliation" element={<ReconciliationPage />} />
          <Route path="/outbox" element={<OutboxPage />} />
          <Route path="/coordination" element={<CoordinationPage />} />
        </Routes>
      </main>
    </div>
  );
}
