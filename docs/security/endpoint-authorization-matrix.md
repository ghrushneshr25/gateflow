# Endpoint authorization matrix (P5.1.0)

**Auth:** HMAC dev JWT (`libs/go/authn`). Middleware attaches claims; protected routes use `authn.Require*` (fail-closed).  
**Buyer (P5.1.1):** Product paths require `principal_type=buyer` JWT; identity = `sub` only. `X-Buyer-ID` is **not** used for authorization.

| Method | Path | Service | Principal | Scope(s) | Unauth | Forbidden |
|---|---|---|---|---|---|---|
| GET | `/healthz`, `/readyz`, `/v1/meta` | all | public | — | 200 | — |
| GET | `/v1/offerings`, `/v1/offerings/{id}`, sessions, ticket-types, purchase-policy | sale | public | — | 200 | — |
| POST | `/v1/offerings`, `.../sessions`, `.../ticket-types` | sale | operator | `ops:sale:write` | 401 | 403 |
| POST | `/v1/offerings/{id}/publish` | sale | operator | `ops:sale:publish` or `ops:sale:write` | 401 | 403 |
| GET | `/v1/pools/{id}`, reservations get/by-op | inventory | public | — | 200 | — |
| POST | `/v1/pools`, `/v1/reservations`, confirm/release/expire/expire-due | inventory | service | `inventory:write` | 401 | 403 |
| POST | queue joins, GET queue/me | admission | buyer JWT | `buyer:self` | 401 | 403 |
| GET | `/v1/entitlements/{id}` | admission | buyer owner **or** service | `buyer:self` / entitlement\|grant | 401 | 403 |
| POST | `/v1/sessions/{id}/grants` | admission | service (`admission:grant` + body `buyer_id`) **or** buyer JWT self-grant only (`sub`; body ignored) → always `GrantFIFO` | 401 | 403 |
| POST | entitlements bind/consume | admission | service | `admission:entitlement` | 401 | 403 |
| GET/PUT | `/v1/admin/budgets/{scope}` | admission | operator/service | budget/read/fence | 401 | 403 |
| GET/POST | coordination, fence, unfence, metrics | admission | operator/service | read/fence | 401 | 403 |
| POST | `/v1/checkouts`, GET `/v1/orders` | order | buyer JWT | `buyer:self` | 401 | — |
| GET | `/v1/orders/{id}` | order | buyer JWT owner **or** ops/service read/recover | 401 | 403 mismatch |
| POST | reconcile | order | buyer JWT owner **or** ops recover | 401 | 403 |
| POST | reconcile | order | owner buyer **or** ops recover | — | 403 | — |
| GET | admin reconciliation, outbox * | order | operator/service | recover/outbox scopes | 401 | 403 |
| POST | `/v1/payments` | payment | service | `payment:create` | 401 | 403 |
| GET | payments by-operation | payment | operator/service | `payment:create` or `ops:payment:recover` | 401 | 403 |
| POST | callbacks | payment | operator/service | `payment:callback` **only** | 401 | 403 |
| POST | compensations, retry, resolve-due | payment | operator/service | `ops:payment:recover` **only** | 401 | 403 |
| GET | compensations | payment | operator/service | `ops:payment:recover` | 401 | 403 |

## Critical gaps closed

| ID | Closure |
|---|---|
| C1 | Inventory mutators require `inventory:write` service JWT |
| C2 | Grant/bind/consume require service scopes |
| C3 | Order get requires principal (buyer header or ops) |
| C4 | Sale writes/publish require operator scopes |
| C5 | Budget admin requires ops scopes |
| C6 | Payment create/lookup/resolve-due protected |
| C7 | No `*` in service mint; production rejects default secret |

## Service token minting

- Order: `authn.OrderServiceScopes` (no wildcard).  
- Sale→inventory: `authn.SaleServiceScopes`.  
- Override: `GATEFLOW_SERVICE_TOKEN`.  
- Local mint CLI: `go run ./tools/devtoken <principal> <scopes> [subject]`.
