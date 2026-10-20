# CHANGE-20261020 — Phase 2 VS-1 vertical slice

## Summary

Implemented end-to-end happy-path booking across Sale, Admission, Inventory, Order, and Payment Simulator with PostgreSQL-backed state, atomic inventory reservation, allowance holds, entitlement bind/consume, and transactional outbox on order confirm.

## Problem

No executable booking path existed after foundation scaffolding.

## Previous → new

- Health-only services → domain APIs for VS-1  
- No migrations beyond bootstrap → domain schemas per service  
- No E2E → `scripts/vs1_e2e.sh` PASS  

## Invariants

- Inventory capacity CHECK + `FOR UPDATE` allocate  
- Purchase allowance hold at order create  
- Payment success does not alone confirm; inventory confirm required  
- Idempotent checkout by `(buyer_id, idempotency_key)`  

## Verification

`./scripts/vs1_e2e.sh` → `VS1_E2E_PASS` (executed 2026-10-20).

## Follow-up

Redis budgets, Kafka publisher, expiry worker, auth middleware, operator UI, concurrency integration test in CI.
