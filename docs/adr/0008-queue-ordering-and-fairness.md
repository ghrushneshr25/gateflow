# ADR-0008: Queue ordering and fairness

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Fair FIFO requires durable order across instances and reconnects.

## Decision
- Server-assigned durable sequence in PostgreSQL
- Join idempotent on (buyer, session/queue scope)
- Reconnect returns existing entry; does not reset position
- Redis may project position; PG is authority

## Alternatives
- Client timestamps — rejected (spoofable, clock skew)
- In-memory only — rejected (restart loss)

## Consequences
+ Fair, restart-safe ordering
- Sequence generation needs careful concurrency design

