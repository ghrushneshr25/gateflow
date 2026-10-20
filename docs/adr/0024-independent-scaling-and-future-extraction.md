# ADR-0024: Independent scaling and future extraction

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Services should scale and potentially split infrastructure later.

## Decision
- Separate deployable images/binaries per service from Phase 1
- Logical DB isolation via schemas/roles now; physical DB-per-service later if needed
- No shared domain libraries
- Independent horizontal scaling validated under multi-instance admission tests

## Alternatives
- Modular monolith single binary — rejected vs learning/deploy goals
- Premature DB-per-service — extra ops without need

## Consequences
+ Extraction path preserved
- Early multi-instance tests required for budget correctness

