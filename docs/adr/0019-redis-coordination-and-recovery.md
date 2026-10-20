# ADR-0019: Redis coordination and recovery

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Redis speeds admission coordination but must not be business authority.

## Decision
Redis: hot rate/concurrency counters and queue projections.
On uncertainty: fence grants → preserve PG entitlements → reconcile → new coordination epoch → resume only when safe.
Stale workers fail fence checks. No blind counter reset.

## Alternatives
- PG-only rate limits — may miss p95 targets under burst; revisit with evidence
- Redis as entitlement authority — rejected

## Consequences
+ Safe multi-instance budgets
- Complex recovery protocol; must be tested (Phase 4)

