# ADR-0015: Order state machine and checkout orchestration

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Checkout spans services without distributed transactions.

## Decision
Order orchestrates: policy snapshot → allowance → entitlement bind → inventory reserve → payment → inventory confirm → finalize.
State machine per GF-SCS-001; payment success ≠ order confirmed.
Idempotent checkout via Idempotency-Key.

## Alternatives
- Saga engine / Temporal — deferred non-goal
- Inventory orchestrates — wrong boundary

## Consequences
+ Clear ownership of workflow progress
- Reconciliation required for ambiguity

