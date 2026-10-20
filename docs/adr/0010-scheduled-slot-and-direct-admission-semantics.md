# ADR-0010: Scheduled-slot and direct-admission semantics

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Not all sales use queues.

## Decision
- Scheduled slots: grants only in configured windows + budgets
- Direct: skip waiting queue; still enforce rate and active-checkout budgets
- Both use same entitlement and budget pipeline

## Alternatives
- Separate codepaths without shared budgets — rejected

## Consequences
+ Consistent safety
- Product config must make strategy explicit per session

