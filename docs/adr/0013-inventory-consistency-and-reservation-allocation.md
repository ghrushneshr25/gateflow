# ADR-0013: Inventory consistency and reservation allocation

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Oversell is a product failure.

## Decision
- Inventory service is sole capacity authority
- Atomic conditional allocation in PostgreSQL
- Invariant: confirmed + active_reserved <= sellable_capacity
- Stable operation_id for reserve; lookup for reconcile
- Lease default 5m (1–15m); UTC deadline; no revive

## Alternatives
- Optimistic UI holds — rejected as authority
- Redis counters as authority — rejected

## Consequences
+ Correct under concurrency
- Hot pools may see lock contention (measure in Phase 6)

