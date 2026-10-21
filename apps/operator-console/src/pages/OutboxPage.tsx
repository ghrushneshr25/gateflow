import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, HttpError } from "../api/client";
import type { QuarantinedEvent } from "../api/types";

export function OutboxPage() {
  const qc = useQueryClient();
  const [selected, setSelected] = useState<QuarantinedEvent | null>(null);
  const [confirmId, setConfirmId] = useState<string | null>(null);
  const [msg, setMsg] = useState("");

  const q = useQuery({
    queryKey: ["outbox-quarantined"],
    queryFn: () => api.listQuarantined(),
    refetchInterval: 10_000,
  });

  const replay = useMutation({
    mutationFn: (eventId: string) => api.replayEvent(eventId),
    onMutate: () => setMsg("Submitting replay…"),
    onSuccess: (data) => {
      setMsg(
        `Replay accepted: ${data.result}. Same event_id is preserved; consumer dedupe applies on redelivery.`,
      );
      setConfirmId(null);
      setSelected(data.event);
      void qc.invalidateQueries({ queryKey: ["outbox-quarantined"] });
    },
    onError: (err: unknown) => {
      if (err instanceof HttpError) {
        if (err.status === 0) {
          setMsg(`Network/timeout: ${err.message}. Refresh before assuming success or failure.`);
        } else if (err.status === 401) {
          setMsg("401 Unauthorized — operator JWT required.");
        } else if (err.status === 403) {
          setMsg("403 Forbidden — need ops:outbox:replay (and read for list).");
        } else {
          setMsg(`Error ${err.status}: ${err.message}`);
        }
      } else {
        setMsg("Unexpected error");
      }
    },
  });

  const items = q.data?.items ?? [];

  return (
    <div>
      <div className="page-head">
        <h2>Outbox quarantine</h2>
        <button type="button" className="secondary" onClick={() => void q.refetch()} disabled={q.isFetching}>
          Refresh
        </button>
      </div>
      <p className="muted">
        Poisoned unpublished events. Replay clears quarantine for the same stable event_id; do not
        edit payloads in the browser.
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
        <p className={replay.isError ? "error" : "info"} role="status">
          {msg}
        </p>
      )}
      {!q.isLoading && !q.isError && items.length === 0 && (
        <p className="muted">No quarantined events.</p>
      )}

      {items.length > 0 && (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th scope="col">Event ID</th>
                <th scope="col">Aggregate</th>
                <th scope="col">Attempts</th>
                <th scope="col">Replays</th>
                <th scope="col">Error</th>
                <th scope="col">Actions</th>
              </tr>
            </thead>
            <tbody>
              {items.map((ev) => (
                <tr key={ev.event_id}>
                  <td>
                    <button type="button" className="linkish" onClick={() => setSelected(ev)}>
                      {ev.event_id}
                    </button>
                  </td>
                  <td>{ev.aggregate_id}</td>
                  <td>{ev.attempt_count}</td>
                  <td>{ev.replay_count}</td>
                  <td className="truncate">{ev.last_error ?? "—"}</td>
                  <td>
                    <button
                      type="button"
                      disabled={replay.isPending}
                      onClick={() => setConfirmId(ev.event_id)}
                    >
                      Replay…
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {confirmId && (
        <div className="modal" role="dialog" aria-modal="true" aria-labelledby="replay-title">
          <div className="modal-card card">
            <h3 id="replay-title">Confirm replay</h3>
            <p>
              Replay <code>{confirmId}</code>? The publisher will reclaim the same event_id. Duplicate
              delivery is expected to be safe via consumer dedupe.
            </p>
            <div className="auth-row">
              <button
                type="button"
                disabled={replay.isPending}
                onClick={() => replay.mutate(confirmId)}
              >
                {replay.isPending ? "Replaying…" : "Confirm replay"}
              </button>
              <button
                type="button"
                className="secondary"
                disabled={replay.isPending}
                onClick={() => setConfirmId(null)}
              >
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}

      {selected && (
        <aside className="detail card" aria-label="Event detail">
          <h3>Event detail</h3>
          <dl className="kv">
            <dt>Event ID</dt>
            <dd>{selected.event_id}</dd>
            <dt>Type</dt>
            <dd>{selected.event_type}</dd>
            <dt>Aggregate</dt>
            <dd>{selected.aggregate_id}</dd>
            <dt>Correlation</dt>
            <dd>{selected.correlation_id || "—"}</dd>
            <dt>Attempts / replays</dt>
            <dd>
              {selected.attempt_count} / {selected.replay_count}
            </dd>
            <dt>Last error</dt>
            <dd>{selected.last_error ?? "—"}</dd>
            <dt>Last replay</dt>
            <dd>
              {selected.last_replay_at ?? "—"} by {selected.last_replay_by ?? "—"}
            </dd>
            <dt>Created</dt>
            <dd>{selected.created_at}</dd>
          </dl>
        </aside>
      )}
    </div>
  );
}
