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

`ops:sale:write`, `ops:sale:publish`, `ops:admission:read`, `ops:admission:fence`, `ops:admission:budget`, `ops:order:read`, `ops:order:recover`, `ops:outbox:read`, `ops:outbox:replay`, `ops:payment:recover`, `ops:audit:read`

## Service scopes (P5.1.0)

`inventory:write`, `admission:grant`, `admission:entitlement`, `payment:create`, `payment:callback`

## Production secrets

When `ENVIRONMENT` is not `local`/`dev`/`development`/`test`, `GATEFLOW_DEV_JWT_SECRET` must be set and must not equal the default development secret.

## Endpoint matrix

See `docs/security/endpoint-authorization-matrix.md`.

## Buyer identity (P5.1.1)

- Buyer JWT: `principal_type=buyer`, `sub` = canonical buyer ID, scope `buyer:self`.
- Mint: `authn.MintBuyerJWT` / `go run ./tools/devtoken buyer buyer:self <buyer_id>`.
- Product paths (checkout, own orders, queue join/me, own entitlement read) authorize from JWT only.
- `X-Buyer-ID` is ignored for authorization on those paths (may appear in legacy clients but does not grant access).
