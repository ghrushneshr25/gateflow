import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FormEvent, useEffect, useState } from "react";
import { api, HttpError } from "../api/client";

export function BudgetsPage() {
  const qc = useQueryClient();
  const [scopeInput, setScopeInput] = useState("global");
  const [activeScope, setActiveScope] = useState("global");
  const [maxGrants, setMaxGrants] = useState(10000);
  const [maxActive, setMaxActive] = useState(1000);
  const [confirm, setConfirm] = useState(false);
  const [msg, setMsg] = useState("");

  const coordQ = useQuery({
    queryKey: ["coordination"],
    queryFn: () => api.getCoordination(),
    refetchInterval: 15_000,
  });

  const budgetQ = useQuery({
    queryKey: ["budget", activeScope],
    queryFn: () => api.getBudget(activeScope),
    retry: false,
  });

  useEffect(() => {
    if (budgetQ.data) {
      setMaxGrants(budgetQ.data.max_grants);
      setMaxActive(budgetQ.data.max_active_checkouts);
    }
  }, [budgetQ.data]);

  const put = useMutation({
    mutationFn: () =>
      api.putBudget(activeScope, {
        max_grants: maxGrants,
        max_active_checkouts: maxActive,
      }),
    onSuccess: (b) => {
      setConfirm(false);
      setMsg(
        `Updated ${b.scope_key}: max_grants=${b.max_grants}, max_active=${b.max_active_checkouts} (issued=${b.grants_issued}, active=${b.active_checkouts}; counters not reset)`,
      );
      void qc.invalidateQueries({ queryKey: ["budget", activeScope] });
    },
    onError: (err: unknown) => {
      if (err instanceof HttpError) {
        if (err.status === 0) {
          setMsg(`Network/timeout: ${err.message}. Refresh before retry.`);
        } else if (err.status === 401) {
          setMsg("401 — operator JWT required.");
        } else if (err.status === 403) {
          setMsg("403 — need ops:admission:budget.");
        } else if (err.status === 404) {
          setMsg("404 — scope not found. Use global or session:<session_id> after a grant path created it.");
        } else {
          setMsg(`${err.status}: ${err.message}`);
        }
      } else {
        setMsg("Unexpected error");
      }
    },
  });

  function loadScope(e: FormEvent) {
    e.preventDefault();
    const s = scopeInput.trim();
    if (!s) {
      setMsg("Scope required (global or session:<id>)");
      return;
    }
    setActiveScope(s.startsWith("session:") || s === "global" ? s : `session:${s}`);
    setMsg("");
  }

  const fenced = coordQ.data?.mode === "FENCED";
  const prev = budgetQ.data;

  return (
    <div>
      <div className="page-head">
        <h2>Admission budgets</h2>
        <button
          type="button"
          className="secondary"
          onClick={() => {
            void budgetQ.refetch();
            void coordQ.refetch();
          }}
          disabled={budgetQ.isFetching}
        >
          Refresh
        </button>
      </div>
      <p className="muted">
        Caps only: <code>PUT</code> updates <code>max_grants</code> / <code>max_active_checkouts</code>{" "}
        and does <strong>not</strong> reset issued/active counters. Fence is independent — budget
        updates do not grant seats or bypass a FENCED coordination mode.
      </p>

      {coordQ.data && (
        <p className={fenced ? "warn" : "info"} role="status">
          Coordination: <strong>{coordQ.data.mode}</strong> · epoch {coordQ.data.epoch}
          {fenced ? " — new grants blocked until unfence." : ""}
        </p>
      )}

      {msg && (
        <p className={/^[45]|^Network|403|401|404/.test(msg) ? "error" : "info"} role="status">
          {msg}
        </p>
      )}

      <form className="detail card" onSubmit={loadScope} aria-label="Load budget scope">
        <label htmlFor="budget-scope">Scope</label>
        <div className="auth-row">
          <input
            id="budget-scope"
            value={scopeInput}
            onChange={(e) => setScopeInput(e.target.value)}
            placeholder="global or session:<session_id>"
          />
          <button type="submit">Load</button>
        </div>
        <p className="muted small">Active scope: <code>{activeScope}</code></p>
      </form>

      {budgetQ.isLoading && <p role="status">Loading budget…</p>}
      {budgetQ.isError && (
        <p className="error" role="alert">
          {budgetQ.error instanceof HttpError
            ? `${budgetQ.error.status || "net"}: ${budgetQ.error.message}`
            : "Failed to load"}
        </p>
      )}

      {prev && (
        <section className="detail card" aria-labelledby="budget-current">
          <h3 id="budget-current">Current · {prev.scope_key}</h3>
          <dl className="kv">
            <dt>Max grants</dt>
            <dd>{prev.max_grants}</dd>
            <dt>Grants issued</dt>
            <dd>{prev.grants_issued}</dd>
            <dt>Max active checkouts</dt>
            <dd>{prev.max_active_checkouts}</dd>
            <dt>Active checkouts</dt>
            <dd>{prev.active_checkouts}</dd>
            <dt>Coordination epoch (snapshot)</dt>
            <dd>{prev.coordination_epoch}</dd>
          </dl>

          <h4>Update caps</h4>
          <label htmlFor="max-grants">Max grants</label>
          <input
            id="max-grants"
            type="number"
            min={0}
            value={maxGrants}
            onChange={(e) => setMaxGrants(Number(e.target.value))}
            disabled={put.isPending}
          />
          <label htmlFor="max-active">Max active checkouts</label>
          <input
            id="max-active"
            type="number"
            min={0}
            value={maxActive}
            onChange={(e) => setMaxActive(Number(e.target.value))}
            disabled={put.isPending}
          />
          <div className="auth-row">
            <button
              type="button"
              disabled={put.isPending || (maxGrants === prev.max_grants && maxActive === prev.max_active_checkouts)}
              onClick={() => setConfirm(true)}
            >
              Review update…
            </button>
          </div>
        </section>
      )}

      {confirm && prev && (
        <div className="modal" role="dialog" aria-modal="true" aria-labelledby="bud-title">
          <div className="modal-card card">
            <h3 id="bud-title">Confirm budget update</h3>
            <p>
              Scope <code>{activeScope}</code>
            </p>
            <ul className="muted">
              <li>
                max_grants: {prev.max_grants} → <strong>{maxGrants}</strong>
              </li>
              <li>
                max_active_checkouts: {prev.max_active_checkouts} → <strong>{maxActive}</strong>
              </li>
              <li>
                issued/active counters stay {prev.grants_issued}/{prev.active_checkouts}
              </li>
            </ul>
            {fenced && (
              <p className="warn">
                Coordination is FENCED. Caps still update, but new grants remain blocked until
                unfence.
              </p>
            )}
            <div className="auth-row">
              <button type="button" disabled={put.isPending} onClick={() => put.mutate()}>
                {put.isPending ? "Saving…" : "Confirm update"}
              </button>
              <button type="button" className="secondary" disabled={put.isPending} onClick={() => setConfirm(false)}>
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
