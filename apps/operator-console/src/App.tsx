import { Link, Route, Routes } from "react-router-dom";

export function App() {
  return (
    <div className="shell">
      <header>
        <span className="badge">Operator Console · Phase 1</span>
        <h1>Gateflow Ops</h1>
        <p className="muted">
          Configure sales, inspect admission and inventory, recover workflows.
        </p>
        <nav>
          <Link to="/">Home</Link>
          <Link to="/offerings">Offerings</Link>
          <Link to="/reconciliation">Reconciliation</Link>
        </nav>
      </header>
      <main className="card">
        <Routes>
          <Route
            path="/"
            element={
              <div>
                <h2>Operations</h2>
                <p className="muted">
                  Foundation scaffold. Configuration and recovery tools land with later phases.
                </p>
              </div>
            }
          />
          <Route
            path="/offerings"
            element={
              <div>
                <h2>Offerings</h2>
                <p className="muted">Create, publish, and unpublish offerings here.</p>
              </div>
            }
          />
          <Route
            path="/reconciliation"
            element={
              <div>
                <h2>Reconciliation</h2>
                <p className="muted">Ambiguous and failed workflows will surface here.</p>
              </div>
            }
          />
        </Routes>
      </main>
    </div>
  );
}
