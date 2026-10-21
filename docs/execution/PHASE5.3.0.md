# Phase 5.3.0 Report — Buyer Discovery-to-Checkout Vertical Slice

## Verdict: **COMPLETE**

## Journey → endpoints

| Step | API | Auth |
|---|---|---|
| List published offerings | `GET /v1/offerings?status=PUBLISHED` | public |
| Offering + sessions + tickets | `GET offerings/{id}`, sessions, ticket-types | public |
| Join queue | `POST .../queue/joins` | buyer JWT |
| Queue status | `GET .../queue/me` | buyer JWT |
| Request grant | `POST .../grants` | buyer JWT self-grant **or** service |
| Checkout | `POST /v1/checkouts` + Idempotency-Key | buyer JWT |
| Orders list/detail | `GET /v1/orders`, `GET /v1/orders/{id}` | buyer JWT owner |
| Reconcile (pending) | `POST .../reconcile` | buyer JWT owner |

## Backend note (smallest justified change)

Admission **grant** accepts buyer JWT for **self-grant only** (`sub`); service grant unchanged. Enables vertical slice without a separate worker process.

## Identity & idempotency

- Buyer JWT sessionStorage; no X-Buyer-ID.
- Idempotency key stored per session+ticket until CONFIRMED; reused on retry/timeout.
- UI never claims confirmed until order status is CONFIRMED.

## Polling

- `queue/me` refetch ~2.5s while waiting, max ~40 ticks, stops on GRANTED/unmount/checkout.

## Verification

| Command | Result |
|---|---|
| buyer-web `npm test -- --run` | **PASS** (5 tests) |
| buyer-web `npm run typecheck` | **PASS** |
| buyer-web `npm run build` | **PASS** |
| `go test ./services/admission/internal/httpserver/` | **PASS** |
| Live full browser journey | **NOT RUN** |
| `make test` / vs1 / crash-recovery | **NOT RUN** this milestone (buyer/admission unit gates only) |

## Residual

- P5.3.1 hardening for sold-out/expired/a11y polish  
- sessionStorage XSS (local)  
- Self-grant is a product convenience; production may still prefer worker grants  

## No commit / no push
