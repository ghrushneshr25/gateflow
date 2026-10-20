# ADR-0009: Lottery reproducibility and auditability

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Lottery must be auditable.

## Decision
Store algorithm version + seed (or verifiable random beacon reference) on each draw/grant batch.
Initial algorithm: seeded Fisher–Yates or weighted draw over eligible set; exact function versioned in code.

## Alternatives
- Opaque Math.random without seed — rejected
- Fully public verifiable delay functions — deferred

## Consequences
+ Replayable audits
- Seed management and publication policy needed

