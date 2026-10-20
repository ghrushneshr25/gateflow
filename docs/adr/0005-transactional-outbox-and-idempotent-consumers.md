# ADR-0005: Transactional outbox and idempotent consumers

**Status:** Accepted  
**Date:** 2026-10-20

## Context
State changes requiring events must not lose or double-apply business effects.

## Decision
- Outbox row inserted in same DB transaction as state change
- Publisher polls/relays to Kafka; safe under retry
- Consumers record processed `event_id` (and business keys where needed)
- Duplicate delivery must be a no-op

## Alternatives
- Listen to Postgres WAL CDC first — deferred complexity
- Dual-write without outbox — rejected: loss/duplication risk

## Consequences
+ Durable publication linked to commits
- Publisher lag must be monitored

## Implementation notes (P4.3)

- Claim: `FOR UPDATE SKIP LOCKED` with `locked_by` / `locked_until` lease; no DB txn held during Kafka I/O.
- Mark `published_at` only after Kafka `RequireOne` ack.
- Crash after ack before mark → republish same `event_id`; consumer `processed_events` + `order_confirmed_effects` in one txn.
- Kafka downtime does not block `Store.Confirm`.
- Poison path: `quarantine` after repeated publish failures (attempt_count ≥ 25).
- Topic: `gateflow.order.order.confirmed`; client: `segmentio/kafka-go`.

