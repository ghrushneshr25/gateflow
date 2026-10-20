# ADR-0011: Global and per-session admission budgets

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Protect downstream systems and fairness under bursts.

## Decision
Separate accounting for:
- Global and per-session grant-rate limits
- Global and per-session active-checkout concurrency budgets
Reserve/commit/release semantics per GF-SCS-001.
Inventory capacity is not an admission budget.

## Alternatives
- Single global limiter only — insufficient for hot sessions
- Rely on inventory alone — overloads checkout path

## Consequences
+ Controlled admission
- Multi-instance enforcement needs Redis coordination + PG durability

