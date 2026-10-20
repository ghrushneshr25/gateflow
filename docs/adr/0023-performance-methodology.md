# ADR-0023: Performance methodology

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Performance claims without methodology are not credible.

## Decision
Documented workloads, environment, warm-up, repetitions, latency distributions, resource use, and **correctness gates**.
Provisional latency targets only; no fixed RPS claim until measured.
30-minute initial soak.
Results published under `docs/benchmarks/` only after real runs.

## Alternatives
- Cite aspirational RPS in README — rejected
- Optimize before measuring — rejected

## Consequences
+ Honest portfolio evidence
- Phase 6 required before perf claims

