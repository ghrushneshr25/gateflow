# Phase 4.2 Report — Payment Ambiguity, Late Success, Compensation

## Security finding (verified + fixed)

| Check | Result |
|---|---|
| Spoof `X-Ops-Role` / `X-Service-Name` on `/v1/admin/reconciliation` | **401** |
| Operator JWT with `ops:order:recover` | **200** |
| Mechanism | `authn.Middleware` + `ClaimsFromContext` / scopes; headers alone ignored |

## Delivered

### Payment simulator
- Scenarios: `success`, `failure`, `delay_success` (+ `resolve_after`), `unknown`, `compensation_fail`
- `POST .../callbacks` (requires service/ops JWT + `payment:callback`)
- `POST /v1/payments/resolve-due` for deterministic delayed success (no sleep)
- Compensation: create/get/retry with stable `comp_<payment_operation_id>`
- First definitive fail escalates; retry can succeed; UNKNOWN requires query-before-force-retry

### Order
- Payment deadline (default 3m) → `RECONCILIATION_REQUIRED` without Fail/hold release
- Late success + expired reservation → compensation ensured; never CONFIRMED
- Reconcile/admin APIs JWT-gated

## Verification (executed)

| Command | Result |
|---|---|
| `make test` / `make build` | PASS |
| integration race order/inv/adm/payment | PASS |
| orchestrate + httpserver unit (incl. auth tests) | PASS |
| `./scripts/migrate.sh` | MIGRATIONS_OK |
| `./scripts/vs1_e2e.sh` | VS1_E2E_PASS |
| Live auth spoof / JWT | 401 / 200 |

## Decisive path

Payment SUCCEEDED + reservation EXPIRED → order stays unconfirmed (`RECONCILIATION_REQUIRED`) + durable compensation operation id recorded. Covered by unit + payment integration tests.

## Limitations

- Not committed/pushed
- Full E2E “pay then expire then interrupt process” multi-service script not automated (logic covered at driver + store layers)
- Kafka still P4.3

## Next

P4.3 — Kafka outbox publisher + consumer dedupe
