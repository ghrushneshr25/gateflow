# Phase 4.5 Report — Process-Kill Crash Recovery

## Goal

Prove durable recovery after **real process death** at critical windows, against the same Postgres/Kafka dependencies — not mock-only unit paths.

## Harness

| Piece | Location |
|---|---|
| Crash barriers | `libs/go/testhooks` (`GATEFLOW_TEST_HOOKS=1`) |
| Process control | `services/order/tests/crash/proc.go` |
| Scenarios | `services/order/tests/crash/crash_test.go` |
| Make target | `make test-crash-recovery` |

Env (test-only):

- `GATEFLOW_TEST_HOOKS=1`
- `GATEFLOW_HOOK_DIR=<tmpdir>`
- `GATEFLOW_CRASH_BOUNDARY=<name>`
- `GATEFLOW_CRASH_EVENT_FILTER=<event_id substring>` (optional)
- `GATEFLOW_OUTBOX_PUBLISHER=0` / `GATEFLOW_OUTBOX_CONSUMER=0`
- `GATEFLOW_KAFKA_GROUP=<id>` (isolate consumer offsets)

Boundaries instrumented:

| Boundary | Where |
|---|---|
| `after_payment_success` | order HTTP downstream CreatePayment success return |
| `after_compensation_created` | driver INV_EXPIRED path after EnsureCompensation, before order status/comp-id persist |
| `after_kafka_ack` | outbox publisher after Kafka write, before `published_at` |
| `before_consumer_db` / `after_consumer_db` | consumer around `ApplyOrderConfirmedEffect` vs offset commit |

Supporting production-path fix for Scenario B:

- Order mints/uses service JWT (`GATEFLOW_SERVICE_TOKEN` or auto-mint via `GATEFLOW_DEV_JWT_SECRET`) on compensation S2S calls.
- `ORDER_LEASE_SECONDS` env override for lease (tests may still force RELEASED via inventory API).

## Scenarios executed (P4.5.1)

| ID | Scenario | Result |
|---|---|---|
| A | Payment success, response lost (process exit), resume same `payment_operation_id` → CONFIRMED | **PASS** |
| B | Payment succeeded + reservation released (INV_EXPIRED) → compensation → kill → restart; never confirm; stable pay/comp ops | **PASS** |
| C | Kafka ack before `published_at`; kill; restart reclaim + mark + consume once | **PASS** |
| D | Consumer DB effect before offset commit; kill; restart redelivery; effect count stays 1 | **PASS** |
| E | Repeated kill/restart during payment-success window; single payment op; CONFIRMED | **PASS** |

### Scenario B evidence (detail)

| Step | Evidence |
|---|---|
| Crash boundary | `after_compensation_created` (filter = payment_operation_id) |
| Pre-kill durable | Payment `SUCCEEDED`; reservation `RELEASED`; compensation row created at payment (`comp_<payOp>`) |
| Process death | Barrier file + process exit/SIGKILL via harness |
| Restart | Same Postgres + payment-simulator; clean hooks |
| Post-recovery | Order `RECONCILIATION_REQUIRED`; `payment_operation_id` unchanged; `compensation_operation_id` = same `comp_*`; never `CONFIRMED`; reservation not ACTIVE/CONFIRMED; compensation count = 1; second reconcile idempotent |

Command:

```bash
export DATABASE_URL='postgres://gateflow:gateflow@localhost:5432/gateflow?sslmode=disable'
export KAFKA_BROKERS=localhost:9092
make build
make test-crash-recovery
```

Last full crash suite (A–E): **PASS** (~68s) on 2026-10-20.

## Broader regression (P4.5.1)

| Gate | Result |
|---|---|
| `make test-crash-recovery` (A–E) | **PASS** |
| `make test` | **PASS** |
| `make build` | **PASS** |
| `./scripts/vs1_e2e.sh` | **VS1_E2E_PASS** |
| `make test-integration-race` (order/inventory/admission) | **PASS** |
| `go test -race -tags=integration ./services/payment-simulator/tests/...` | **PASS** |

## Invariants preserved

- Payment success alone never confirms an order.
- Expired/released reservations never revive; order never confirms on that path.
- Stable `payment_operation_id` and `comp_<payment_operation_id>` across kill/restart.
- Outbox `event_id` stable; republish after ack-before-mark is safe.
- Consumer effect at-most-once via `processed_events` / `order_confirmed_effects`.
- Redis not on grant path.
- Timeouts remain uncertainty (resume/reconcile), not auto-fail.

## Limitations / residual

1. **Scenario B vs natural lease expiry (explicit distinction)**  
   - **What B proves:** compensation crash/recovery invariants when inventory is already non-confirmable (`RELEASED`/`EXPIRED`): never confirm, stable `payment_operation_id` / `comp_<payOp>`, single compensation row, idempotent reconcile, seat not revived.  
   - **How B reaches INV_EXPIRED:** deterministic inventory **release** while Order is down (after payment success kill), not a wall-clock wait for `expires_at`.  
   - **What B does not prove alone:** the natural lease timer path (min lease clamp is 60s in inventory HTTP), expire-due scheduling, and “clock reaches expires_at then Order recovers” multi-service timing.  
   - Inventory store/HTTP expiry semantics and races are covered separately under inventory integration tests (e.g. expiry race); a dedicated multi-service natural-expiry timing e2e remains a follow-up if not already claimed elsewhere.
2. Crash hooks are process-local env flags only — never public APIs.
3. Test shortens outbox lease to 2s when `GATEFLOW_TEST_HOOKS=1` for reclaim speed.
4. Kafka scenario isolation uses dedicated consumer groups where needed.
5. Full “Kafka down at process start → bring Kafka up → workers recover” OS/Docker automation is still approximated by in-process Runtime reconnect tests (see P4.3.1), not a docker stop/start harness.
6. **Not committed/pushed** (per standing instruction).

## CTO acceptance (P4.5.1)

P4.5 accepted on reported evidence: crash suite A–E + regression gates. Phase 4 eligible for closure with residuals documented above (not silently treated as solved).

## Phase 4 close recommendation

Record Phase 4 complete after a closure audit of change records and evidence across P4.1–P4.5.1. Keep the worktree local until that audit confirms docs match implementation.
