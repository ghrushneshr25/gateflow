# ADR-0017: Payment simulator and compensation

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Need realistic failure modes without real card processing.

## Decision
Dedicated Payment Simulator service:
- Deterministic scenarios (success, fail, delay, duplicate callback, compensation fail)
- Durable payment and compensation operations
- First definitive compensation failure escalates; manual idempotent retry
- Never mark compensation success without verification

## Alternatives
- Mock payment inside Order — weaker isolation and realism
- Real PSP integration — non-goal

## Consequences
+ Testable failure modes
- Must not be mistaken for production payments (docs + naming)

