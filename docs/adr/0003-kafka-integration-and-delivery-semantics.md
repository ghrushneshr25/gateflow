# ADR-0003: Kafka integration and delivery semantics

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Async integration must not imply exactly-once business effects.

## Decision
- Kafka transports domain events (local: single-node KRaft)
- At-least-once delivery
- Business exactly-once via transactional outbox + idempotent consumers on `event_id`
- Topic names: `gateflow.<domain>.<entity>.<event>`
- Partition by stable entity id; no global ordering
- DLQ after bounded retries

## Alternatives
- NATS/Rabbit only — rejected: Kafka is the specified portfolio skill target
- Exactly-once Kafka transactions alone — rejected: insufficient for cross-service business effects
- Schema Registry from day one — deferred (non-goal); commit JSON Schemas in Git

## Consequences
+ Clear semantics; consumers must be idempotent
- Operational complexity of Kafka in Compose (acceptable)

