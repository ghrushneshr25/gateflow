import { useMutation, useQueryClient } from "@tanstack/react-query";
import { FormEvent, useState } from "react";
import { Link } from "react-router-dom";
import { api, HttpError } from "../api/client";

function isoLocal(hoursFromNow: number) {
  return new Date(Date.now() + hoursFromNow * 3600_000).toISOString();
}

/**
 * One-shot wizard: draft offering → session → ticket type → publish (inventory pools).
 * End-to-end path for admins to put a bookable event on the buyer site.
 */
export function CreateEventPage() {
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [category, setCategory] = useState("concert");
  const [description, setDescription] = useState("");
  const [ttName, setTtName] = useState("GA");
  const [capacity, setCapacity] = useState(50);
  const [priceCents, setPriceCents] = useState(5000);
  const [maxPerBuyer, setMaxPerBuyer] = useState(4);
  const [log, setLog] = useState<string[]>([]);
  const [resultId, setResultId] = useState<string | null>(null);
  const [err, setErr] = useState("");

  const run = useMutation({
    mutationFn: async () => {
      const steps: string[] = [];
      const n = name.trim();
      if (!n) throw new Error("Event name is required");
      if (capacity < 1) throw new Error("Capacity must be at least 1");

      steps.push("1. Creating draft offering…");
      setLog([...steps]);
      const offering = await api.createOffering({
        name: n,
        description: description.trim() || `${n} — Gateflow lab event`,
        category: category.trim() || "general",
      });
      steps.push(`   ✓ offering ${offering.id} (${offering.status})`);
      setLog([...steps]);

      steps.push("2. Adding session (sale open, event in ~7 days)…");
      setLog([...steps]);
      const sess = await api.createSession(offering.id, {
        starts_at: isoLocal(24 * 7),
        ends_at: isoLocal(24 * 7 + 3),
        sale_opens_at: isoLocal(-1),
        sale_closes_at: isoLocal(24 * 6),
        max_per_buyer: maxPerBuyer,
        admission_strategy: "fifo",
      });
      steps.push(`   ✓ session ${sess.session.id}`);
      setLog([...steps]);

      steps.push("3. Adding ticket type + capacity…");
      setLog([...steps]);
      const tt = await api.createTicketType(sess.session.id, {
        name: ttName.trim() || "GA",
        capacity,
        price_cents: priceCents,
        currency: "USD",
      });
      steps.push(`   ✓ ticket ${tt.id} capacity=${tt.capacity}`);
      setLog([...steps]);

      steps.push("4. Publishing (creates inventory pool)…");
      setLog([...steps]);
      const pub = await api.publishOffering(offering.id);
      const pool =
        pub.ticket_types?.[0]?.inventory_pool_id ??
        tt.inventory_pool_id ??
        "—";
      steps.push(`   ✓ PUBLISHED · pool ${pool}`);
      setLog([...steps]);

      return { offeringId: offering.id, sessionId: sess.session.id, ticketTypeId: tt.id, pool };
    },
    onSuccess: (r) => {
      setResultId(r.offeringId);
      setErr("");
      void qc.invalidateQueries({ queryKey: ["offerings"] });
    },
    onError: (e: unknown) => {
      if (e instanceof HttpError) {
        if (e.status === 401) setErr("Sign in as operator first (button above).");
        else if (e.status === 403) setErr("Missing ops:sale:write or ops:sale:publish.");
        else setErr(`${e.status}: ${e.message}${e.code ? ` (${e.code})` : ""}`);
      } else if (e instanceof Error) setErr(e.message);
      else setErr("Unexpected error");
    },
  });

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    setResultId(null);
    setLog([]);
    setErr("");
    run.mutate();
  }

  return (
    <div>
      <div className="page-head">
        <h2>Create event</h2>
        <Link to="/sales" className="muted">
          Advanced sale tools →
        </Link>
      </div>
      <p className="muted">
        Publish a bookable event in one step. Buyers will see it under Offerings after publish.
      </p>

      <form className="detail card" onSubmit={onSubmit} aria-label="Create event wizard">
        <label htmlFor="ev-name">Event name</label>
        <input
          id="ev-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Friday Night Concert"
          required
          disabled={run.isPending}
        />
        <label htmlFor="ev-cat">Category</label>
        <input
          id="ev-cat"
          value={category}
          onChange={(e) => setCategory(e.target.value)}
          disabled={run.isPending}
        />
        <label htmlFor="ev-desc">Description (optional)</label>
        <input
          id="ev-desc"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          disabled={run.isPending}
        />
        <label htmlFor="ev-tt">Ticket name</label>
        <input id="ev-tt" value={ttName} onChange={(e) => setTtName(e.target.value)} disabled={run.isPending} />
        <label htmlFor="ev-cap">Capacity</label>
        <input
          id="ev-cap"
          type="number"
          min={1}
          value={capacity}
          onChange={(e) => setCapacity(Number(e.target.value))}
          disabled={run.isPending}
        />
        <label htmlFor="ev-price">Price (cents)</label>
        <input
          id="ev-price"
          type="number"
          min={0}
          value={priceCents}
          onChange={(e) => setPriceCents(Number(e.target.value))}
          disabled={run.isPending}
        />
        <label htmlFor="ev-max">Max per buyer</label>
        <input
          id="ev-max"
          type="number"
          min={1}
          value={maxPerBuyer}
          onChange={(e) => setMaxPerBuyer(Number(e.target.value))}
          disabled={run.isPending}
        />
        <div className="auth-row" style={{ marginTop: "1rem" }}>
          <button type="submit" disabled={run.isPending || !name.trim()}>
            {run.isPending ? "Creating & publishing…" : "Create & publish event"}
          </button>
        </div>
      </form>

      {err ? (
        <p className="error" role="alert">
          {err}
        </p>
      ) : null}

      {log.length > 0 ? (
        <pre className="card muted small" style={{ whiteSpace: "pre-wrap" }}>
          {log.join("\n")}
        </pre>
      ) : null}

      {resultId ? (
        <p className="ok" role="status">
          Live for buyers: offering <code>{resultId}</code>. Open{" "}
          <strong>Buyer Web</strong> → Offerings, sign in, book.
        </p>
      ) : null}
    </div>
  );
}
