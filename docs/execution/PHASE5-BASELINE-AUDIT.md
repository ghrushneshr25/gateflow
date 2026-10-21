# Phase 5 — Baseline Audit and Implementation Plan

**Document ID:** GF-P5-AUDIT-001  
**Date:** 2026-10-20  
**Type:** Read-only audit and planning (no application code changes)  
**Repository state:** HEAD `dad01e5` (`feat(phase4): …`), `main` ahead of `origin/main` by 12; working tree clean at audit start  
**References:** `docs/product/PRD.md`, `docs/contracts/GF-SCS-001.md`, `docs/adr/0020`, `docs/adr/0021`, `docs/execution/PLAN.md`, `docs/execution/PHASE4-CLOSURE-AUDIT.md`, `AGENTS.md`

---

## A. Executive assessment

### Product readiness

Gateflow is a **strong backend correctness and recovery platform** (Phases 0–4) with a **thin product shell**. Buyers cannot complete a real booking in the UI. Operators cannot run sales or recovery from the console. Observability is mostly compose scaffolding plus a few admin JSON counters.

| Layer | Readiness |
|---|---|
| Durable booking mechanics (admission, inventory, order, payment, outbox, fence, crash recovery) | **High** (Phase 4 closed with documented residuals) |
| Cryptographic identity + consistent authz on all mutators | **Low** — privileged admin islands exist; business S2S/catalog surfaces largely open |
| Buyer Web | **Scaffold + two read-only lists** |
| Operator Console | **Navigation scaffold only** |
| OTel / dashboards / alerts / runbooks | **Documented + optional Docker profile; not a working product surface** |

### Most important buyer-facing gaps

1. No UI path: offering detail → queue → grant → checkout → order detail / pending payment.  
2. Identity is hardcoded `buyer_web_demo` via spoofable `X-Buyer-ID` (no buyer JWT).  
3. Dead API helper `joinAndGrant` never wired; marketing copy overstates capability.  
4. No mapped business errors (sold out, expired entitlement, limits, ambiguous payment).

### Most important operator-facing gaps

1. Operator Console has **zero** API integration (no JWT, no fetches, no mutations).  
2. Backend already exposes valuable ops APIs (reconciliation, outbox quarantine/replay, coordination fence, budgets, compensations) that the UI does not use.  
3. No audit trail UX for privileged actions (store-level actor fields exist in places; no console history).  
4. Sale catalog admin APIs exist but are **unauthenticated** — unsafe to expose via UI without fixing P5.1 first.

### Most important security gaps

1. **Inventory entire surface public** (including confirm/release) — capacity theft risk.  
2. **Sale mutators and publish public**.  
3. **Admission grant/bind/consume and budget PUT public** (coordination fence is JWT-protected).  
4. **Order `GET /v1/orders/{id}` IDOR** if `X-Buyer-ID` omitted.  
5. **Buyer identity not cryptographic** (`X-Buyer-ID` trust).  
6. Default HMAC secret + service mint with `*` scope if misconfigured.  
7. Most S2S calls (except compensation) send no JWT; callees do not require it.

### Most important observability gaps

1. `telemetry.Init` is a placeholder; no OTel SDK.  
2. Compose obs profile does not wire traces→Tempo or logs→Loki; Grafana unprovisioned.  
3. No Prometheus `/metrics` from apps; Prometheus scrapes collector only.  
4. No dashboards, alerts, or written runbooks (index only).  
5. Logs lack bound `correlation_id`; causation IDs unused.

### CTO direction vs evidence

Initial expectation (P5.1 → P5.2 → P5.3 → P5.4) is **confirmed by evidence**. Security consistency is the correct first milestone: building operator/buyer UX on open mutators would amplify risk. Observability polish is valuable but secondary to authz and product journeys.

---

## B. Capability matrix

### B.1 P5.1 — Authentication and authorization

| Capability | Current state | Evidence | Gap | Risk / impact | Required change | Verification |
|---|---|---|---|---|---|---|
| Dev HMAC JWT adapter | **Implemented** | `libs/go/authn/devtoken.go`, ADR-0020 | Not production IdP (accepted) | Low if local-only | Keep; document non-prod | unit tests |
| Optional Bearer middleware | **Implemented (fail-open)** | `libs/go/authn/middleware.go` | Invalid token ignored | Med | Document; optional fail-closed for privileged mux | unit |
| Reject `X-Ops-Role` spoof | **Implemented** | order/admission `auth_test.go` | — | — | Preserve | auth tests |
| Order admin recon / outbox | **Implemented JWT** | `order/.../server.go`, `cmd/server/main.go` | — | — | Preserve | auth + live 401/200 |
| Admission coordination fence | **Implemented JWT** | `admission/.../server.go` | — | — | Preserve | auth tests |
| Payment compensations / callbacks | **Implemented JWT (loose scopes)** | `payment-simulator/.../server.go` | Scope OR across families | High | Tighten scopes | auth tests |
| Order → payment compensation S2S JWT | **Implemented** | `http_downstream.go`, auto-mint | Mint includes `*` | High | Least-privilege mint | integration |
| Sale authz | **Missing** | sale main: correlation only; all routes public | C4 | Critical | Middleware + ops scopes on write/publish | matrix tests |
| Inventory authz | **Missing** | inventory main: no authn; all public | C1 | Critical | Service JWT on mutators | matrix tests |
| Admission bind/consume/grant | **Public** | `admission/.../server.go` | C2 | Critical | Service JWT | matrix tests |
| Admission budget PUT | **Public** | same | C5 | Critical | Ops JWT | matrix tests |
| Buyer principal | **Header spoof** | `X-Buyer-ID` / body buyer | H1 | High | Buyer JWT `sub` (or signed session) | e2e + negative |
| Order get ownership | **Partial / IDOR** | getOrder: buyer check only if header set | C3 | Critical | Require buyer JWT or ops; never open | security test |
| Payment create/lookup/resolve-due | **Public** | payment server | C6 | Critical | Service JWT on create; ops on resolve-due | tests |
| Frontend auth | **None** | buyer/operator apps | — | High with UI growth | Token storage + attach Bearer | UI + API tests |
| Audit of privileged actions | **Partial** | fence/replay store actor subject | No operator-visible audit trail | Med | Append-only audit or structured logs | recon UI later |

**Endpoint scorecard (summary):**

| Service | authn.Middleware | Buyer | Ops JWT | S2S in | Notes |
|---|---|---|---|---|---|
| sale | No | N/A | None | N/A | All catalog mutators open |
| admission | Yes | X-Buyer spoof | coordination only | bind/consume/grant open | budgets open |
| inventory | No | N/A | None | all open | confirm/release open |
| order | Yes | X-Buyer spoof + IDOR | admin/outbox OK | N/A | checkout header identity |
| payment | Yes | N/A | comp/callback | create open | scope crossover |

### B.2 P5.2 — Operator Console

| Capability | Current state | Evidence | Gap | Risk / impact | Required change | Verification |
|---|---|---|---|---|---|---|
| Shell + routes | **Scaffold** | `apps/operator-console/src/App.tsx` | — | Low | Keep shell | smoke test |
| API client / JWT | **Missing** | no `src/api/` | Cannot call backends | High | Client + token config | unit |
| Offerings CRUD/publish | **UI missing**; **API public** | placeholder page; sale APIs exist | Unsafe until P5.1 | Critical if wired early | Wire after sale authz | e2e |
| Queue / grant inspect | **Missing** | no pages | Ops blind | High | Read APIs + UI | e2e |
| Reconciliation list | **API exists**; **UI scaffold** | `GET /v1/admin/reconciliation` | No UI | High | List + drill-down + reconcile action | e2e + auth |
| Outbox quarantine/replay | **API exists**; **UI missing** | order admin outbox routes | No UI | High | List + replay with confirm | e2e + auth |
| Coordination fence | **API exists**; **UI missing** | admission admin coordination | No UI | High | Status + fence/unfence + confirm | e2e + auth |
| Payment/compensation | **API exists**; **UI missing** | payment compensations | No UI | High | Inspect + authorized retry | e2e |
| Budgets | **API exists (open)** | admission budgets | Must auth first | Critical | Authz then UI | tests |
| Success/failure feedback | **Missing** | static copy | — | Med | Toasts/banners; idempotent POSTs | UI tests |
| Audit history view | **Missing** | — | — | Med | After audit store or log query | manual |

**Reuse first (do not rebuild backends):** order admin recon/outbox; admission coordination (+ budgets after authz); payment compensation GET/retry; sale write/publish after authz.

### B.3 P5.3 — Buyer experience

| Capability | Current state | Evidence | Gap | Risk / impact | Required change | Verification |
|---|---|---|---|---|---|---|
| List published offerings | **Partial functional** | `OfferingsPage`, `listOfferings` | No detail/sessions | Med | Detail + sessions | e2e |
| Orders list | **Partial functional** | `OrdersPage`, hardcoded buyer | Wrong identity model | High | Buyer JWT + own orders | e2e |
| Queue join | **Client stub unused** | `joinAndGrant` | No UI | High | Wire + status | e2e |
| Grant / entitlement | **Stub unused** | same | No timer/status | High | Show entitlement window | e2e |
| Checkout | **Missing** | — | Core journey broken | Critical | Checkout form + Idempotency-Key | e2e + vs1 alignment |
| Order detail / status | **Missing** | list only | Ambiguous payment UX gone | High | Detail + poll/reconcile | e2e |
| Pending/ambiguous payment | **Missing** | — | PRD explicit uncertainty | High | Status copy + refresh | e2e |
| Refresh/resume | **Partial** | poll lists | No checkout resume | High | Idempotent checkout key in storage | e2e |
| Dup submit protection | **Missing** | no mutations | Double charge risk if added naively | Critical | Disable + stable idem key | tests |
| a11y / empty / loading | **Partial on lists** | App.tsx | — | Low–Med | Expand with flows | a11y checks |
| Tests | **Smoke h1 only** | `App.test.tsx` PASS | No journey tests | Med | Component + e2e | vitest + script |

**Smallest coherent vertical slice (post P5.1.x buyer identity):**  
Discover published offering → join queue → receive grant → checkout with idempotency key → see order status (CONFIRMED or explicit RECONCILIATION/PENDING) → refresh preserves identity and order view.

### B.4 P5.4 — Observability

| Capability | Current state | Evidence | Gap | Risk / impact | Required change | Verification |
|---|---|---|---|---|---|---|
| Correlation HTTP header | **Implemented** | `libs/go/httpx` | No context/slog bind | Med | Context + log attr | unit |
| Structured JSON logs | **Implemented** | `libs/go/logging` | No corr; no Loki ship | Med | Enrich + optional ship | manual |
| telemetry.Init | **Placeholder** | `libs/go/telemetry/telemetry.go` | No OTel | High vs PRD | OTel SDK or defer with honesty | build |
| Outbox admin metrics JSON | **Implemented** | order admin | Not Prometheus | Med | Keep; optional export | curl JWT |
| Coordination metrics JSON | **Implemented** | admission admin | Not Prometheus | Med | Keep | curl JWT |
| Compose obs profile | **Compose-only** | docker-compose `obs` | Broken pipelines | High if demoed | Wire Tempo/Loki/Grafana DS | compose smoke |
| Prometheus scrape apps | **Missing** | scrapes collector only | Empty useful series | High | `/metrics` or OTLP | prom query |
| Traces | **Missing** | — | FR-OBS-004 | High | HTTP+S2S+Kafka spans | Tempo UI |
| Dashboards | **None** | no grafana provisioning | Phase 5 | High | 3 starter dashboards | screenshot |
| Alerts | **None** | — | Phase 5 | Med | Few actionable rules + runbook link | alert test |
| Runbooks | **Index only** | `docs/runbooks/README.md` | All Planned | High | Write 4–6 real runbooks tied to metrics/APIs | review |
| Cardinality controls | **Vacuous** | ADR-0021 | No real metrics yet | — | Enforce when adding series | review |

### B.5 Phase 4 residuals (carry forward — not resolved)

| Residual | Treatment in Phase 5 |
|---|---|
| Natural inventory lease-expiry multi-service e2e | **Open** — optional P5.x or later; do not claim solved |
| Full Docker Kafka cold-start recovery | **Open** |
| Broader process-kill coverage (non-order) | **Open** |
| Redis on grant path | **Deferred** until measured contention; never on grant authority without ADR |
| Local commits not pushed | Informational — push only when authorized |

---

## C. Prioritized implementation plan

### Guiding principles

- **Security before UI that mutates.**  
- **Reuse Phase 4 APIs**; do not reimplement recovery backends.  
- **Small independently verifiable milestones.**  
- **Preserve Phase 4 invariants** (inventory, limits, payment≠confirm, outbox dedupe, fence, Redis off grant path).  
- **No new IdP** unless audit proves HMAC adapter insufficient for local product goals (it is sufficient for P5 with least-privilege and fail-closed mutators).

### Recommended order

```
P5.1.0  Authz hard-close (S2S + sale/inventory + IDOR + budgets)     ← FIRST
P5.1.1  Buyer JWT principal (replace bare X-Buyer-ID for product paths)
P5.2.0  Operator Console: JWT client + recon + outbox + fence
P5.2.1  Operator Console: sale admin + budgets (after 5.1.0)
P5.3.0  Buyer vertical slice (discover → grant → checkout → status)
P5.3.1  Buyer polish (pending payment, errors, a11y, resume)
P5.4.0  Correlation-in-logs + compose obs wiring + 2 runbooks
P5.4.1  Minimal RED metrics + 1 dashboard + 3 alerts
P5.4.2  Traces (HTTP/S2S) + remaining runbooks
```

Optional interleaved: natural-lease e2e / Docker Kafka recovery as **P5.x residual burn-down** only if scheduled explicitly—not required to start P5.1.

---

### Milestone P5.1.0 — Authorization hard-close (FIRST)

**Scope**

- Wire `authn.Middleware` on sale + inventory.  
- Require service JWT (+ scopes) on: inventory pools/reservations mutators; admission bind/consume/grant; payment create (and tighten callback vs recover scopes).  
- Require operator JWT on: sale write/publish; admission budget PUT/GET.  
- Fix order `GET /v1/orders/{id}`: require buyer ownership **or** ops scope; never return body with empty principal.  
- Order S2S: attach service token to inventory/admission/payment create (not only compensation).  
- Least-privilege service mint (drop blanket `*` from default mint or restrict to explicit scopes).  
- Refuse default secret in `ENVIRONMENT=production` (or equivalent).  
- Auth matrix tests per service (positive + negative + spoof).

**Exclusions**

- OIDC/IdP; full buyer JWT UX (can remain header for tests behind flag until P5.1.1).  
- Operator/Buyer UI work.  
- OTel.

**Dependencies:** Phase 4 complete (done).

**Acceptance criteria**

- Unauthenticated calls to inventory confirm/release, sale publish, admission budget PUT, payment create → **401/403**.  
- Spoof `X-Ops-Role` still fails on existing admin routes.  
- Order get without principal → **401/403**, not 200.  
- VS1 and crash-recovery still pass with service tokens configured.  
- `make test`, relevant integration race, migrate green.

**Tests:** unit auth matrix; integration with minted tokens; vs1_e2e; test-crash-recovery.  
**Docs:** update security overview + change record; endpoint matrix appendix.  
**Exit:** CRITICAL gaps C1–C6 closed or explicitly waived with CTO sign-off (waiver not recommended).

---

### Milestone P5.1.1 — Buyer cryptographic identity

**Scope:** Buyer JWT (`principal_type=buyer`, `sub=buyer_id`); checkout/list/get/reconcile-as-owner use `sub`; deprecate trusting raw `X-Buyer-ID` on product paths (dev escape hatch optional).  
**Exclusions:** Social login, password IdP.  
**Acceptance:** Cannot act as another buyer by header alone; buyer-web uses token.  
**Tests:** negative cross-buyer access; frontend token attach.  
**Exit:** H1 closed for product HTTP paths.

---

### Milestone P5.2.0 — Operator Console recovery spine

**Scope:** API client + operator JWT config; pages for reconciliation list/detail+reconcile, outbox quarantine+replay, coordination status+fence/unfence; confirm dialogs; error banners; idempotent POSTs.  
**Exclusions:** Full sale designer; custom reporting.  
**Dependencies:** P5.1.0 (tokens and protected APIs).  
**Acceptance:** Operator with JWT can diagnose and execute recon/outbox/fence safely; without JWT all calls fail closed; repeated replay/reconcile safe.  
**Tests:** component tests with MSW or mocked fetch; optional scripted e2e.  
**Exit:** Ops can run Phase 4 recovery without curl.

---

### Milestone P5.2.1 — Operator sales and budgets

**Scope:** Create/publish offerings (and sessions/ticket types minimal), view budgets, set caps (authz from P5.1.0).  
**Dependencies:** P5.1.0, P5.2.0 shell.  
**Acceptance:** Publish flow works only with ops scopes; budgets no longer public.  
**Exit:** Operator can stand up a sale without raw SQL/curl.

---

### Milestone P5.3.0 — Buyer vertical slice

**Scope:** Offering detail → join queue → show grant → checkout (idempotency key) → order status page; wire real APIs; loading/error/empty; prevent double submit.  
**Dependencies:** P5.1.0 + P5.1.1 strongly recommended before public demo.  
**Acceptance:** One buyer completes CONFIRMED path on published sale; refresh keeps status; backend invariants untouched.  
**Tests:** vitest flow tests; extend vs1 or buyer e2e script.  
**Exit:** PRD happy path visible in UI.

---

### Milestone P5.3.1 — Buyer recoverable outcomes

**Scope:** PENDING/RECONCILIATION_REQUIRED copy; expired entitlement; sold out; network retry; order history polish; basic a11y.  
**Dependencies:** P5.3.0.  
**Acceptance:** Explicit non-success states; no silent failures.  
**Exit:** Buyer understands uncertainty without operator intervention for happy/pending paths.

---

### Milestone P5.4.0 — Observability foundation (honest wiring)

**Scope:** Bind correlation_id into request logs; fix obs compose (Tempo/Loki exporters or document removal); Grafana datasources; write runbooks: reconciliation backlog, outbox quarantine/replay, fence/unfence, late payment/compensation.  
**Exclusions:** Full trace coverage; Phase 6 benchmarks.  
**Acceptance:** Local `compose --profile obs` shows logs or traces from a demo request **or** docs clearly mark stack non-functional; runbooks reference real admin APIs/metrics.  
**Exit:** Operators have written procedures tied to real controls.

---

### Milestone P5.4.1 — Metrics and alerts (minimal useful set)

**Scope:** Low-cardinality RED metrics (service, route class, status class); export outbox backlog + quarantine + fence grants; one Grafana dashboard; three alerts (outbox age, quarantine >0, recon backlog).  
**Dependencies:** P5.4.0 wiring.  
**Acceptance:** ADR-0021 cardinality respected; alerts link to runbooks.  
**Exit:** Actionable signal without cardinality explosion.

---

### Milestone P5.4.2 — Distributed traces

**Scope:** OTel HTTP server/client instrumentation; propagate trace context on S2S; optional Kafka producer/consumer spans for outbox.  
**Dependencies:** P5.4.0.  
**Acceptance:** Single checkout visible as connected trace in Tempo.  
**Exit:** FR-OBS-004 minimally satisfied.

---

## D. CTO decisions

| Decision | Options | Recommendation | Rationale |
|---|---|---|---|
| D1. First implementation milestone | P5.1 / P5.2 / P5.3 / P5.4 | **P5.1.0 authz hard-close** | CRITICAL open mutators; UI would amplify risk; ADR-0020 already chose scoped principals |
| D2. New IdP now? | OIDC now vs keep dev HMAC | **Keep dev HMAC JWT** for Phase 5 | Sufficient for local product; ADR-0020 accepts OIDC later; cost high, gap is enforcement not crypto algorithm |
| D3. Buyer identity model | Header forever vs buyer JWT | **Buyer JWT in P5.1.1** | PRD/GF-SCS-001 require distinct buyer principal; header is spoofable |
| D4. S2S mesh | mTLS vs JWT scopes vs network trust | **Service JWT scopes on mutators** | Matches ADR-0020; compose has no mesh; fail-closed APIs |
| D5. Operator before buyer UI? | 5.2 before 5.3 vs reverse | **P5.2.0 after 5.1.0, before or parallel thin 5.3** | Recovery spine unblocks ops on Phase 4; buyer slice is product demo—both after authz |
| D6. OTel depth in P5 | Full FR-OBS vs foundation | **Foundation then minimal RED/traces** (5.4.0→5.4.2) | Avoid instrumenting everything; answer ops questions first |
| D7. Redis in P5 | Introduce on grant path | **No** | Phase 4 residual; PG fence sufficient until measured contention |
| D8. Waive inventory auth “for local demo”? | Waive vs enforce | **Enforce** | Confirm/release public is unacceptable even locally once multi-user |

---

## E. Regression risks (Phase 4 invariants to preserve)

Every Phase 5 milestone must not weaken:

| Invariant | Risk if broken by P5 |
|---|---|
| confirmed + active_reserved ≤ sellable | Open inventory confirm |
| purchase limits atomic | Checkout without holds / double submit |
| Payment success ≠ order confirm | Buyer UI “success” on payment alone |
| Expired/released seats never confirm | Buyer retry UX forcing confirm |
| Stable operation / event IDs | New clients generating new IDs on retry |
| Outbox dedupe / consumer at-most-once | Replay UI without idempotent backend (backend OK if used correctly) |
| Fence blocks grants; unfence manual | Operator auto-unfence on startup |
| Redis not grant authority | “Performance” Redis grant path |
| JWT not replaceable by role headers | UI sending X-Ops-Role |
| Compensation identity `comp_<payOp>` | New compensation APIs bypassing stable id |

**Mandatory regression gates per milestone:** `make test`, `make build`, relevant integration tests; after authz/S2S changes also `./scripts/vs1_e2e.sh` and `make test-crash-recovery` when environment allows.

---

## F. Residual risks and assumptions

### Facts (verified in tree)

- Phase 4 committed at `dad01e5`; not pushed (ahead 12).  
- Admin JWT islands work; spoof role headers rejected where tested.  
- Buyer/Operator apps are scaffolds (buyer: 2 lists; operator: static).  
- Telemetry placeholder; obs compose incomplete.  
- Frontend tests: shell h1 only (both PASS when run).

### Assumptions (not live-probed this audit)

- Live 401/200 on admin routes **not re-probed** (services not running during audit curl). Prior Phase 4 docs reported 401/200.  
- Default compose network exposure assumes local trust—authz still required.

### Unresolved product choices for later

- Whether buyer demo remains single shared buyer or multi-buyer local accounts.  
- Whether Operator Console embeds Grafana or links out.  
- Push timing of `dad01e5` to origin (out of audit scope).

---

## G. Commands and results (this audit)

| Command | Result |
|---|---|
| `git rev-parse --short HEAD` | `dad01e5` |
| `git status -sb` | `main...origin/main [ahead 12]`, clean before report |
| `git log --oneline -3` | phase4 commit present |
| Code inspection (services, apps, libs, compose, docs) | Completed via targeted reads/greps |
| `npm test -- --run` in `apps/buyer-web` | **PASS** (1 test, shell h1) |
| `npm test -- --run` in `apps/operator-console` | **PASS** (1 test, shell h1) |
| Live curl to :8081/:8083/:8084 | **NOT REACHABLE** (services down) |
| `make test` / crash suite | **NOT RUN** this audit (Phase 4 final verification already PASS on prior pass; no code changed) |

---

## H. Audit exit checklist

| Criterion | Met? |
|---|---|
| P5.1–P5.4 evidence-backed | **Yes** |
| Existing functionality not scheduled for needless rewrite | **Yes** (reuse admin APIs; keep HMAC adapter) |
| Next milestone clearly selected | **Yes — P5.1.0** |
| Acceptance criteria + tests concrete | **Yes** (§C) |
| Facts vs assumptions distinguished | **Yes** (§F) |
| Phase 4 residuals carried forward | **Yes** (§B.5) |
| Application code unmodified | **Yes** |
| No commit/push | **Yes** (only this report file added when written) |

---

## I. Recommended next action

**Authorize implementation of milestone P5.1.0 (Authorization hard-close)** as a dedicated coding task with the acceptance criteria in §C. Do not start Operator Console or Buyer journey work that performs privileged mutations until P5.1.0 exits.

After P5.1.0: prefer **P5.2.0** (operator recovery spine on existing APIs) and **P5.1.1** (buyer JWT) before a public buyer demo (**P5.3.0**).

---

## Appendix — Critical security gap IDs (quick ref)

| ID | Summary |
|---|---|
| C1 | Inventory API fully public (incl. confirm/release) |
| C2 | Admission grant/bind/consume public |
| C3 | Order get IDOR without buyer header |
| C4 | Sale mutators/publish public |
| C5 | Admission budget PUT public |
| C6 | Payment create/lookup/resolve-due public |
| C7 | Default JWT secret + overly broad service `*` mint |
