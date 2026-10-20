# ADR-0014: Purchase-limit scope and atomic enforcement

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Buyers must not exceed configured limits under concurrent checkouts.

## Decision
- Sale owns policy config/versions
- Order owns atomic allowance accounting: committed + held + requested <= limit
- Default 4 tickets per buyer per session
- Scopes: session, offering, ticket type, combinations with documented precedence

## Alternatives
- Enforce only in Inventory — wrong ownership; incomplete multi-pool cases
- Frontend-only limits — rejected

## Consequences
+ Correct multi-checkout behavior
- Ambiguous checkouts must retain holds

