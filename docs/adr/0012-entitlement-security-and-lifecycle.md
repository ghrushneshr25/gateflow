# ADR-0012: Entitlement security and lifecycle

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Admission proof must not be transferable or reusable across checkouts.

## Decision
- Durable entitlement owned by Admission
- Default unbound TTL 10 minutes
- Atomic bind to (buyer_id, checkout_operation_id)
- Bound entitlements not killed solely by unbound TTL
- Consume after reservation success established
- Ambiguous states block unsafe reuse

## Alternatives
- Stateless signed tokens only — weaker revocation/bind control
- Inventory as admission proof — coupling

## Consequences
+ Strong checkout binding
- Order and Admission must coordinate bind/consume carefully

