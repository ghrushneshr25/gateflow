import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, HttpError } from "../api/client";

export function CoordinationPage() {
  const qc = useQueryClient();
  const [confirm, setConfirm] = useState<"fence" | "unfence" | null>(null);
  const [reason, setReason] = useState("operator console");
  const [msg, setMsg] = useState("");

  const q = useQuery({
    queryKey: ["coordination"],
    queryFn: () => api.getCoordination(),
    refetchInterval: 8_000,
  });

  const fence = useMutation({
    mutationFn: () => api.fence(reason || "operator console"),
    onSuccess: (c) => {
      setMsg(`Fenced · epoch ${c.epoch} · mode ${c.mode}`);
      setConfirm(null);
      void qc.invalidateQueries({ queryKey: ["coordination"] });
    },
    onError: onErr,
  });

  const unfence = useMutation({
    mutationFn: () => api.unfence(),
    onSuccess: (c) => {
      setMsg(`Unfenced · epoch ${c.epoch} · mode ${c.mode}`);
      setConfirm(null);
      void qc.invalidateQueries({ queryKey: ["coordination"] });
    },
    onError: onErr,
  });

  function onErr(err: unknown) {
    if (err instanceof HttpError) {
      if (err.status === 0) {
        setMsg(`Network/timeout: ${err.message}. Refresh to learn authoritative mode.`);
      } else if (err.status === 401) {
        setMsg("401 Unauthorized — operator JWT required.");
      } else if (err.status === 403) {
        setMsg("403 Forbidden — need ops:admission:fence (read uses ops:admission:read).");
      } else {
        setMsg(`Error ${err.status}: ${err.message}`);
      }
    } else {
      setMsg("Unexpected error");
    }
  }

  const c = q.data;
  const busy = fence.isPending || unfence.isPending;

  return (
    <div>
      <div className="page-head">
        <h2>Admission coordination</h2>
        <button type="button" className="secondary" onClick={() => void q.refetch()} disabled={q.isFetching}>
          Refresh
        </button>
      </div>
      <p className="muted">
        Backend PG fence is authoritative. Fencing blocks new grants; existing entitlement retries
        follow server rules. Epoch only changes on successful fence/unfence responses.
      </p>

      {q.isLoading && <p role="status">Loading…</p>}
      {q.isError && (
        <p className="error" role="alert">
          {q.error instanceof HttpError
            ? `${q.error.status || "net"}: ${q.error.message}`
            : "Failed to load"}
        </p>
      )}
      {msg && (
        <p className={fence.isError || unfence.isError ? "error" : "info"} role="status">
          {msg}
        </p>
      )}

      {c && (
        <div className="detail card" aria-live="polite">
          <dl className="kv">
            <dt>Mode</dt>
            <dd className={c.mode === "FENCED" ? "warn" : "ok"}>{c.mode}</dd>
            <dt>Epoch</dt>
            <dd>{c.epoch}</dd>
            <dt>Reason</dt>
            <dd>{c.fence_reason ?? "—"}</dd>
            <dt>Fenced at / by</dt>
            <dd>
              {c.fenced_at ?? "—"} / {c.fenced_by ?? "—"}
            </dd>
            <dt>Updated</dt>
            <dd>{c.updated_at}</dd>
          </dl>
          <div className="auth-row">
            <button type="button" disabled={busy || c.mode === "FENCED"} onClick={() => setConfirm("fence")}>
              Fence…
            </button>
            <button
              type="button"
              className="secondary"
              disabled={busy || c.mode === "ACTIVE"}
              onClick={() => setConfirm("unfence")}
            >
              Unfence…
            </button>
          </div>
        </div>
      )}

      {confirm === "fence" && (
        <div className="modal" role="dialog" aria-modal="true" aria-labelledby="fence-title">
          <div className="modal-card card">
            <h3 id="fence-title">Confirm fence</h3>
            <p>New admission grants will be blocked until an authorized unfence.</p>
            <label htmlFor="fence-reason">Reason</label>
            <input
              id="fence-reason"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              disabled={busy}
            />
            <div className="auth-row">
              <button type="button" disabled={busy} onClick={() => fence.mutate()}>
                {fence.isPending ? "Fencing…" : "Confirm fence"}
              </button>
              <button type="button" className="secondary" disabled={busy} onClick={() => setConfirm(null)}>
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}

      {confirm === "unfence" && (
        <div className="modal" role="dialog" aria-modal="true" aria-labelledby="unfence-title">
          <div className="modal-card card">
            <h3 id="unfence-title">Confirm unfence</h3>
            <p>Resume grant issuance. This never happens automatically on process restart.</p>
            <div className="auth-row">
              <button type="button" disabled={busy} onClick={() => unfence.mutate()}>
                {unfence.isPending ? "Unfencing…" : "Confirm unfence"}
              </button>
              <button type="button" className="secondary" disabled={busy} onClick={() => setConfirm(null)}>
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
