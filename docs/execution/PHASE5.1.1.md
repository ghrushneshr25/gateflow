# Phase 5.1.1 Report — Buyer JWT Principal

## Verdict: **COMPLETE**

## Objective

Buyer identity on product paths comes from verified JWT (`principal_type=buyer`, `sub` = buyer ID). `X-Buyer-ID` cannot authorize or override.

## Delivered

- `authn.RequireBuyer`, `MintBuyerJWT`, scope `buyer:self`
- Order: checkout, list, get (buyer path), reconcile (buyer path) require buyer JWT; ownership = `sub`
- Admission: queue join/me require buyer JWT; entitlement GET owner or service
- Grant remains service-only with body `buyer_id` (worker-issued)
- Invalid bearer does not fall back to header
- VS1 + crash harness mint buyer tokens
- `tools/devtoken buyer buyer:self <buyer_id>`

## Endpoint policy (buyer)

| Endpoint | Policy |
|---|---|
| POST checkout, GET orders, GET order (owner), POST reconcile (owner) | Buyer JWT `sub` |
| GET order / reconcile cross-buyer | Ops/service scopes only |
| POST queue join, GET queue/me | Buyer JWT |
| GET entitlement | Buyer owner **or** service entitlement/grant scope |
| POST grant | Service + body buyer_id |

## Verification

| Command | Result |
|---|---|
| authn + order/admission unit | PASS |
| `make test` / `make build` | PASS |
| integration race + payment | PASS |
| `./scripts/vs1_e2e.sh` | VS1_E2E_PASS |
| `make test-crash-recovery` | PASS A–E |

## Residual

- Buyer Web still hardcoded header (UI not in scope)
- No external IdP
- No commit/push
