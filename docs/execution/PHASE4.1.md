# Phase 4.1 Report — Order Recovery Foundation

**HEAD baseline before change:** `6fa4994`  
**Status:** Complete (verified, uncommitted until requested)

## What changed

Order checkout is driven by `orchestrate.Driver` which safely advances durable state using stable operation IDs. Retries and `POST /v1/orders/{id}/reconcile` resume rather than no-op or dual-create.

## State machine

See `CHANGE-20261020-p4.1-order-recovery.md`.

## Files

- `services/order/internal/orchestrate/*`
- `services/order/internal/httpserver/server.go` (+ tests)
- `services/order/internal/store/store.go` (list/outbox helpers)
- `services/order/internal/ports/ports.go`
- `services/order/tests/recovery_test.go`
- docs change record + this report

## Verification

All required commands PASS — details in change record.

## Not done

P4.2+ compensation, Kafka, Redis fence. No push.
