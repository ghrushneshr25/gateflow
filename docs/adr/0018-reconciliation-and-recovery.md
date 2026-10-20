# ADR-0018: Reconciliation and recovery

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Timeouts and crashes create unknown outcomes.

## Decision
- Ambiguous ops discoverable by operation_id / order state
- Reconciliation reuses original operation identities
- Holds retained while ambiguous
- Operator tools call service APIs only; audited
- Unresolved work remains durable and visible

## Alternatives
- Client-only retry with new ids — rejected
- Auto-fail on timeout — rejected

## Consequences
+ Recoverability
- Backlog metrics and runbooks required

