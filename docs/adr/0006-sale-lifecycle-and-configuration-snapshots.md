# ADR-0006: Sale lifecycle and configuration snapshots

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Checkout must not silently change meaning when operators edit policies mid-flight.

## Decision
- Offering/session publication state machines per GF-SCS-001
- Purchase policies versioned; Order snapshots `policy_version` at checkout
- Active holds retain snapshot unless explicit migration

## Alternatives
- Always read live policy — rejected: non-deterministic enforcement
- Freeze entire offering on first checkout — too rigid

## Consequences
+ Stable checkout semantics
- Need UI/ops clarity on which version applied

