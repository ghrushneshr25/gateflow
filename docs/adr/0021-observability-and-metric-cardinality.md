# ADR-0021: Observability and metric cardinality

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Need investigation tools without metric explosion.

## Decision
OpenTelemetry + Prometheus + Grafana + Loki + Tempo.
Correlation/causation IDs on requests/events.
Identifiers in logs/traces; **not** as metric labels.
Liveness vs readiness endpoints.

## Alternatives
- Logs only — insufficient
- Per-order metrics — rejected (cardinality)

## Consequences
+ Operable system
- Requires disciplined label design

