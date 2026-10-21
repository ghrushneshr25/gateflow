import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FormEvent, useMemo, useState } from "react";
import { api, HttpError } from "../api/client";
import type { Offering } from "../api/types";

function isoLocal(hoursFromNow: number) {
  const d = new Date(Date.now() + hoursFromNow * 3600_000);
  return d.toISOString();
}

export function SalesPage() {
  const qc = useQueryClient();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [msg, setMsg] = useState("");
  const [confirmPublish, setConfirmPublish] = useState(false);

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [category, setCategory] = useState("concert");

  const [ttName, setTtName] = useState("GA");
  const [capacity, setCapacity] = useState(100);
  const [priceCents, setPriceCents] = useState(5000);
  const [maxPerBuyer, setMaxPerBuyer] = useState(4);

  const listQ = useQuery({
    queryKey: ["offerings"],
    queryFn: () => api.listOfferings(),
  });

  const detailQ = useQuery({
    queryKey: ["offering", selectedId],
    enabled: Boolean(selectedId),
    queryFn: async () => {
      const id = selectedId!;
      const offering = await api.getOffering(id);
      const sessions = await api.listSessions(id);
      const ticketTypes =
        sessions.items[0] != null
          ? await api.listTicketTypes(sessions.items[0].id)
          : { items: [] };
      return { offering, sessions: sessions.items, ticketTypes: ticketTypes.items };
    },
  });

  const createOffering = useMutation({
    mutationFn: () =>
      api.createOffering({
        name: name.trim(),
        description: description.trim(),
        category: category.trim() || "general",
      }),
    onSuccess: (o) => {
      setMsg(`Created offering ${o.id} (${o.status})`);
      setName("");
      setDescription("");
      setSelectedId(o.id);
      void qc.invalidateQueries({ queryKey: ["offerings"] });
    },
    onError: onErr,
  });

  const createSession = useMutation({
    mutationFn: () => {
      if (!selectedId) throw new Error("select offering");
      return api.createSession(selectedId, {
        starts_at: isoLocal(24 * 7),
        ends_at: isoLocal(24 * 7 + 3),
        sale_opens_at: isoLocal(-1),
        sale_closes_at: isoLocal(24 * 6),
        max_per_buyer: maxPerBuyer,
        admission_strategy: "fifo",
      });
    },
    onSuccess: (data) => {
      setMsg(`Session ${data.session.id} created (max_per_buyer=${data.purchase_policy.max_per_buyer})`);
      void qc.invalidateQueries({ queryKey: ["offering", selectedId] });
    },
    onError: onErr,
  });

  const createTT = useMutation({
    mutationFn: () => {
      const sid = detailQ.data?.sessions[0]?.id;
      if (!sid) throw new Error("create a session first");
      if (capacity <= 0) throw new Error("capacity must be positive");
      return api.createTicketType(sid, {
        name: ttName.trim() || "GA",
        capacity,
        price_cents: priceCents,
        currency: "USD",
      });
    },
    onSuccess: (tt) => {
      setMsg(`Ticket type ${tt.id} capacity=${tt.capacity}`);
      void qc.invalidateQueries({ queryKey: ["offering", selectedId] });
    },
    onError: onErr,
  });

  const publish = useMutation({
    mutationFn: () => {
      if (!selectedId) throw new Error("select offering");
      return api.publishOffering(selectedId);
    },
    onSuccess: (data) => {
      setConfirmPublish(false);
      setMsg(`Published ${data.offering.id} → ${data.offering.status}`);
      void qc.invalidateQueries({ queryKey: ["offerings"] });
      void qc.invalidateQueries({ queryKey: ["offering", selectedId] });
    },
    onError: onErr,
  });

  function onErr(err: unknown) {
    if (err instanceof HttpError) {
      if (err.status === 0) {
        setMsg(`Network/timeout: ${err.message}. Refresh before retry.`);
      } else if (err.status === 401) {
        setMsg("401 — operator JWT required for sale mutations.");
      } else if (err.status === 403) {
        setMsg("403 — need ops:sale:write (and ops:sale:publish to publish).");
      } else {
        setMsg(`${err.status}: ${err.message}${err.code ? ` (${err.code})` : ""}`);
      }
    } else if (err instanceof Error) {
      setMsg(err.message);
    } else {
      setMsg("Unexpected error");
    }
  }

  const busy =
    createOffering.isPending ||
    createSession.isPending ||
    createTT.isPending ||
    publish.isPending;

  const selected: Offering | undefined = useMemo(
    () => listQ.data?.items.find((o) => o.id === selectedId) ?? detailQ.data?.offering,
    [listQ.data, detailQ.data, selectedId],
  );

  function onCreateOffering(e: FormEvent) {
    e.preventDefault();
    if (!name.trim()) {
      setMsg("Name is required");
      return;
    }
    createOffering.mutate();
  }

  return (
    <div>
      <div className="page-head">
        <h2>Sale administration</h2>
        <button type="button" className="secondary" onClick={() => void listQ.refetch()} disabled={listQ.isFetching}>
          Refresh list
        </button>
      </div>
      <p className="muted">
        Lifecycle: DRAFT → add session + ticket type → publish (creates inventory pools). Backend
        validates transitions; publish requires <code>ops:sale:publish</code>.
      </p>
      {msg && (
        <p className={/^[45]|^Network|403|401/.test(msg) ? "error" : "info"} role="status">
          {msg}
        </p>
      )}

      <section className="detail card" aria-labelledby="create-off-title">
        <h3 id="create-off-title">Create offering</h3>
        <form onSubmit={onCreateOffering}>
          <label htmlFor="off-name">Name</label>
          <input id="off-name" value={name} onChange={(e) => setName(e.target.value)} required disabled={busy} />
          <label htmlFor="off-cat">Category</label>
          <input id="off-cat" value={category} onChange={(e) => setCategory(e.target.value)} disabled={busy} />
          <label htmlFor="off-desc">Description</label>
          <input id="off-desc" value={description} onChange={(e) => setDescription(e.target.value)} disabled={busy} />
          <div className="auth-row">
            <button type="submit" disabled={busy || !name.trim()}>
              {createOffering.isPending ? "Creating…" : "Create draft"}
            </button>
          </div>
        </form>
      </section>

      {listQ.isLoading && <p role="status">Loading offerings…</p>}
      {listQ.isError && (
        <p className="error" role="alert">
          {listQ.error instanceof HttpError
            ? `${listQ.error.status || "net"}: ${listQ.error.message}`
            : "Failed to load"}
        </p>
      )}
      {!listQ.isLoading && (listQ.data?.items.length ?? 0) === 0 && (
        <p className="muted">No offerings yet.</p>
      )}

      {(listQ.data?.items.length ?? 0) > 0 && (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th scope="col">ID</th>
                <th scope="col">Name</th>
                <th scope="col">Status</th>
                <th scope="col">Category</th>
              </tr>
            </thead>
            <tbody>
              {listQ.data!.items.map((o) => (
                <tr key={o.id} className={selectedId === o.id ? "selected" : undefined}>
                  <td>
                    <button type="button" className="linkish" onClick={() => setSelectedId(o.id)}>
                      {o.id}
                    </button>
                  </td>
                  <td>{o.name}</td>
                  <td className={o.status === "PUBLISHED" ? "ok" : "warn"}>{o.status}</td>
                  <td>{o.category}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {selectedId && (
        <aside className="detail card" aria-label="Offering detail">
          <h3>
            Detail · {selected?.name ?? selectedId}{" "}
            <span className={selected?.status === "PUBLISHED" ? "ok" : "warn"}>
              {selected?.status ?? "…"}
            </span>
          </h3>
          {detailQ.isLoading && <p role="status">Loading detail…</p>}
          {detailQ.data && (
            <>
              <dl className="kv">
                <dt>Offering ID</dt>
                <dd>{detailQ.data.offering.id}</dd>
                <dt>Sessions</dt>
                <dd>
                  {detailQ.data.sessions.length === 0
                    ? "none"
                    : detailQ.data.sessions.map((s) => s.id).join(", ")}
                </dd>
                <dt>Ticket types</dt>
                <dd>
                  {detailQ.data.ticketTypes.length === 0
                    ? "none"
                    : detailQ.data.ticketTypes
                        .map((t) => `${t.name} cap=${t.capacity} pool=${t.inventory_pool_id ?? "—"}`)
                        .join("; ")}
                </dd>
              </dl>

              {selected?.status === "DRAFT" && (
                <>
                  <h4>Add session (defaults: opens now-ish, event in 7d)</h4>
                  <label htmlFor="max-buyer">Max per buyer</label>
                  <input
                    id="max-buyer"
                    type="number"
                    min={1}
                    value={maxPerBuyer}
                    onChange={(e) => setMaxPerBuyer(Number(e.target.value))}
                    disabled={busy}
                  />
                  <div className="auth-row">
                    <button type="button" disabled={busy} onClick={() => createSession.mutate()}>
                      {createSession.isPending ? "Adding session…" : "Add session"}
                    </button>
                  </div>

                  <h4>Add ticket type to first session</h4>
                  <label htmlFor="tt-name">Name</label>
                  <input id="tt-name" value={ttName} onChange={(e) => setTtName(e.target.value)} disabled={busy} />
                  <label htmlFor="tt-cap">Capacity</label>
                  <input
                    id="tt-cap"
                    type="number"
                    min={1}
                    value={capacity}
                    onChange={(e) => setCapacity(Number(e.target.value))}
                    disabled={busy}
                  />
                  <label htmlFor="tt-price">Price cents</label>
                  <input
                    id="tt-price"
                    type="number"
                    min={0}
                    value={priceCents}
                    onChange={(e) => setPriceCents(Number(e.target.value))}
                    disabled={busy}
                  />
                  <div className="auth-row">
                    <button
                      type="button"
                      disabled={busy || !detailQ.data.sessions[0]}
                      onClick={() => createTT.mutate()}
                    >
                      {createTT.isPending ? "Adding…" : "Add ticket type"}
                    </button>
                  </div>

                  <div className="auth-row">
                    <button
                      type="button"
                      disabled={
                        busy ||
                        detailQ.data.sessions.length === 0 ||
                        detailQ.data.ticketTypes.length === 0
                      }
                      onClick={() => setConfirmPublish(true)}
                    >
                      Publish…
                    </button>
                  </div>
                </>
              )}
            </>
          )}
        </aside>
      )}

      {confirmPublish && selectedId && (
        <div className="modal" role="dialog" aria-modal="true" aria-labelledby="pub-title">
          <div className="modal-card card">
            <h3 id="pub-title">Confirm publish</h3>
            <p>
              Publish <code>{selectedId}</code>? This creates inventory pools and sets status to
              PUBLISHED. Backend rejects invalid drafts.
            </p>
            <div className="auth-row">
              <button type="button" disabled={busy} onClick={() => publish.mutate()}>
                {publish.isPending ? "Publishing…" : "Confirm publish"}
              </button>
              <button type="button" className="secondary" disabled={busy} onClick={() => setConfirmPublish(false)}>
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
