# Phase 5.1.0 Report — Authorization Hard-Close

## Verdict: **COMPLETE**

## Delivered

- Shared `authn.Require*` fail-closed helpers; invalid bearer marked; production secret validation.
- Least-privilege `MintServiceJWT` / `MintOperatorJWT` (no implicit `*`).
- Sale mutators + publish: operator JWT.
- Inventory mutators: service `inventory:write`.
- Admission grant/bind/consume + budgets: service/ops scopes.
- Order get IDOR fixed; S2S bearer on all order downstream HTTP.
- Payment create/lookup/callback/comp/resolve-due scoped strictly (no scope-OR crossover).
- `tools/devtoken`, vs1 + crash harness credentials.
- Docs: security overview, endpoint matrix, change record.

## Intentional public endpoints

- Health/meta; sale catalog **reads**; inventory **reads**; admission queue join/me (buyer header transitional); entitlement GET.

## Buyer identity limitation (P5.1.1)

`X-Buyer-ID` remains spoofable for checkout/list/owner checks. Not claimed as cryptographic buyer auth.

## Verification (executed)

| Command | Result |
|---|---|
| `go test ./libs/go/authn/ ... sale/inv/order/admission httpserver` | PASS |
| `make test` | PASS |
| `make build` | PASS |
| `./scripts/migrate.sh` | MIGRATIONS_OK |
| `make test-integration-race` + payment integration | PASS |
| `./scripts/vs1_e2e.sh` | VS1_E2E_PASS |
| `make test-crash-recovery` A–E | PASS |

## Gap IDs

C1–C7 closed with tests/gates above.

## Not committed/pushed
