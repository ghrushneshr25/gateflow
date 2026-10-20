# Gateflow Test Strategy

**Status:** Accepted baseline (Phase 0)

## Layers

| Layer | What | Dependencies |
|---|---|---|
| Unit | Policies, state transitions, strategies, error classification | None / fakes |
| DB integration | Atomic SQL, constraints, outbox txn, migrations | Real PostgreSQL |
| API contract | OpenAPI request/response/error shapes | Schema files |
| Event schema | Envelope + payload validation | JSON Schema |
| Concurrency / race | Last-unit inventory, purchase limits, grant races | Real PG (+ Redis as needed) |
| Failure injection | Timeouts, duplicates, Redis/Kafka down, late payment | Compose + harness |
| E2E | Buyer and operator journeys | Full stack |
| Frontend | Components, forms, query states, Playwright journeys | Vitest/RTL/Playwright |
| Benchmark | Latency/throughput + **correctness assertions** | Documented env |

## Hard correctness suite (must stay green)

1. Competing reservations for last inventory units  
2. Concurrent checkouts exceeding buyer limit  
3. Same idempotency key replay  
4. Conflicting idempotency key  
5. Lost response after DB commit  
6. Duplicate Kafka events  
7. Consumer restart/replay  
8. Redis failure during admission  
9. Stale worker after recovery  
10. Expiry vs confirmation race  
11. Payment success after reservation expiry  
12. Compensation failure + manual retry  
13. Policy migration with active checkouts  
14. Process restart during reconciliation  

## Rules

- Assert **persisted authoritative state**, not only HTTP status.
- Mocks OK for unit isolation; distributed claims require real deps.
- Race-sensitive Go packages: `go test -race`.
- Do not skip tests to advance phases.
