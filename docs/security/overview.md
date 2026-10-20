# Gateflow Security Baseline

**Status:** Accepted defaults (Phase 0)  
**Related:** ADR-0020, GF-SCS-001 §2

## Principals

| Type | Purpose |
|---|---|
| Buyer | Browse, queue, checkout, view own orders |
| Operator | Configure sales, inspect, authorized recovery |
| Service | S2S calls with restricted audience/scopes |

## Local development auth

- HMAC-signed JWT via dev token adapter
- Claims: `sub`, `principal_type`, `scopes`, `iss`, `aud`, `exp`
- Production path: OIDC-compatible (not required for VS-1)

## Rules

1. Every service validates authz for its endpoints.
2. Frontend guards are not security controls.
3. Least-privilege PostgreSQL roles per service schema.
4. Payment callbacks require verified auth (HMAC or service JWT).
5. Never log tokens, credentials, payment secrets, or unnecessary PII.
6. Sensitive operator actions: audit actor, action, target, timestamp, outcome, reason.
7. Secrets only via env/secret store; `.env` gitignored; `.env.example` has placeholders.

## Operator scopes (initial)

`ops:sale:write`, `ops:sale:publish`, `ops:admission:read`, `ops:inventory:read`, `ops:order:read`, `ops:order:recover`, `ops:payment:recover`, `ops:audit:read`
