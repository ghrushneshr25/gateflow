# ADR-0007: Admission strategy interface

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Multiple admission strategies must share budgets and entitlement rules.

## Decision
Common Go `Strategy` interface; implementations: FIFO, lottery, scheduled_slot, direct.
Shared budget enforcement and entitlement lifecycle live outside strategy code paths.

## Alternatives
- Separate microservices per strategy — rejected
- Hardcoded FIFO only forever — rejected: extensibility goal

## Consequences
+ Pluggable policies without invariant bypass
- Interface must stay thin; heavy logic stays in shared grant pipeline

