# Gateflow Execution Plan

**Aligned with master implementation prompt phases.**  
**Go module prefix:** `github.com/ghrushneshr25/gateflow`

---

## Phase 0 — Specification (DONE)

- Inspect repo and tools
- Consolidate PRD, architecture, GF-SCS-001
- ADRs 0001–0024
- Vertical slice VS-1 defined
- Evidence: `docs/execution/PHASE0.md`

**Exit:** Gate 0 pass → start Phase 1

---

## Phase 1 — Repository foundation

1. Root `go.work` + five service modules
2. Shared technical libs only (`libs/go`)
3. Makefile: fmt, lint, test, build, contract-validate
4. React apps: Vite + TS strict + Vitest
5. Docker Compose: Postgres, Redis, Kafka, obs stack
6. `.env.example`, Dockerfiles, migration dirs
7. CI workflow
8. Health-only service mains (no fake domain completeness)

**Verify:** `make build test`, compose config/up infra, frontend typecheck  
**Exit:** all Phase 1 acceptance criteria in PHASE0.md

---

## Phase 2 — First vertical slice (VS-1)

Real persistence + contracts for happy-path FIFO booking end-to-end.

**Verify:** VS1-01…VS1-10 tests green  
**Exit:** buyer can confirm one order; outbox event committed

---

## Phase 3 — Concurrency and invariants

Atomic inventory, allowance, idempotency, expiry, multi-instance budgets, outbox dedupe.

**Verify:** concurrency + race tests; zero oversell; zero limit bypass

---

## Phase 4 — Failure and recovery

Lost response, Redis fence, Kafka replay, late payment, compensation, operator recovery APIs, failure injection.

**Verify:** listed failure scenarios pass with durable correct state

---

## Phase 5 — Product polish and operations

Full buyer/operator UX, dashboards, runbooks, audit trails, a11y.

---

## Phase 6 — Performance and resilience

Benchmark harness, 30m soak, document measured results only.

---

## Phase 7 — Deployment maturity

Helm/K8s after correctness gates; independent deploy evidence.

---

## Working rules

- Small verifiable steps; no phase skip on failed gates
- Do not claim success without executed evidence
- Do not push unless authorized
- Do not weaken invariants for green tests
