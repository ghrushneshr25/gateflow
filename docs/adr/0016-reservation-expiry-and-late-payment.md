# ADR-0016: Reservation expiry and late payment

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Payment may succeed after inventory lease ends.

## Decision
- Expiry by durable deadline; worker lag does not extend eligibility
- Confirm vs expire: conditional single winner
- Late payment success: no revive; keep payment success; order RECONCILIATION_REQUIRED; idempotent compensation; operator visibility

## Alternatives
- Auto-extend lease on payment pending — rejected (unbounded inventory hold risk)
- Auto-confirm without inventory — rejected

## Consequences
+ Correct money/inventory decoupling
- Explicit compensation path mandatory

